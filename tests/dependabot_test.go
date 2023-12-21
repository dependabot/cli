package tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"rsc.io/script"
	"rsc.io/script/scripttest"
	"testing"
)

func TestDependabot(t *testing.T) {
	ctx := context.Background()
	engine := &script.Engine{
		Conds: scripttest.DefaultConds(),
		Cmds:  Commands(),
		Quiet: !testing.Verbose(),
	}
	env := os.Environ()
	scripttest.Test(t, ctx, engine, env, "../testdata/scripts/*.txt")
}

// Commands returns the commands that can be used in the scripts.
// Each line of the scripts are <command> <args...>
// So if you enter "dependabot update go_modules rsc/quote", it will run
// the Dependabot() function with args "update go_modules rsc/quote".
// When you use "echo" in the scripts it's actually running the echo command
// from the scripttest package.
func Commands() map[string]script.Cmd {
	commands := scripttest.DefaultCmds()

	// additional Dependabot commands
	commands["dependabot"] = Dependabot()

	return commands
}

// Dependabot runs the Dependabot CLI. TODO Should this build once then execute thereafter?
func Dependabot() script.Cmd {
	return script.Command(
		script.CmdUsage{
			Summary: "runs the Dependabot CLI",
			Args:    "[<package_manager> <repo> | -f <input.yml>] [flags]",
		},
		func(s *script.State, args ...string) (script.WaitFunc, error) {
			if len(args) == 0 {
				return nil, script.ErrUsage
			}

			args = append([]string{"run", "../cmd/dependabot/dependabot.go"}, args...)
			execCmd := exec.Command("go", args...)

			var execOut, execErr bytes.Buffer
			execCmd.Stdout = &execOut
			execCmd.Stderr = &execErr

			if err := execCmd.Start(); err != nil {
				return nil, fmt.Errorf("failed to run dependabot: %w", err)
			}

			wait := func(*script.State) (stdout, stderr string, err error) {
				err = execCmd.Wait()
				return execOut.String(), execErr.String(), err
			}
			return wait, nil
		})
}
