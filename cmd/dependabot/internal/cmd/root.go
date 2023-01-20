package cmd

import (
	"os"
	"time"

	"github.com/dependabot/cli/internal/infra"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
)

var (
	file          string
	cache         string
	debugging     bool
	proxyCertPath string
	extraHosts    []string
	output        string
	pullImages    bool
	volumes       []string
	timeout       time.Duration
	updaterImage  string
	proxyImage    string
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
	rootCmd.PersistentFlags().StringVar(&updaterImage, "updater-image", "", "container image to use for the updater")
	rootCmd.PersistentFlags().StringVar(&proxyImage, "proxy-image", infra.ProxyImageName, "container image to use for the proxy")
}
