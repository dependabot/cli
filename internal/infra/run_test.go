package infra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/server"

	"github.com/dependabot/cli/internal/model"
)

func Test_checkCredAccess(t *testing.T) {
	l, err := net.Listen("tcp", ":0")
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
}

func Test_hasCredentials(t *testing.T) {
	t.Run("returns false when credentials are nil", func(t *testing.T) {
		params := RunParams{
			Creds: nil,
		}
		hasCredentials := len(params.Creds) > 0
		if hasCredentials {
			t.Error("expected hasCredentials to be false with nil credentials")
		}
	})

	t.Run("returns false when credentials are empty", func(t *testing.T) {
		params := RunParams{
			Creds: []model.Credential{},
		}
		hasCredentials := len(params.Creds) > 0
		if hasCredentials {
			t.Error("expected hasCredentials to be false with empty credentials")
		}
	})

	t.Run("returns true when credentials exist", func(t *testing.T) {
		params := RunParams{
			Creds: []model.Credential{{
				"type":  "test",
				"token": "test_token",
			}},
		}
		hasCredentials := len(params.Creds) > 0
		if !hasCredentials {
			t.Error("expected hasCredentials to be true with credentials")
		}
	})
}
