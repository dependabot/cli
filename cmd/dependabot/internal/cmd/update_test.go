package cmd

import (
	"os"
	"reflect"
	"testing"

	"github.com/dependabot/cli/internal/model"
)

func Test_processInput(t *testing.T) {
	t.Run("initializes some fields", func(t *testing.T) {
		var input model.Input
		processInput(&input)

		if input.Job.ExistingPullRequests == nil {
			t.Error("expected existing pull requests to be initialized")
		}
		if input.Job.IgnoreConditions == nil {
			t.Error("expected ignore conditions to be initialized")
		}
		if input.Job.SecurityAdvisories == nil {
			t.Error("expected security advisories to be initialized")
		}
	})

	t.Run("injects environment variables", func(t *testing.T) {
		var input model.Input
		input.Credentials = []model.Credential{{
			"type":     "any",
			"host":     "host",
			"url":      "url",
			"username": "$ENV1",
			"pass":     "$ENV2",
		}}
		os.Setenv("ENV1", "value1")
		os.Setenv("ENV2", "value2")
		os.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "") // fixes test while running locally

		processInput(&input)

		if input.Credentials[0]["username"] != "value1" {
			t.Error("expected username to be injected")
		}
		if input.Credentials[0]["pass"] != "value2" {
			t.Error("expected pass to be injected")
		}
		if !reflect.DeepEqual(input.Job.CredentialsMetadata, []model.Credential{{
			"type": "any",
			"host": "host",
			"url":  "url",
		}}) {
			t.Error("expected credentials metadata to be to", input.Job.CredentialsMetadata)
		}
	})

	t.Run("adds git_source to credentials", func(t *testing.T) {
		var input model.Input
		os.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "token")
		// Adding a dummy metadata to test the inner if
		input.Job.CredentialsMetadata = []model.Credential{{}}

		processInput(&input)

		if len(input.Credentials) != 1 {
			t.Fatal("expected credentials to be added")
		}
		if !reflect.DeepEqual(input.Credentials[0], model.Credential{
			"type":     "git_source",
			"host":     "github.com",
			"username": "x-access-token",
			"password": "token",
		}) {
			t.Error("expected credentials to be added")
		}
		if !reflect.DeepEqual(input.Job.CredentialsMetadata[1], model.Credential{
			"type": "git_source",
			"host": "github.com",
		}) {
			t.Error("expected credentials metadata to be added")
		}
	})
}
