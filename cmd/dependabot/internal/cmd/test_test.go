package cmd

import (
	"github.com/dependabot/cli/internal/infra"
	"testing"
)

func TestTestCommand(t *testing.T) {
	t.Cleanup(func() {
		executeTestJob = infra.Run
	})

	t.Run("Read a scenario file", func(t *testing.T) {
		var actualParams *infra.RunParams
		executeTestJob = func(params infra.RunParams) error {
			actualParams = &params
			return nil
		}
		cmd := NewTestCommand()
		err := cmd.ParseFlags([]string{"-f", "../../../../testdata/scenario.yml"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		err = cmd.RunE(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if actualParams == nil {
			t.Fatalf("expected params to be set")
		}
		if actualParams.InputName == "" {
			t.Errorf("expected input name to be set")
		}
		if actualParams.Job.PackageManager != "go_modules" {
			t.Errorf("expected package manager to be set")
		}
	})
}
