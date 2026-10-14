package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
)

const testJob = `{"data":{"attributes":{"command":"update","package-manager":"npm_and_yarn","source":{"provider":"github","repo":"org/repo","hostname":"github.com","api-endpoint":"https://api.github.com","commit":"0123456789012345678901234567890123456789"},"credentials-metadata":[{"type":"git_source","host":"github.com"}]}}}`
const testCredentials = `{"data":{"attributes":{"credentials":[{"type":"git_source","host":"github.com","password":"target$TOKEN","accessible-repos":["org/repo"]},{"type":"git_source","host":"github.com","password":"dependency-secret","accessible-repos":["org/private-library"]},{"type":"npm_registry","token":"registry-secret"},{"type":"jit_access","endpoint":"/update_jobs/42/create_jit_access"}]}}}`

func TestRunHostedJob(t *testing.T) {
	t.Setenv("JOB_TOKEN", "")
	t.Setenv("DEPENDABOT_JOB_ID", "")
	var paths []string
	var output bytes.Buffer
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/prefix/update_jobs/42/details":
			if r.Header.Get("Authorization") != "job-secret" {
				t.Error("details did not use job token")
			}
			if !strings.Contains(output.String(), "::add-mask::job-secret") {
				t.Error("bootstrap token was not masked before fetching")
			}
			fmt.Fprint(w, testJob)
		case "/prefix/update_jobs/42/credentials":
			if r.Header.Get("Authorization") != "cred-secret" {
				t.Error("credentials did not use credential token")
			}
			fmt.Fprint(w, testCredentials)
		default:
			t.Errorf("unexpected callback: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cfg := config{
		apiURL: server.URL + "/prefix", jobID: "42", jobToken: "job-secret", credentialsToken: "cred-secret",
		updaterImage: "custom:job", proxyImage: "custom:proxy", pullImages: false,
		timeout: 3 * time.Hour, volumes: []string{"/github/workspace/cache:/cache"},
		updaterEnv: []string{"CUSTOM_CACHE_PATH=/cache"},
	}
	api := newJobClient(cfg)
	api.http = server.Client()
	called := false
	err := run(context.Background(), cfg, api, &output, func(ctx context.Context, params infra.RunParams) error {
		called = true
		if len(paths) != 2 {
			t.Fatal("runner invoked before fetching both responses")
		}
		if params.Job.Command != "update" || params.Job.Source.Hostname == nil || *params.Job.Source.Hostname != "github.com" || params.Job.Source.Commit != "0123456789012345678901234567890123456789" {
			t.Fatalf("job was changed: %+v", params.Job)
		}
		if len(params.Creds) != 4 || params.Creds[0]["password"] != "target$TOKEN" || !params.CredentialsResolved {
			t.Fatalf("credentials not passed intact: %+v", params.Creds)
		}
		for _, secret := range []string{"target$TOKEN", "dependency-secret", "registry-secret", "cred-secret"} {
			if !strings.Contains(output.String(), "::add-mask::"+secret+"\n") {
				t.Errorf("%q not masked before execution", secret)
			}
		}
		if params.ApiUrl != cfg.apiURL || params.UpdaterImage != cfg.updaterImage || params.ProxyImage != cfg.proxyImage ||
			params.PullImages || params.Timeout != cfg.timeout || params.StorageImage != infra.StorageImageName ||
			!reflect.DeepEqual(params.Volumes, cfg.volumes) || !reflect.DeepEqual(params.UpdaterEnvironmentVariables, cfg.updaterEnv) {
			t.Fatalf("incorrect runner options: %+v", params)
		}
		if params.Output != "" || params.Writer != nil {
			t.Error("hosted jobs must not emit smoke test files or fake API output")
		}
		if got := os.Getenv("JOB_TOKEN"); got != cfg.jobToken {
			t.Errorf("proxy token = %q", got)
		}
		if got := os.Getenv("DEPENDABOT_JOB_ID"); got != cfg.jobID {
			t.Errorf("job ID = %q", got)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("run() = %v, called = %v", err, called)
	}
}

func TestHostedJobFailures(t *testing.T) {
	for _, failure := range []string{"details", "credentials", "runner", "report", "complete", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("JOB_TOKEN", "")
			t.Setenv("DEPENDABOT_JOB_ID", "")
			var paths []string
			var report string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				endpoint := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				paths = append(paths, endpoint)
				switch endpoint {
				case "details":
					if failure == "details" {
						w.WriteHeader(http.StatusUnauthorized)
						fmt.Fprint(w, "do-not-log-response")
					} else {
						fmt.Fprint(w, testJob)
					}
				case "credentials":
					if failure == "credentials" {
						w.WriteHeader(http.StatusUnauthorized)
					} else {
						fmt.Fprint(w, testCredentials)
					}
				case "record_update_job_error":
					if r.Method != http.MethodPost || r.Header.Get("Authorization") != "job-secret" {
						t.Error("incorrect error callback")
					}
					body, _ := io.ReadAll(r.Body)
					report = string(body)
					if failure == "report" {
						w.WriteHeader(http.StatusForbidden)
					} else {
						w.WriteHeader(http.StatusNoContent)
					}
				case "mark_as_processed":
					if r.Method != http.MethodPatch {
						t.Error("incorrect completion method")
					}
					var body map[string]map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["data"]["base-commit-sha"] != "unknown" {
						t.Errorf("incorrect completion body: %v, %v", body, err)
					}
					if failure == "complete" {
						w.WriteHeader(http.StatusForbidden)
					} else {
						w.WriteHeader(http.StatusNoContent)
					}
				default:
					t.Errorf("unexpected endpoint %q", endpoint)
				}
			}))
			defer server.Close()
			cfg := config{apiURL: server.URL, jobID: "42", jobToken: "job-secret", credentialsToken: "cred-secret"}
			api := newJobClient(cfg)
			api.http = server.Client()
			var output bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := run(ctx, cfg, api, &output, func(context.Context, infra.RunParams) error {
				if failure == "cancel" {
					cancel()
					return context.Canceled
				}
				return errors.New("updater failed with registry-secret")
			})
			if err == nil {
				t.Fatal("failure was swallowed")
			}
			if strings.Contains(err.Error(), "do-not-log-response") || strings.Contains(err.Error(), "registry-secret") || strings.Contains(report, "registry-secret") {
				t.Fatalf("secret or response leaked: %v, %s", err, report)
			}
			switch failure {
			case "details":
				if !reflect.DeepEqual(paths, []string{"details"}) {
					t.Fatalf("reported failure before job entered processing: %v", paths)
				}
			case "cancel":
				if len(paths) != 2 {
					t.Fatalf("canceled job made callbacks: %v", paths)
				}
			case "report":
				if !strings.Contains(err.Error(), "report") || paths[len(paths)-1] != "record_update_job_error" {
					t.Fatalf("report failure ignored: %v, %v", err, paths)
				}
			default:
				if paths[len(paths)-1] != "mark_as_processed" || !strings.Contains(report, "actions_workflow_updater") {
					t.Fatalf("missing failure lifecycle: %v, %s", paths, report)
				}
				if failure == "complete" && !strings.Contains(err.Error(), "mark_as_processed") {
					t.Fatalf("completion failure ignored: %v", err)
				}
			}
		})
	}
}

