package cmd

import (
	"log"
	"os"
	"time"

	"github.com/dependabot/cli/internal/infra"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
)

type SharedFlags struct {
	file                string
	cache               string
	debugging           bool
	flamegraph          bool
	proxyCertPath       string
	collectorConfigPath string
	extraHosts          []string
	output              string
	pullImages          bool
	volumes             []string
	timeout             time.Duration
	local               string
}

// root flags
var (
	updaterImage   string
	proxyImage     string
	collectorImage string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dependabot <command> <subcommand> [flags]",
	Short: "Dependabot end-to-end runner",
	Long:  `Run Dependabot jobs from the command line.`,
	Example: heredoc.Doc(`
        $ dependabot update go_modules rsc/quote
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
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("    cli | ")

	rootCmd.PersistentFlags().StringVar(&updaterImage, "updater-image", "", "container image to use for the updater")
	rootCmd.PersistentFlags().StringVar(&proxyImage, "proxy-image", infra.ProxyImageName, "container image to use for the proxy")
	rootCmd.PersistentFlags().StringVar(&collectorImage, "collector-image", infra.CollectorImageName, "container image to use for the OpenTelemetry collector")
}
