package tests

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntegration(t *testing.T) {
	// build the binary for the rest of the tests
	_, filename, _, _ := runtime.Caller(0)
	testPath := filepath.Dir(filename)
	cliMain := path.Join(testPath, "../cmd/dependabot/dependabot.go")

	if data, err := exec.Command("go", "build", cliMain).CombinedOutput(); err != nil {
		t.Fatal("Failed to build the binary: ", string(data))
	}
	defer func() {
		_ = os.Remove("dependabot")
	}()

	// Helper to run dependabot in the right directory
	dependabot := func(args ...string) (string, error) {
		cmd := exec.Command("./dependabot", args...)
		cmd.Dir = testPath
		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	t.Run("works with valid commits", func(t *testing.T) {
		if output, err := dependabot("update", "-f", "../testdata/valid-commit.yml"); err != nil {
			t.Fatal("Expected no error, but got: ", output)
		}
	})

	t.Run("rejects invalid commits", func(t *testing.T) {
		output, err := dependabot("update", "-f", "../testdata/invalid-commit.yml")
		if err == nil {
			t.Fatal("Expected an error, but got none")
		}
		if !strings.Contains(output, "commit must be a SHA, or not provided") {
			t.Fatalf("Expected error message to mention bad commit, but got: \n%s", output)
		}
	})
}
