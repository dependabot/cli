package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func writeEventFile(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadConfigDynamicEvent(t *testing.T) {
	path := writeEventFile(t, `{"inputs":{
		"dependabotApiUrl":"https://dependabot.example/api/",
		"jobId":"42","jobToken":"event-job","credentialsToken":"event-cred",
		"updaterImage":"event:updater"
	}}`)
	dockerPath := writeEventFile(t, `{"inputs":{
		"dependabotApiUrl":"https://dependabot.example/api/",
		"dependabotApiDockerUrl":"https://event-docker.example/api/",
		"jobId":"42","jobToken":"event-job","credentialsToken":"event-cred",
		"updaterImage":"event:updater"
	}}`)
	for _, tc := range []struct {
		name      string
		overrides map[string]string
		want      config
	}{
		{
			name: "event inputs",
			want: config{
				apiURL: "https://dependabot.example/api", apiDockerURL: "https://dependabot.example/api",
				jobID: "42", jobToken: "event-job", credentialsToken: "event-cred", updaterImage: "event:updater",
			},
		},
		{
			name: "explicit inputs",
			overrides: map[string]string{
				"INPUT_API-URL": "https://override.example/prefix/", "INPUT_JOB-ID": "99",
				"INPUT_JOB-TOKEN": "input-job", "INPUT_CREDENTIALS-TOKEN": "input-cred",
				"INPUT_UPDATER-IMAGE": "input:updater",
			},
			want: config{
				apiURL: "https://override.example/prefix", apiDockerURL: "https://override.example/prefix",
				jobID: "99", jobToken: "input-job", credentialsToken: "input-cred", updaterImage: "input:updater",
			},
		},
		{
			name: "environment tokens",
			overrides: map[string]string{
				"GITHUB_DEPENDABOT_JOB_TOKEN": "env-job", "GITHUB_DEPENDABOT_CRED_TOKEN": "env-cred",
				"INPUT_JOB-TOKEN": "input-job", "INPUT_CREDENTIALS-TOKEN": "input-cred",
			},
			want: config{
				apiURL: "https://dependabot.example/api", apiDockerURL: "https://dependabot.example/api",
				jobID: "42", jobToken: "env-job", credentialsToken: "env-cred", updaterImage: "event:updater",
			},
		},
		{
			name: "event Docker URL",
			overrides: map[string]string{
				"GITHUB_EVENT_PATH": dockerPath,
			},
			want: config{
				apiURL: "https://dependabot.example/api", apiDockerURL: "https://event-docker.example/api",
				jobID: "42", jobToken: "event-job", credentialsToken: "event-cred", updaterImage: "event:updater",
			},
		},
		{
			name: "explicit Docker URL",
			overrides: map[string]string{
				"GITHUB_EVENT_PATH":    dockerPath,
				"INPUT_API-DOCKER-URL": "https://docker.example/prefix/",
			},
			want: config{
				apiURL: "https://dependabot.example/api", apiDockerURL: "https://docker.example/prefix",
				jobID: "42", jobToken: "event-job", credentialsToken: "event-cred", updaterImage: "event:updater",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"GITHUB_EVENT_NAME": "dynamic", "GITHUB_EVENT_PATH": path}
			for key, value := range tc.overrides {
				env[key] = value
			}
			got, err := readConfig(func(key string) string { return env[key] })
			tc.want.pullImages = true
			tc.want.timeout = time.Hour
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("readConfig() = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
}

func TestReadConfigEventErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		payload   string
		eventName string
		overrides map[string]string
		want      string
	}{
		{name: "missing path", eventName: "dynamic", want: "GITHUB_EVENT_PATH is required"},
		{name: "unreadable path", eventName: "dynamic", overrides: map[string]string{"GITHUB_EVENT_PATH": filepath.Join(t.TempDir(), "missing.json")}, want: "cannot read GITHUB_EVENT_PATH"},
		{name: "malformed JSON", eventName: "dynamic", payload: `{"inputs":{"jobToken":"do-not-log-secret"`, want: "invalid Dependabot event JSON"},
		{name: "missing inputs", eventName: "dynamic", payload: `{}`, want: "Dependabot event must contain inputs"},
		{name: "null inputs", eventName: "dynamic", payload: `{"inputs":null}`, want: "Dependabot event must contain inputs"},
		{name: "wrong input type", eventName: "dynamic", payload: `{"inputs":{"jobId":[]}}`, want: "invalid Dependabot event JSON"},
		{name: "missing URL", eventName: "dynamic", payload: `{"inputs":{"jobId":"42"}}`, want: "api-url is required"},
		{name: "missing job ID", eventName: "dynamic", payload: `{"inputs":{"dependabotApiUrl":"https://example.com"}}`, want: "job-id is required"},
		{name: "other event ignored", eventName: "workflow_dispatch", payload: `{"inputs":{"dependabotApiUrl":"https://example.com","jobId":"42"}}`, want: "api-url is required"},
		{name: "invalid explicit URL", eventName: "dynamic", payload: `{"inputs":{"dependabotApiUrl":"https://example.com","jobId":"42"}}`, overrides: map[string]string{"INPUT_API-URL": "http://invalid.example"}, want: "api-url must be an HTTPS URL"},
		{name: "invalid event URL", eventName: "dynamic", payload: `{"inputs":{"dependabotApiUrl":"https://user:do-not-log-secret@example.com","jobId":"42"}}`, want: "api-url must be an HTTPS URL"},
		{name: "query in event URL", eventName: "dynamic", payload: `{"inputs":{"dependabotApiUrl":"https://example.com?token=do-not-log-secret","jobId":"42"}}`, want: "api-url must be an HTTPS URL"},
		{name: "fragment in event URL", eventName: "dynamic", payload: `{"inputs":{"dependabotApiUrl":"https://example.com#do-not-log-secret","jobId":"42"}}`, want: "api-url must be an HTTPS URL"},
		{name: "invalid Docker URL", eventName: "dynamic", payload: `{"inputs":{"dependabotApiUrl":"https://example.com","dependabotApiDockerUrl":"http://invalid.example","jobId":"42"}}`, want: "api-docker-url must be an HTTPS URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"GITHUB_EVENT_NAME":           tc.eventName,
				"GITHUB_DEPENDABOT_JOB_TOKEN": "job", "GITHUB_DEPENDABOT_CRED_TOKEN": "cred",
			}
			if tc.payload != "" {
				env["GITHUB_EVENT_PATH"] = writeEventFile(t, tc.payload)
			}
			for key, value := range tc.overrides {
				env[key] = value
			}
			_, err := readConfig(func(key string) string { return env[key] })
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "do-not-log-secret") {
				t.Fatalf("readConfig() error = %v; want %q without secrets", err, tc.want)
			}
		})
	}
}

