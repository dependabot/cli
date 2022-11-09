package cmd

import (
	"os"
	"reflect"
	"testing"

	"github.com/dependabot/cli/internal/model"
)

func Test_processInput(t *testing.T) {
	t.Run("initializes some fields", func(t *testing.T) {
		os.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "")

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
		if len(input.Credentials) != 0 {
			t.Fatal("expected NO credentials to be added")
		}
	})

	t.Run("adds git_source to credentials when local token is present", func(t *testing.T) {
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
			"password": "$LOCAL_GITHUB_ACCESS_TOKEN",
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
