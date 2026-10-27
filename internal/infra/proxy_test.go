package infra

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func TestNewProxyOptions(t *testing.T) {
	for _, stage := range []string{"legacy", "action", "certificate failure", "readiness failure"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("JOB_TOKEN", "job-secret")
			t.Setenv("HTTP_PROXY", "http://legacy.example")
			var env []string
			var config map[string]any
			var certificate string
			var ready, removed bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
					var cfg container.Config
					if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
						t.Error(err)
					}
					env = cfg.Env
					writeJSON(t, w, map[string]string{"Id": "proxy"})
				case r.Method == http.MethodPut && r.URL.Path == "/v1.44/containers/proxy/archive":
					archive := tar.NewReader(r.Body)
					header, err := archive.Next()
					if err != nil {
						t.Error(err)
						return
					}
					if strings.TrimPrefix(header.Name, "/") == strings.TrimPrefix(ConfigFilePath, "/") {
						if err := json.NewDecoder(archive).Decode(&config); err != nil {
							t.Error(err)
						}
					} else if strings.TrimPrefix(header.Name, "/") == strings.TrimPrefix(proxyCertPath, "/") {
						data, err := io.ReadAll(archive)
						if err != nil {
							t.Error(err)
						}
						certificate = string(data)
						if stage == "certificate failure" {
							w.WriteHeader(http.StatusInternalServerError)
							writeJSON(t, w, map[string]string{"message": "certificate copy denied"})
							return
						}
					} else {
						t.Errorf("unexpected archive: %s", header.Name)
					}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/proxy/start",
					r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/proxy/stop":
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/proxy/json":
					writeJSON(t, w, map[string]any{
						"Id": "proxy", "State": map[string]any{"Running": true, "ExitCode": 0},
					})
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/proxy/exec":
					ready = true
					var cfg container.ExecOptions
					if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
						t.Error(err)
					}
					if cfg.User != root || len(cfg.Cmd) != 7 || cfg.Cmd[0] != "timeout" || cfg.Cmd[5] != "127.0.0.1" || cfg.Cmd[6] != "1080" {
						t.Errorf("incorrect readiness command: %+v", cfg)
					}
					writeJSON(t, w, map[string]string{"Id": "ready"})
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/exec/ready/start":
					var cfg container.ExecStartOptions
					if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil || !cfg.Detach {
						t.Errorf("readiness check must run detached: %+v, %v", cfg, err)
					}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == "/v1.44/exec/ready/json":
					exitCode := 0
					if stage == "readiness failure" {
						exitCode = 124
					}
					writeJSON(t, w, map[string]any{"Running": false, "ExitCode": exitCode})
				case r.Method == http.MethodDelete && r.URL.Path == "/v1.44/containers/proxy":
					removed = true
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			cli, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithVersion(testDockerAPIVersion))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			params := RunParams{ApiUrl: "https://updater.example", Creds: []model.Credential{{"type": "npm_registry", "token": "registry-secret"}}}
			if stage != "legacy" {
				params.ProxyOptions = &ProxyOptions{
					APIURL: "https://bootstrap.example", Experiments: model.Experiment{"feature": true},
					CACert: []byte("custom CA"), HTTPProxy: "http://action.example",
					HTTPSProxy: "http://secure.example", NoProxy: "internal.example", WaitForReady: true,
				}
			}
			proxy, err := NewProxy(t.Context(), cli, &params, nil)
			if stage == "certificate failure" || stage == "readiness failure" {
				if err == nil || !removed || proxy != nil {
					t.Fatalf("failed proxy not cleaned up: proxy=%v, err=%v, removed=%v", proxy, err, removed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := proxy.Close(); err != nil {
				t.Fatal(err)
			}
			if stage == "legacy" {
				if _, ok := config["experiments"]; ok || certificate != "" || ready {
					t.Fatalf("action settings leaked into legacy proxy: config=%v, cert=%q, ready=%v", config, certificate, ready)
				}
				if value, _ := envValue(env, "DEPENDABOT_API_URL"); value != params.ApiUrl {
					t.Errorf("legacy API URL changed: %q", value)
				}
				if value, _ := envValue(env, "HTTP_PROXY"); value != "http://legacy.example" {
					t.Errorf("legacy proxy environment changed: %q", value)
				}
			} else {
				if !reflect.DeepEqual(config["experiments"], map[string]any{"feature": true}) || certificate != "custom CA" || !ready {
					t.Fatalf("missing action proxy settings: config=%v, cert=%q, ready=%v", config, certificate, ready)
				}
				for key, want := range map[string]string{
					"DEPENDABOT_API_URL": "https://bootstrap.example", "HTTP_PROXY": "http://action.example",
					"HTTPS_PROXY": "http://secure.example", "NO_PROXY": "internal.example",
				} {
					if got, _ := envValue(env, key); got != want {
						t.Errorf("%s = %q; want %q", key, got, want)
					}
				}
			}
		})
	}
}

func TestProxyReadinessCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/proxy/exec":
			writeJSON(t, w, map[string]string{"Id": "ready"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/exec/ready/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/exec/ready/json":
			writeJSON(t, w, map[string]any{"Running": true})
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	cli, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithVersion(testDockerAPIVersion))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	proxy := &Proxy{cli: cli, containerID: "proxy"}
	if err := proxy.waitUntilReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("readiness did not honor cancellation: %v", err)
	}
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, e := range env {
		if after, ok := strings.CutPrefix(e, prefix); ok {
			return after, true
		}
	}
	return "", false
}