func TestReadConfig(t *testing.T) {
	env := map[string]string{
		"INPUT_API-URL":                 "https://dependabot.example/api/",
		"INPUT_JOB-ID":                  "42",
		"GITHUB_DEPENDABOT_JOB_TOKEN":   "job",
		"GITHUB_DEPENDABOT_CRED_TOKEN":  "cred",
		"INPUT_UPDATER-IMAGE":           "custom:job",
		"INPUT_PULL-IMAGES":             "false",
		"INPUT_SKIP-IMAGE-VERIFICATION": "true",
		"INPUT_TIMEOUT":                 "3h",
		"INPUT_VOLUMES":                 "\n/github/workspace/cache:/cache\n/github/workspace/data:/data:ro\n",
		"INPUT_UPDATER-ENV":             "CACHE_PATH=/cache\nVALUE=a=b\n",
	}
	cfg, err := readConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.apiURL != "https://dependabot.example/api" || cfg.jobToken != "job" || cfg.credentialsToken != "cred" ||
		cfg.pullImages || !cfg.skipImageVerification || cfg.timeout != 3*time.Hour || cfg.updaterImage != "custom:job" ||
		len(cfg.volumes) != 2 || !reflect.DeepEqual(cfg.updaterEnv, []string{"CACHE_PATH=/cache", "VALUE=a=b"}) {
		t.Fatalf("incorrect config: %+v", cfg)
	}
	for key, invalid := range map[string]string{
		"INPUT_API-URL": "http://example.com", "INPUT_JOB-ID": "../42",
		"INPUT_PULL-IMAGES": "yes", "INPUT_TIMEOUT": "0",
		"INPUT_SKIP-IMAGE-VERIFICATION": "yes",
		"INPUT_UPDATER-ENV":             "INVALID", "GITHUB_DEPENDABOT_JOB_TOKEN": "",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := readConfig(func(k string) string {
				if k == key {
					return invalid
				}
				return env[k]
			})
			if err == nil {
				t.Fatalf("accepted invalid %s", key)
			}
		})
	}
	for _, value := range []string{"", "false", "true"} {
		t.Run("skip-image-verification="+value, func(t *testing.T) {
			cfg, err := readConfig(func(k string) string {
				if k == "INPUT_SKIP-IMAGE-VERIFICATION" {
					return value
				}
				return env[k]
			})
			if err != nil || cfg.skipImageVerification != (value == "true") {
				t.Fatalf("verification input %q = %v, %v", value, cfg.skipImageVerification, err)
			}
		})
	}
}

func TestTranslateVolumes(t *testing.T) {
	mounts := []container.MountPoint{
		{Type: mount.TypeBind, Source: "/runner/work/repo", Destination: "/github/workspace", RW: true},
		{Type: mount.TypeBind, Source: "/runner/temp/cache", Destination: "/github/workspace/cache", RW: true},
		{Type: mount.TypeBind, Source: "/runner/readonly", Destination: "/readonly", RW: false},
	}
	got, err := translateVolumes([]string{"/github/workspace/cache:/cache", "/github/workspace/data:/data:ro"}, mounts)
	want := []string{"/runner/temp/cache:/cache", "/runner/work/repo/data:/data:ro"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("translation = %v, %v; want %v", got, err, want)
	}
	for _, invalid := range []string{
		"/github/workspace-other:/data", "/github/workspace/../../etc:/data",
		"/tmp/unmounted:/data", "/github/workspace:/data:rw", "/github/workspace:relative",
		"/readonly:/data", "/github/workspace", "/:/",
	} {
		if _, err := translateVolumes([]string{invalid}, mounts); err == nil {
			t.Errorf("accepted invalid volume %q", invalid)
		}
	}
	got, err = translateVolumes([]string{"/readonly:/data:ro"}, mounts)
	if err != nil || len(got) != 1 || !strings.HasSuffix(got[0], ":ro") {
		t.Fatalf("read-only mount = %v, %v", got, err)
	}
}
