package cmd

import (
	"testing"

	"github.com/dependabot/cli/internal/infra"
)

// TestReachabilityCommand verifies the reachability subcommand wires its flags
// into infra.ReachabilityParams, using the executeReachability seam so no Docker
// is needed.
func TestReachabilityCommand(t *testing.T) {
	var captured infra.ReachabilityParams
	original := executeReachability
	executeReachability = func(params infra.ReachabilityParams) error {
		captured = params
		return nil
	}
	defer func() { executeReachability = original }()

	cmd := NewReachabilityCommand()
	cmd.SetArgs([]string{
		"--input-dir", "/tmp/inputs",
		"--reachability-image", "example/reach:latest",
		"--annotations", "annotations",
		"--codeql", "/opt/codeql/codeql",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	if captured.InputDir != "/tmp/inputs" {
		t.Errorf("InputDir = %q, want /tmp/inputs", captured.InputDir)
	}
	if captured.ReachabilityImage != "example/reach:latest" {
		t.Errorf("ReachabilityImage = %q, want example/reach:latest", captured.ReachabilityImage)
	}
	if captured.CodeqlPath != "/opt/codeql/codeql" {
		t.Errorf("CodeqlPath = %q, want /opt/codeql/codeql", captured.CodeqlPath)
	}
	if captured.Annotations != "annotations" {
		t.Errorf("Annotations = %q, want annotations", captured.Annotations)
	}
	if captured.Job == nil {
		t.Fatal("Job should be set")
	}
	if captured.Job.PackageManager == "" {
		t.Error("Job.PackageManager should default to a value")
	}
}

// TestReachabilityCommandRequiresInputs ensures the required flags are enforced.
func TestReachabilityCommandRequiresInputs(t *testing.T) {
	original := executeReachability
	executeReachability = func(params infra.ReachabilityParams) error { return nil }
	defer func() { executeReachability = original }()

	cmd := NewReachabilityCommand()
	cmd.SetArgs([]string{}) // no --input-dir / --reachability-image
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when required flags are missing")
	}
}
