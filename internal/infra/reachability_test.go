package infra

import (
	"strings"
	"testing"

	"github.com/dependabot/cli/internal/model"
)

// reachRunCommand should add --repo when the job has a source repo, so reach
// fetches the alerts + SBOM itself; and omit it otherwise (pre-staged inputs).
func TestReachRunCommandRepo(t *testing.T) {
	withRepo := reachRunCommand(ReachabilityParams{
		Job:        &model.Job{Source: model.Source{Repo: "octo/repo"}},
		CodeqlPath: "/opt/codeql/codeql",
	})
	if !strings.Contains(withRepo, "--repo octo/repo") {
		t.Errorf("expected --repo octo/repo in %q", withRepo)
	}
	if !strings.Contains(withRepo, "--codeql /opt/codeql/codeql") {
		t.Errorf("expected --codeql in %q", withRepo)
	}

	noRepo := reachRunCommand(ReachabilityParams{Job: &model.Job{}})
	if strings.Contains(noRepo, "--repo") {
		t.Errorf("did not expect --repo in %q", noRepo)
	}
}

// reachAPIEnv should surface a token from the environment as GH_TOKEN and, for
// GHES, the API endpoint as GITHUB_API_URL.
func TestReachAPIEnv(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "")

	ghes := "https://ghe.example.com/api/v3"
	env := reachAPIEnv(&model.Job{Source: model.Source{APIEndpoint: &ghes}})

	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GH_TOKEN=secret") {
		t.Errorf("expected GH_TOKEN=secret in %v", env)
	}
	if !strings.Contains(joined, "GITHUB_API_URL="+ghes) {
		t.Errorf("expected GITHUB_API_URL in %v", env)
	}
}

// With no token in the environment and no GHES endpoint, reachAPIEnv is empty
// (reach falls back to unauthenticated / the public API base).
func TestReachAPIEnvEmpty(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "")
	if env := reachAPIEnv(&model.Job{}); len(env) != 0 {
		t.Errorf("expected empty env, got %v", env)
	}
}
