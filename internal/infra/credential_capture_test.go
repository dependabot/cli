package infra

import (
	"testing"

	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/server"
)

func TestExplicitCredentialsAreNotCaptured(t *testing.T) {
	t.Setenv("REGISTRY_TOKEN", "resolved-token")
	for _, resolved := range []bool{false, true} {
		params := RunParams{
			Creds:               []model.Credential{{"type": "npm_registry", "token": "$REGISTRY_TOKEN"}},
			CredentialsResolved: resolved, OmitCredentials: true,
		}
		api := &server.API{}
		expandEnvironmentVariables(api, &params)
		if len(api.Actual.Input.Credentials) != 0 {
			t.Fatal("explicit credentials were included in scenario capture")
		}
		want := "resolved-token"
		if resolved {
			want = "$REGISTRY_TOKEN"
		}
		if params.Creds[0]["token"] != want {
			t.Fatal("credential resolution did not preserve its source semantics")
		}
	}
}