func TestMaskEscapesWorkflowCommands(t *testing.T) {
	var output bytes.Buffer
	mask := newMasker(&output)
	mask.add("abc%\r\n::error::bad")
	mask.add("abc%\r\n::error::bad")
	if got, want := output.String(), "::add-mask::abc%25%0D%0A::error::bad\n"; got != want {
		t.Fatalf("mask output = %q, want %q", got, want)
	}

	if got := mask.redact("failure abc%\r\n::error::bad"); got != "failure ***" {
		t.Fatalf("redaction = %q", got)
	}
}

func TestMaskCredentialFields(t *testing.T) {
	var output bytes.Buffer
	mask := newMasker(&output)
	creds := []model.Credential{{
		"password": "password-value", "token": "token-value", "key": "key-value",
		"auth-key": "auth-key-value", "username": "username-value",
		"client-secret": "client-secret-value", "private-key": "private-key-value",
		"secret": "secret-value",
	}}
	if err := mask.credentials(creds); err != nil {
		t.Fatal(err)
	}
	for _, value := range creds[0] {
		secret := value.(string)
		if !strings.Contains(output.String(), "::add-mask::"+secret+"\n") || mask.redact(secret) != "***" {
			t.Errorf("credential was not masked/redacted: %s", secret)
		}
	}
}