func Test_proxyEnv_ProxyCache(t *testing.T) {
	t.Run("uses host PROXY_CACHE when set", func(t *testing.T) {
		t.Setenv("PROXY_CACHE", "false")

		env := proxyEnv("")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "false" {
			t.Errorf("expected PROXY_CACHE to be %q, got %q", "false", value)
		}
	})

	t.Run("falls back to true when host PROXY_CACHE is unset", func(t *testing.T) {
		t.Setenv("PROXY_CACHE", "placeholder")
		os.Unsetenv("PROXY_CACHE")

		env := proxyEnv("")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "true" {
			t.Errorf("expected PROXY_CACHE to fall back to %q, got %q", "true", value)
		}
	})

	t.Run("falls back to true when host PROXY_CACHE is empty", func(t *testing.T) {
		t.Setenv("PROXY_CACHE", "")

		env := proxyEnv("")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "true" {
			t.Errorf("expected PROXY_CACHE to fall back to %q, got %q", "true", value)
		}
	})
}

func Test_proxyEnv_OpenSSLForceFIPSMode(t *testing.T) {
	t.Run("passes OPENSSL_FORCE_FIPS_MODE from environment", func(t *testing.T) {
		t.Setenv("OPENSSL_FORCE_FIPS_MODE", "1")

		env := proxyEnv("")

		value, ok := envValue(env, "OPENSSL_FORCE_FIPS_MODE")
		if !ok {
			t.Fatal("expected OPENSSL_FORCE_FIPS_MODE to be present in proxy env")
		}
		if value != "1" {
			t.Errorf("expected OPENSSL_FORCE_FIPS_MODE to be %q, got %q", "1", value)
		}
	})

	t.Run("omits OPENSSL_FORCE_FIPS_MODE when unset", func(t *testing.T) {
		t.Setenv("OPENSSL_FORCE_FIPS_MODE", "placeholder")
		os.Unsetenv("OPENSSL_FORCE_FIPS_MODE")

		env := proxyEnv("")

		if _, ok := envValue(env, "OPENSSL_FORCE_FIPS_MODE"); ok {
			t.Error("expected OPENSSL_FORCE_FIPS_MODE to be absent from proxy env when host has it unset")
		}
	})
}

func Test_proxyEnv_JobID(t *testing.T) {
	for _, tt := range []struct {
		name  string
		jobID string
		unset bool
		want  string
	}{
		{name: "uses host job ID", jobID: "1588055836", want: "1588055836"},
		{name: "defaults when empty", want: "cli"},
		{name: "defaults when unset", unset: true, want: "cli"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEPENDABOT_JOB_ID", tt.jobID)
			if tt.unset {
				if err := os.Unsetenv("DEPENDABOT_JOB_ID"); err != nil {
					t.Fatal(err)
				}
			}

			env := proxyEnv("")

			value, ok := envValue(env, "JOB_ID")
			if !ok {
				t.Fatal("expected JOB_ID to be present in proxy env")
			}
			if value != tt.want {
				t.Errorf("expected JOB_ID to be %q, got %q", tt.want, value)
			}
		})
	}
}

func Test_proxyEnv_JobToken(t *testing.T) {
	t.Run("passes JOB_TOKEN from environment", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "super-secret-token")

		env := proxyEnv("")

		value, ok := envValue(env, "JOB_TOKEN")
		if !ok {
			t.Fatal("expected JOB_TOKEN to be present in proxy env")
		}
		if value != "super-secret-token" {
			t.Errorf("expected JOB_TOKEN to be %q, got %q", "super-secret-token", value)
		}
	})

	t.Run("omits JOB_TOKEN when unset", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "placeholder")
		os.Unsetenv("JOB_TOKEN")

		env := proxyEnv("")

		if _, ok := envValue(env, "JOB_TOKEN"); ok {
			t.Error("expected JOB_TOKEN to be absent from proxy env when host has it unset")
		}
	})

	t.Run("omits JOB_TOKEN when set to empty", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "")

		env := proxyEnv("")

		if _, ok := envValue(env, "JOB_TOKEN"); ok {
			t.Error("expected JOB_TOKEN to be absent from proxy env when host has it empty")
		}
	})
}

func Test_proxyEnv_DependabotAPIURL(t *testing.T) {
	t.Run("sets DEPENDABOT_API_URL from the apiURL parameter when JOB_TOKEN is set", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "super-secret-token")

		env := proxyEnv("https://api.example.com")

		value, ok := envValue(env, "DEPENDABOT_API_URL")
		if !ok {
			t.Fatal("expected DEPENDABOT_API_URL to be present when JOB_TOKEN is set")
		}
		if value != "https://api.example.com" {
			t.Errorf("expected DEPENDABOT_API_URL to be %q, got %q", "https://api.example.com", value)
		}
	})

	t.Run("omits DEPENDABOT_API_URL when JOB_TOKEN is unset", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "placeholder")
		os.Unsetenv("JOB_TOKEN")

		env := proxyEnv("https://api.example.com")

		if _, ok := envValue(env, "DEPENDABOT_API_URL"); ok {
			t.Error("expected DEPENDABOT_API_URL to be absent when JOB_TOKEN is unset")
		}
	})

	t.Run("omits DEPENDABOT_API_URL when JOB_TOKEN is empty", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "")

		env := proxyEnv("https://api.example.com")

		if _, ok := envValue(env, "DEPENDABOT_API_URL"); ok {
			t.Error("expected DEPENDABOT_API_URL to be absent when JOB_TOKEN is empty")
		}
	})
}
