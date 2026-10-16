package cmd

import (
	"github.com/dependabot/cli/internal/infra"
	"strconv"
	"testing"
)

func TestTestCommand(t *testing.T) {
	t.Cleanup(func() {
		executeTestJob = infra.Run
	})

	t.Run("Read a smoke test file", func(t *testing.T) {
		var actualParams *infra.RunParams
		executeTestJob = func(params infra.RunParams) error {
			actualParams = &params
			return nil
		}

		cmd := NewTestCommand()
		err := cmd.ParseFlags([]string{"-f", "../../../../testdata/smoke-test.yml"})
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

func TestTestCommandVerificationFlag(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("skip-image-verification")
	if flag == nil || flag.DefValue != "false" {
		t.Fatal("image verification must be enabled by default")
	}
	originalRunner, originalValue, originalChanged := executeTestJob, flag.Value.String(), flag.Changed
	t.Cleanup(func() {
		executeTestJob = originalRunner
		if err := flag.Value.Set(originalValue); err != nil {
			t.Error(err)
		}
		flag.Changed = originalChanged
	})
	for _, skip := range []bool{false, true} {
		t.Run(strconv.FormatBool(skip), func(t *testing.T) {
			if err := rootCmd.PersistentFlags().Set("skip-image-verification", strconv.FormatBool(skip)); err != nil {
				t.Fatal(err)
			}
			var actual *infra.RunParams
			executeTestJob = func(params infra.RunParams) error {
				actual = &params
				return nil
			}
			cmd := NewTestCommand()
			if err := cmd.ParseFlags([]string{"-f", "../../../../testdata/smoke-test.yml"}); err != nil {
				t.Fatal(err)
			}
			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatal(err)
			}
			if actual == nil || actual.SkipImageVerification != skip {
				t.Fatalf("skip flag %v not passed to runner: %+v", skip, actual)
			}
		})
	}
}
