package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
)

func TestGraphHostedCredentialsKeepCallbacksLocal(t *testing.T) {
	t.Setenv("DEPENDABOT_CREDENTIALS_TOKEN", "opaque-credentials-token")
	t.Setenv("JOB_TOKEN", "opaque-job-token")
	t.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "must-not-be-used")
	previousRun, previousFetch := executeGraphJob, fetchGraphCredentials
	t.Cleanup(func() { executeGraphJob, fetchGraphCredentials = previousRun, previousFetch })
	fetchGraphCredentials = func(_ context.Context, endpoint, token string) ([]model.Credential, error) {
		if endpoint != "https://api.example/update_jobs/1/credentials" || token != "opaque-credentials-token" {
			t.Error("incorrect credential request")
		}
		return []model.Credential{{"type": "npm_registry", "registry": "registry.example", "token": "$literal-token"}}, nil
	}
	var called bool
	executeGraphJob = func(p infra.RunParams) error {
		called = true
		if p.ApiUrl != "" || p.ProxyApiUrl != "https://api.example" || !p.CredentialsResolved {
			t.Error("credential lookup must not redirect graph callbacks or expand resolved secrets")
		}
		if len(p.Creds) != 1 || p.Creds[0]["token"] != "$literal-token" {
			t.Error("hosted credentials must not include ambient local credentials")
		}
		if p.Job.Command != model.UpdateGraphCommand || p.Job.Experiments["enable_dependency_submission_poc"] != true {
			t.Error("must execute graph with dependency submission enabled")
		}
		if len(p.Job.CredentialsMetadata) != 1 || p.Job.CredentialsMetadata[0]["token"] != nil {
			t.Error("updater metadata must not contain secrets")
		}
		return nil
	}
	command := NewGraphCommand()
	command.SetArgs([]string{"npm_and_yarn", "owner/repo", "--credentials-url", "https://api.example/update_jobs/1/credentials", "--proxy-api-url", "https://api.example"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("graph runner was not called")
	}
}

func TestGraphCredentialFailureDoesNotRun(t *testing.T) {
	t.Setenv("DEPENDABOT_CREDENTIALS_TOKEN", "opaque-token")
	previousRun, previousFetch := executeGraphJob, fetchGraphCredentials
	t.Cleanup(func() { executeGraphJob, fetchGraphCredentials = previousRun, previousFetch })
	expected := errors.New("credentials denied")
	fetchGraphCredentials = func(context.Context, string, string) ([]model.Credential, error) { return nil, expected }
	executeGraphJob = func(infra.RunParams) error {
		t.Fatal("must not run without credentials")
		return nil
	}

	command := NewGraphCommand()
	command.SetArgs([]string{"npm_and_yarn", "owner/repo", "--credentials-url", "https://api.example/credentials"})
	if err := command.Execute(); !errors.Is(err, expected) {
		t.Fatalf("expected credential error, got %v", err)
	}
}

func TestGraphHostedCredentialsRejectOtherCommands(t *testing.T) {
	previousFetch := fetchGraphCredentials
	t.Cleanup(func() { fetchGraphCredentials = previousFetch })
	fetchGraphCredentials = func(context.Context, string, string) ([]model.Credential, error) {
		return []model.Credential{}, nil
	}
	input := &model.Input{Job: model.Job{Command: model.UpdateFilesCommand}}
	if err := loadGraphCredentials(context.Background(), input, "https://api.example/credentials", "", "", ""); err == nil {
		t.Fatal("hosted graph credentials must not execute an update job")
	}
}

func TestGraphLocalCredentialFile(t *testing.T) {
	t.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "")
	previousRun := executeGraphJob
	t.Cleanup(func() { executeGraphJob = previousRun })
	file := filepath.Join(t.TempDir(), "credentials.yml")
	if err := os.WriteFile(file, []byte("credentials:\n- type: npm_registry\n  registry: registry.example\n  token: $NPM_TOKEN\n"), 0600); err != nil {
		t.Fatal(err)
	}

	executeGraphJob = func(p infra.RunParams) error {
		if p.CredentialsResolved || len(p.Creds) != 1 || p.Creds[0]["token"] != "$NPM_TOKEN" {
			t.Error("local credential references must retain normal environment expansion")
		}
		if !p.OmitCredentials {
			t.Error("explicit credentials must be omitted from the captured scenario")
		}
		return nil
	}
	command := NewGraphCommand()
	command.SetArgs([]string{"npm_and_yarn", "owner/repo", "--credentials-file", file})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}
