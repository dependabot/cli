package cmd

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/model"
)

func Test_processInput(t *testing.T) {
	t.Run("initializes some fields", func(t *testing.T) {
		os.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "")

		var input model.Input
		processInput(&input, nil)

		if input.Job.ExistingPullRequests == nil {
			t.Error("expected existing pull requests to be initialized")
		}
		if input.Job.IgnoreConditions == nil {
			t.Error("expected ignore conditions to be initialized")
		}
		if input.Job.SecurityAdvisories == nil {
			t.Error("expected security advisories to be initialized")
		}
		if len(input.Credentials) != 0 {
			t.Fatal("expected NO credentials to be added")
		}
	})

	t.Run("adds git_source to credentials when local token is present", func(t *testing.T) {
		var input model.Input
		os.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "token")

		processInput(&input, nil)

		if len(input.Credentials) != 1 {
			t.Fatal("expected credentials to be added")
		}
		if !reflect.DeepEqual(input.Credentials[0], model.Credential{
			"type":     "git_source",
			"host":     "github.com",
			"username": "x-access-token",
			"password": "$LOCAL_GITHUB_ACCESS_TOKEN",
		}) {
			t.Error("expected credentials to be added")
		}
		if !reflect.DeepEqual(input.Job.CredentialsMetadata[0], model.Credential{
			"type": "git_source",
			"host": "github.com",
		}) {
			t.Error("expected credentials metadata to be added")
		}
	})

	t.Run("adds metadata when credentials are provided", func(t *testing.T) {
		var input model.Input
		input.Credentials = []model.Credential{
			{
				"type":          "git_source",
				"host":          "example.com",
				"registry":      "registry.example.com",
				"url":           "https://example.com",
				"python-index":  "https://example.com",
				"replaces-base": "true",

				// These values will not propagate to the metadata
				"password": "password",
				"token":    "token",
				"key":      "key",
				"auth-key": "auth-key",
			},
		}

		processInput(&input, nil)

		if len(input.Job.CredentialsMetadata) != 1 {
			t.Fatal("expected credentials metadata to be added")
		}
		if !reflect.DeepEqual(input.Job.CredentialsMetadata, []model.Credential{{
			"type":          "git_source",
			"host":          "example.com",
			"registry":      "registry.example.com",
			"url":           "https://example.com",
			"python-index":  "https://example.com",
			"replaces-base": "true",
		}}) {
			t.Error("expected credentials metadata to be added")
		}
	})

	t.Run("add Azure credentials when local token is present", func(t *testing.T) {
		var input = model.Input{
			Job: model.Job{
				PackageManager: "nuget",
				Source: model.Source{
					Repo:      "org/project/_git/repo",
					Directory: "/",
				},
			},
		}
		var flags = UpdateFlags{
			apiUrl: "https://dev.azure.com/org/project/_git/repo",
		}
		os.Setenv("LOCAL_AZURE_ACCESS_TOKEN", "token")

		processInput(&input, &flags)

		if len(input.Credentials) != 4 {
			t.Fatal("expected credentials to be added")
		}
		// Ensure all credentials are either git_source or azure
		for _, cred := range input.Credentials {
			if cred["type"] != "git_source" && cred["type"] != "nuget_feed" {
				t.Errorf("expected credentials to be either git_source or nuget_feed, got %s", cred["type"])
			}
		}
	})
}

func Test_extractInput(t *testing.T) {
	t.Run("test arguments", func(t *testing.T) {
		cmd := NewUpdateCommand()
		if err := cmd.ParseFlags([]string{"go_modules", "rsc/quote"}); err != nil {
			t.Fatal(err)
		}
		input, err := extractInput(cmd, &UpdateFlags{})
		if err != nil {
			t.Fatal(err)
		}
		if input.Job.PackageManager != "go_modules" {
			t.Errorf("expected package manager to be go_modules, got %s", input.Job.PackageManager)
		}
	})
	t.Run("test file", func(t *testing.T) {
		cmd := NewUpdateCommand()
		input, err := extractInput(cmd, &UpdateFlags{
			SharedFlags: SharedFlags{
				// The working directory is cmd/dependabot/internal/cmd
				file: "../../../../testdata/basic.yml",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if input.Job.PackageManager != "go_modules" {
			t.Errorf("expected package manager to be go_modules, got %s", input.Job.PackageManager)
		}
	})
	t.Run("test server", func(t *testing.T) {
		go func() {
			// Retry the calls in case the server takes a bit to start up.
			for i := 0; i < 10; i++ {
				body := strings.NewReader(`{"job":{"package-manager":"go_modules"}}`)
				_, err := http.Post("http://127.0.0.1:8080", "application/json", body)
				if err != nil {
					time.Sleep(10 * time.Millisecond)
				} else {
					return
				}
			}
		}()

		cmd := NewUpdateCommand()
		input, err := extractInput(cmd, &UpdateFlags{
			inputServerPort: 8080,
		})
		if err != nil {
			t.Fatal(err)
		}
		if input.Job.PackageManager != "go_modules" {
			t.Errorf("expected package manager to be go_modules, got %s", input.Job.PackageManager)
		}
	})
	t.Run("test stdin", func(t *testing.T) {
		tmp, err := os.CreateTemp("", "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(tmp.Name()) })

		_, err = tmp.WriteString(`{"job":{"package-manager":"go_modules"}}`)
		if err != nil {
			t.Fatal(err)
		}
		tmp.Close()

		// This test changes os.Stdin, which contains global state, so ensure we reset it after the test
		originalStdIn := os.Stdin
		t.Cleanup(func() { os.Stdin = originalStdIn })
		os.Stdin, err = os.Open(tmp.Name())
		if err != nil {
			t.Fatal(err)
		}

		cmd := NewUpdateCommand()
		input, err := extractInput(cmd, &UpdateFlags{})
		if err != nil {
			t.Fatal(err)
		}
		if input.Job.PackageManager != "go_modules" {
			t.Errorf("expected package manager to be go_modules, got %s", input.Job.PackageManager)
		}
	})
	t.Run("test too many input types", func(t *testing.T) {
		cmd := NewUpdateCommand()
		if err := cmd.ParseFlags([]string{"go_modules", "-f", "basic.yml"}); err != nil {
			t.Fatal(err)
		}
		_, err := extractInput(cmd, &UpdateFlags{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}
