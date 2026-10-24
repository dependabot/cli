package infra

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/server"
	"github.com/docker/docker/client"

	"github.com/dependabot/cli/internal/model"
)

func Test_setImageNames(t *testing.T) {
	tests := []struct {
		packageManager string
		expectedSuffix string
	}{
		{"conda", "conda"},
		{"deno", "deno"},
		{"powershell", "powershell"},
	}
	for _, tt := range tests {
		t.Run(tt.packageManager, func(t *testing.T) {
			params := &RunParams{
				Job: &model.Job{PackageManager: tt.packageManager},
			}
			if err := setImageNames(params); err != nil {
				t.Fatalf("setImageNames returned unexpected error: %v", err)
			}
			expected := "ghcr.io/dependabot/dependabot-updater-" + tt.expectedSuffix
			if params.UpdaterImage != expected {
				t.Errorf("expected UpdaterImage %q, got %q", expected, params.UpdaterImage)
			}
		})
	}

	t.Run("unknown package manager returns error", func(t *testing.T) {
		params := &RunParams{
			Job: &model.Job{PackageManager: "unknown-ecosystem"},
		}
		if err := setImageNames(params); err == nil {
			t.Error("expected error for unknown package manager, got nil")
		}
	})
}

func TestRunContainersRejectsUnverifiedImages(t *testing.T) {
	var mu sync.Mutex
	writes := 0
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mu.Lock()
			writes++
			mu.Unlock()
			t.Errorf("Docker write before image verification: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/json") {
			writeJSON(t, w, map[string]any{"Id": "unsigned-image", "RepoDigests": []string{}})
			return
		}
		w.Header().Set("API-Version", testDockerAPIVersion)
		fmt.Fprint(w, "OK")
	}))
	defer docker.Close()
	t.Setenv("DOCKER_HOST", docker.URL)
	t.Setenv("DOCKER_API_VERSION", testDockerAPIVersion)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	err := runContainers(t.Context(), RunParams{
		Job: &model.Job{}, ProxyImage: "unsigned-proxy", UpdaterImage: "unsigned-updater",
	})
	if err == nil || !strings.Contains(err.Error(), "no matching repository digest") {
		t.Fatalf("runContainers() = %v, want unsigned image rejection", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 0 {
		t.Fatalf("performed %d Docker writes before verification", writes)
	}
}

func TestRunContainersRejectsUnverifiedCollector(t *testing.T) {
	var mu sync.Mutex
	writes := 0
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mu.Lock()
			writes++
			mu.Unlock()
			t.Errorf("Docker write before Collector verification: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1.44/images/") && strings.HasSuffix(r.URL.Path, "/json") {
			imageName := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.44/images/"), "/json")
			var digests []string
			if imageName != "collector" {
				digests = []string{imageName + "@sha256:" + strings.Repeat("a", 64)}
			}
			writeJSON(t, w, map[string]any{"Id": "local-" + imageName, "RepoDigests": digests})
			return
		}
		w.Header().Set("API-Version", testDockerAPIVersion)
		fmt.Fprint(w, "OK")
	}))
	defer docker.Close()
	t.Setenv("DOCKER_HOST", docker.URL)
	t.Setenv("DOCKER_API_VERSION", testDockerAPIVersion)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	err := runContainers(t.Context(), RunParams{
		Job: &model.Job{}, ProxyImage: "proxy", UpdaterImage: "updater",
		CollectorImage: "collector", CollectorConfigPath: filepath.Join(t.TempDir(), "collector.yaml"),
	})
	if err == nil || !strings.Contains(err.Error(), "collector image") || !strings.Contains(err.Error(), "no matching repository digest") {
		t.Fatalf("runContainers() = %v, want unsigned Collector rejection", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 0 {
		t.Fatalf("performed %d Docker writes before Collector verification", writes)
	}
}

func TestRunContainersCollectorPullFailure(t *testing.T) {
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", testDockerAPIVersion)
			fmt.Fprint(w, "OK")
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/images/cached-proxy/json":
			writeJSON(t, w, map[string]any{"Id": "local-proxy", "RepoDigests": []string{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/images/missing-collector/json":
			w.WriteHeader(http.StatusNotFound)
			writeJSON(t, w, map[string]string{"message": "image not found"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/images/create":
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(t, w, map[string]string{"message": "collector pull denied"})
		default:
			t.Errorf("Docker operation after Collector pull failure: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(t, w, map[string]string{"message": "unexpected Docker operation"})
		}
	}))
	defer docker.Close()
	t.Setenv("DOCKER_HOST", docker.URL)
	t.Setenv("DOCKER_API_VERSION", testDockerAPIVersion)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	err := runContainers(t.Context(), RunParams{
		Job: &model.Job{}, ProxyImage: "cached-proxy", UpdaterImage: "unused-updater",
		CollectorImage: "missing-collector", CollectorConfigPath: filepath.Join(t.TempDir(), "collector.yaml"),
		PullImages: true, SkipImageVerification: true,
	})
	if err == nil || !strings.Contains(err.Error(), "OpenTelemetry collector") || !strings.Contains(err.Error(), "collector pull denied") {
		t.Fatalf("runContainers() = %v, want Collector pull failure", err)
	}
}

func TestRunContainersCollectorStartupFailure(t *testing.T) {
	for _, stage := range []string{"create", "start"} {
		t.Run(stage, func(t *testing.T) {
			var mu sync.Mutex
			var noInternetName string
			var created []string
			removedProxy, removedNetworks := false, 0
			docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.URL.Path == "/_ping":
					w.Header().Set("API-Version", testDockerAPIVersion)
					fmt.Fprint(w, "OK")
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/networks/create":
					var cfg struct {
						Name     string
						Internal bool
					}
					if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					id := "internet"
					if cfg.Internal {
						id, noInternetName = "no-internet", cfg.Name
					}
					writeJSON(t, w, map[string]string{"Id": id})
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
					var cfg struct{ Image string }
					if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					created = append(created, cfg.Image)
					if cfg.Image == "collector" && stage == "create" {
						w.WriteHeader(http.StatusInternalServerError)
						writeJSON(t, w, map[string]string{"message": "collector create denied"})
					} else if cfg.Image == "proxy" || cfg.Image == "collector" {
						writeJSON(t, w, map[string]string{"Id": cfg.Image})
					} else {
						t.Errorf("created updater after Collector failure: %s", cfg.Image)
						w.WriteHeader(http.StatusInternalServerError)
					}
				case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/proxy/json":
					writeJSON(t, w, map[string]any{
						"Id": "proxy", "State": map[string]any{"Running": true, "ExitCode": 0},
						"NetworkSettings": map[string]any{"Networks": map[string]any{
							noInternetName: map[string]string{"IPAddress": "192.0.2.2"},
						}},
					})
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/collector/start":
					w.WriteHeader(http.StatusInternalServerError)
					writeJSON(t, w, map[string]string{"message": "collector start denied"})
				case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/archive"),
					r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect"),
					r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"),
					r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/proxy/start":
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1.44/containers/"):
					removedProxy = removedProxy || r.URL.Path == "/v1.44/containers/proxy"
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1.44/networks/"):
					removedNetworks++
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer docker.Close()
			t.Setenv("DOCKER_HOST", docker.URL)
			t.Setenv("DOCKER_API_VERSION", testDockerAPIVersion)
			t.Setenv("DOCKER_TLS_VERIFY", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			err := runContainers(t.Context(), RunParams{
				Job: &model.Job{}, ProxyImage: "proxy", UpdaterImage: "unused-updater",
				CollectorImage: "collector", CollectorConfigPath: filepath.Join(t.TempDir(), "collector.yaml"),
				SkipImageVerification: true, Debug: true,
			})
			if err == nil || !strings.Contains(err.Error(), "collector "+stage+" denied") {
				t.Fatalf("runContainers() = %v, want Collector %s failure", err, stage)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(created, ",") != "proxy,collector" || !removedProxy || removedNetworks != 2 {
				t.Fatalf("created = %v, proxy removed = %v, networks removed = %d", created, removedProxy, removedNetworks)
			}
		})
	}
}

func TestRunContainersExplicitVerificationBypass(t *testing.T) {
	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1.44/networks/create" {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(t, w, map[string]string{"message": "stopped after explicit bypass"})
			return
		}
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", testDockerAPIVersion)
			fmt.Fprint(w, "OK")
			return
		}
		t.Errorf("unexpected verification request during bypass: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer docker.Close()
	t.Setenv("DOCKER_HOST", docker.URL)
	t.Setenv("DOCKER_API_VERSION", testDockerAPIVersion)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	err := runContainers(t.Context(), RunParams{
		Job: &model.Job{}, ProxyImage: "unsigned-proxy", UpdaterImage: "unsigned-updater",
		CollectorImage: "unsigned-collector", CollectorConfigPath: filepath.Join(t.TempDir(), "collector.yaml"),
		SkipImageVerification: true,
	})
	if err == nil || !strings.Contains(err.Error(), "stopped after explicit bypass") {
		t.Fatalf("runContainers() = %v, want legacy execution after explicit bypass", err)
	}
	if !strings.Contains(output.String(), "WARNING: updater, proxy, and OpenTelemetry collector image verification is disabled") {
		t.Fatalf("missing bypass warning: %s", output.String())
	}
}

func Test_RunParamsValidateCooldown(t *testing.T) {
	days := func(n int) *int { return &n }

	tests := []struct {
		name     string
		cooldown *model.UpdateCooldown
		wantErr  bool
	}{
		{"no cooldown", nil, false},
		{"zero is allowed", &model.UpdateCooldown{DefaultDays: days(0)}, false},
		{"positive is allowed", &model.UpdateCooldown{DefaultDays: days(7)}, false},
		{"negative is rejected", &model.UpdateCooldown{DefaultDays: days(-1)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &RunParams{
				Job: &model.Job{PackageManager: "go_modules", UpdateCooldown: tt.cooldown},
			}
			if err := params.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestCaseInsensitiveRepoSetupCommand(t *testing.T) {
	t.Run("configures the CIFS staging path for NuGet experiment", func(t *testing.T) {
		job := &model.Job{
			PackageManager: "nuget",
			Experiments: model.Experiment{
				"use_case_insensitive_filesystem": true,
			},
		}
		expected := "git config --global --add safe.directory " + caseSensitiveRepoContentsPath
		if got := caseInsensitiveRepoSetupCommand(job); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("does not configure normal jobs", func(t *testing.T) {
		if got := caseInsensitiveRepoSetupCommand(&model.Job{PackageManager: "nuget"}); got != "" {
			t.Errorf("expected no setup command, got %q", got)
		}
	})
}

type checkedCommandRunnerFunc func(context.Context, string, string, ...string) error

func (f checkedCommandRunnerFunc) runCmdChecked(ctx context.Context, cmd, user string, env ...string) error {
	return f(ctx, cmd, user, env...)
}

func TestConfigureCaseInsensitiveRepoPropagatesCommandFailure(t *testing.T) {
	job := &model.Job{
		PackageManager: "nuget",
		Experiments: model.Experiment{
			"use_case_insensitive_filesystem": true,
		},
	}
	execErr := errors.New("exec exited with code 127: git: not found")
	runner := checkedCommandRunnerFunc(func(_ context.Context, cmd, user string, _ ...string) error {
		expected := "git config --global --add safe.directory " + caseSensitiveRepoContentsPath
		if cmd != expected {
			t.Errorf("command = %q, want %q", cmd, expected)
		}
		if user != dependabot {
			t.Errorf("user = %q, want %q", user, dependabot)
		}
		return execErr
	})

	err := configureCaseInsensitiveRepo(context.Background(), runner, job)
	if !errors.Is(err, execErr) {
		t.Fatalf("configureCaseInsensitiveRepo error = %v, want wrapped %v", err, execErr)
	}
}

func TestUpdateUpdaterCertificatesPropagatesCommandFailure(t *testing.T) {
	execErr := errors.New("exec exited with code 1")
	runner := checkedCommandRunnerFunc(func(_ context.Context, cmd, user string, _ ...string) error {
		if cmd != "update-ca-certificates" {
			t.Errorf("command = %q, want update-ca-certificates", cmd)
		}
		if user != root {
			t.Errorf("user = %q, want %q", user, root)
		}
		return execErr
	})

	err := updateUpdaterCertificates(context.Background(), runner)
	if !errors.Is(err, execErr) {
		t.Fatalf("updateUpdaterCertificates error = %v, want wrapped %v", err, execErr)
	}
}

func TestCreateLocalStagingDirPropagatesCommandFailure(t *testing.T) {
	execErr := errors.New("exec exited with code 1")
	runner := checkedCommandRunnerFunc(func(_ context.Context, cmd, user string, _ ...string) error {
		expected := "mkdir -p " + guestRepoDir
		if cmd != expected {
			t.Errorf("command = %q, want %q", cmd, expected)
		}
		if user != dependabot {
			t.Errorf("user = %q, want %q", user, dependabot)
		}
		return execErr
	})

	err := createLocalStagingDir(context.Background(), runner, guestRepoDir)
	if !errors.Is(err, execErr) {
		t.Fatalf("createLocalStagingDir error = %v, want wrapped %v", err, execErr)
	}
}

func TestInitializeLocalRepositoryPropagatesCommandFailure(t *testing.T) {
	execErr := errors.New("exec exited with code 1")
	runner := checkedCommandRunnerFunc(func(_ context.Context, cmd, user string, _ ...string) error {
		for _, required := range []string{"git init", "git config user.email", "git add .", "git commit"} {
			if !strings.Contains(cmd, required) {
				t.Errorf("command %q does not contain %q", cmd, required)
			}
		}
		if user != dependabot {
			t.Errorf("user = %q, want %q", user, dependabot)
		}
		return execErr
	})

	err := initializeLocalRepository(context.Background(), runner, guestRepoDir)
	if !errors.Is(err, execErr) {
		t.Fatalf("initializeLocalRepository error = %v, want wrapped %v", err, execErr)
	}
}

func Test_checkCredAccess(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Failed to create listener: ", err.Error())
	}

	testServer := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "repo, write:packages")
			_, _ = w.Write([]byte("SUCCESS"))
		}),
	}
	go func() {
		_ = testServer.Serve(l)
	}()

	t.Cleanup(func() {
		testServer.Shutdown(context.Background())
		l.Close()
	})

	addr := fmt.Sprintf("http://127.0.0.1:%v", l.Addr().(*net.TCPAddr).Port)

	t.Run("returns error if the credential has write access", func(t *testing.T) {
		defaultApiEndpoint = addr

		credentials := []model.Credential{{
			"token": "ghp_fake",
		}}
		err := checkCredAccess(context.Background(), nil, credentials)
		if !errors.Is(err, ErrWriteAccess) {
			t.Error("unexpected error", err)
		}
	})

	t.Run("it works with GitHub Enterprise", func(t *testing.T) {
		defaultApiEndpoint = "http://example.com" // ensure it's not used

		credentials := []model.Credential{{
			"token": "ghp_fake",
		}}
		job := &model.Job{Source: model.Source{APIEndpoint: &addr}}
		err := checkCredAccess(context.Background(), job, credentials)
		if !errors.Is(err, ErrWriteAccess) {
			t.Error("unexpected error", err)
		}
	})
}

func Test_expandEnvironmentVariables(t *testing.T) {
	t.Run("keeps resolved API credentials literal and out of smoke test output", func(t *testing.T) {
		t.Setenv("SECRET", "expanded")
		api := &server.API{}
		params := &RunParams{
			CredentialsResolved: true,
			Creds: []model.Credential{{
				"type": "npm_registry", "token": "literal$SECRET",
			}},
		}
		expandEnvironmentVariables(api, params)
		if got := params.Creds[0]["token"]; got != "literal$SECRET" {
			t.Fatalf("resolved credential changed: %v", got)
		}
		if len(api.Actual.Input.Credentials) != 0 {
			t.Fatal("resolved credentials must not be included in smoke test output")
		}
	})

	t.Run("injects environment variables", func(t *testing.T) {
		os.Setenv("ENV1", "value1")
		os.Setenv("ENV2", "value2")
		api := &server.API{}
		params := &RunParams{
			Creds: []model.Credential{{
				"type":     "test",
				"url":      "url",
				"username": "$ENV1",
				"pass":     "$ENV2",
			}},
		}

		expandEnvironmentVariables(api, params)

		if params.Creds[0]["username"] != "value1" {
			t.Error("expected username to be injected", params.Creds[0]["username"])
		}
		if params.Creds[0]["pass"] != "value2" {
			t.Error("expected pass to be injected", params.Creds[0]["pass"])
		}
		if api.Actual.Input.Credentials[0]["username"] != "$ENV1" {
			t.Error("expected username NOT to be injected", api.Actual.Input.Credentials[0]["username"])
		}
		if api.Actual.Input.Credentials[0]["pass"] != "$ENV2" {
			t.Error("expected pass NOT to be injected", api.Actual.Input.Credentials[0]["pass"])
		}
	})
}

func TestRunContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunContext(ctx, RunParams{Job: &model.Job{PackageManager: "npm_and_yarn"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunContext() = %v, want context.Canceled", err)
	}
}

func Test_generateIgnoreConditions(t *testing.T) {
	const (
		outputFileName = "test_output"
		dependencyName = "dep1"
		version        = "1.0.0"
	)

	t.Run("generates ignore conditions", func(t *testing.T) {
		runParams := &RunParams{
			Output: outputFileName,
		}
		v := "1.0.0"
		actual := &model.SmokeTest{
			Output: []model.Output{{
				Type: "create_pull_request",
				Expect: model.UpdateWrapper{Data: model.CreatePullRequest{
					Dependencies: []model.Dependency{{
						Name:    dependencyName,
						Version: &v,
					}},
				}},
			}},
		}
		if err := generateIgnoreConditions(runParams, actual); err != nil {
			t.Fatal(err)
		}
		if len(actual.Input.Job.IgnoreConditions) != 1 {
			t.Error("expected 1 ignore condition to be generated, got", len(actual.Input.Job.IgnoreConditions))
		}
		ignore := actual.Input.Job.IgnoreConditions[0]
		if reflect.DeepEqual(ignore, &model.Condition{
			DependencyName:     dependencyName,
			Source:             outputFileName,
			VersionRequirement: ">" + version,
		}) {
			t.Error("unexpected ignore condition", ignore)
		}
	})

	t.Run("handles removed dependency", func(t *testing.T) {
		runParams := &RunParams{
			Output: outputFileName,
		}
		actual := &model.SmokeTest{
			Output: []model.Output{{
				Type: "create_pull_request",
				Expect: model.UpdateWrapper{Data: model.CreatePullRequest{
					Dependencies: []model.Dependency{{
						Name:    dependencyName,
						Removed: true,
					}},
				}},
			}},
		}
		if err := generateIgnoreConditions(runParams, actual); err != nil {
			t.Fatal(err)
		}
		if len(actual.Input.Job.IgnoreConditions) != 0 {
			t.Error("expected 0 ignore condition to be generated, got", len(actual.Input.Job.IgnoreConditions))
		}
	})

	t.Run("skips git SHA versions", func(t *testing.T) {
		runParams := &RunParams{
			Output: outputFileName,
		}
		semver := "1.0.0"
		sha := "8b27c1239e5c421a2bbc2c65d52e4a6fbf2ff296"
		actual := &model.SmokeTest{
			Output: []model.Output{{
				Type: "create_pull_request",
				Expect: model.UpdateWrapper{Data: model.CreatePullRequest{
					Dependencies: []model.Dependency{
						{Name: "semver-dep", Version: &semver},
						{Name: "sha-dep", Version: &sha},
					},
				}},
			}},
		}
		if err := generateIgnoreConditions(runParams, actual); err != nil {
			t.Fatal(err)
		}
		if len(actual.Input.Job.IgnoreConditions) != 1 {
			t.Fatalf("expected 1 ignore condition, got %d", len(actual.Input.Job.IgnoreConditions))
		}
		if actual.Input.Job.IgnoreConditions[0].DependencyName != "semver-dep" {
			t.Errorf("expected semver-dep, got %q", actual.Input.Job.IgnoreConditions[0].DependencyName)
		}
	})
}

func Test_updaterCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  model.RunCommand
		expected string
	}{
		{"update", model.UpdateFilesCommand, "bin/run fetch_files && bin/run update_files"},
		{"graph", model.UpdateGraphCommand, "bin/run fetch_files && bin/run update_graph"},
		{"unmapped command falls through to a bare bin/run", "reachability", "bin/run"},
		{"unrecognized command is never a no-op", "not-a-real-command", "bin/run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updaterCommand(tt.command); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestCopyLocalDirSetsUpdaterOwnership(t *testing.T) {
	root := t.TempDir()
	nestedDir := filepath.Join(root, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "file.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatal(err)
	}

	var headers []*tar.Header
	var fileContents string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasSuffix(r.URL.Path, "/containers/updater-id/archive") {
			http.NotFound(w, r)
			return
		}
		tarReader := tar.NewReader(r.Body)
		for {
			header, err := tarReader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Errorf("reading archive: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			headerCopy := *header
			headers = append(headers, &headerCopy)
			if header.Name == "nested/file.txt" {
				contents, err := io.ReadAll(tarReader)
				if err != nil {
					t.Errorf("reading file contents: %v", err)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				fileContents = string(contents)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.44"),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(workingDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyLocalDir(context.Background(), cli, "updater-id", relativeRoot, guestRepoDir, 4096, 4096); err != nil {
		t.Fatal(err)
	}

	foundFile := false
	for _, header := range headers {
		if header.Uid != 4096 || header.Gid != 4096 {
			t.Errorf("%s ownership = %d:%d, want 4096:4096", header.Name, header.Uid, header.Gid)
		}
		if header.Name == "nested/file.txt" {
			if fileContents != "contents" {
				t.Errorf("file contents = %q, want %q", fileContents, "contents")
			}
			foundFile = true
		}
	}
	if !foundFile {
		t.Error("archive did not contain nested/file.txt")
	}
}
