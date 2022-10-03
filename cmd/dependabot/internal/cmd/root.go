package cmd

import (
	"os"
	"time"

	"github.com/dependabot/cli/internal/infra"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
)

var (
	file       string
	cache      string
	debugging  bool
	extraHosts []string
	output     string
	pullImages bool
	volumes    []string
	timeout    time.Duration
	tempDir    string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dependabot <command> <subcommand> [flags]",
	Short: "Dependabot end-to-end runner",
	Long:  `Run Dependabot jobs from the command line.`,
	Example: heredoc.Doc(`
        $ dependabot update go_modules rsc/quote --dry-run
        $ dependabot test -f input.yml
	`),
	Version: Version(),
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&infra.UpdaterImageName, "updater-image", infra.UpdaterImageName, "container image to use for the updater")
	rootCmd.PersistentFlags().StringVar(&infra.ProxyImageName, "proxy-image", infra.ProxyImageName, "container image to use for the proxy")

	rootCmd.PersistentFlags().StringVar(&tempDir, "temp-dir", "tmp", "path to the temporary directory for the job")
}
