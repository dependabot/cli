package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"rsc.io/script"
	"rsc.io/script/scripttest"
	"testing"
	"time"
)

func TestDependabot(t *testing.T) {
	err := exec.Command("go", "build", ".").Run()
	if err != nil {
		t.Fatal("failed to build dependabot")
	}
	t.Cleanup(func() {
		os.Remove("dependabot")
	})

	ctx := context.Background()
	engine := &script.Engine{
		Conds: scripttest.DefaultConds(),
		Cmds:  Commands(),
		Quiet: !testing.Verbose(),
	}
	env := []string{
		"PATH=" + os.Getenv("PATH"),
	}
	scripttest.Test(t, ctx, engine, env, "../../testdata/scripts/*.txt")
}

// Commands returns the commands that can be used in the scripts.
// Each line of the scripts are <command> <args...>
// When you use "echo" in the scripts it's actually running script.Echo
// not the echo binary on your system.
func Commands() map[string]script.Cmd {
	commands := scripttest.DefaultCmds()
	wd, _ := os.Getwd()
	dependabot := filepath.Join(wd, "dependabot")

	// additional Dependabot commands
	commands["dependabot"] = script.Program(dependabot, nil, 100*time.Millisecond)

	return commands
}
