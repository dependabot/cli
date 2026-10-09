package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func TestReadConfig(t *testing.T) {
	env := map[string]string{
		"INPUT_API-URL":                "https://dependabot.example/api/",
		"INPUT_JOB-ID":                 "42",
		"GITHUB_DEPENDABOT_JOB_TOKEN":  "job",
		"GITHUB_DEPENDABOT_CRED_TOKEN": "cred",
		"INPUT_UPDATER-IMAGE":          "custom:job",
		"INPUT_PULL-IMAGES":            "false",
		"INPUT_TIMEOUT":                "3h",
		"INPUT_VOLUMES":                "\n/github/workspace/cache:/cache\n/github/workspace/data:/data:ro\n",
		"INPUT_UPDATER-ENV":            "CACHE_PATH=/cache\nVALUE=a=b\n",
	}
	cfg, err := readConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.apiURL != "https://dependabot.example/api" || cfg.jobToken != "job" || cfg.credentialsToken != "cred" ||
		cfg.pullImages || cfg.timeout != 3*time.Hour || cfg.updaterImage != "custom:job" ||
		len(cfg.volumes) != 2 || !reflect.DeepEqual(cfg.updaterEnv, []string{"CACHE_PATH=/cache", "VALUE=a=b"}) {
		t.Fatalf("incorrect config: %+v", cfg)
	}
	for key, invalid := range map[string]string{
		"INPUT_API-URL": "http://example.com", "INPUT_JOB-ID": "../42",
		"INPUT_PULL-IMAGES": "yes", "INPUT_TIMEOUT": "0",
		"INPUT_UPDATER-ENV": "INVALID", "GITHUB_DEPENDABOT_JOB_TOKEN": "",
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
