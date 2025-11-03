package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
	"github.com/spf13/cobra"
)

var graphCmd = NewGraphCommand()

func init() {
	rootCmd.AddCommand(graphCmd)
}

func NewGraphCommand() *cobra.Command {
	var flags UpdateFlags

	cmd := &cobra.Command{
		Use:   "graph [<package_manager> <repo> | -f <input.yml>] [flags]",
		Short: "[Experimental] List the dependencies of a manifest/lockfile",
		Example: heredoc.Doc(`
		    NOTE: This command is a work in progress.

		    It will only work with some package managers and the dependency list
		    may be incomplete.

		    $ dependabot graph bundler dependabot/dependabot-core
		    $ dependabot graph bundler --local .
		    $ dependabot graph -f input.yml
	    `),
		RunE: func(cmd *cobra.Command, args []string) error {
			var outFile *os.File
			if flags.output != "" {
				var err error
				outFile, err = os.Create(flags.output)
				if err != nil {
					return fmt.Errorf("failed to create output file: %w", err)
				}
				defer outFile.Close()
			}

			input, err := extractInput(cmd, &flags)
			if err != nil {
				return err
			}

			processInput(input, &flags)

			// It doesn't make sense to suppress the graph output when running the graph command,
			// so forcing the experiment to true.
			if input.Job.Experiments == nil {
				input.Job.Experiments = make(map[string]any)
			}
			input.Job.Experiments["enable_dependency_submission_poc"] = true

			if input.Job.Command == "" {
				input.Job.Command = model.UpdateGraphCommand
			}

			var writer io.Writer
			if !flags.debugging {
				writer = os.Stdout
			}

			if err := infra.Run(infra.RunParams{
				CacheDir:            flags.cache,
				CollectorConfigPath: flags.collectorConfigPath,
				CollectorImage:      collectorImage,
				Creds:               input.Credentials,
				Debug:               flags.debugging,
				Flamegraph:          flags.flamegraph,
				Expected:            nil, // graph subcommand doesn't use expectations
				ExtraHosts:          flags.extraHosts,
				InputName:           flags.file,
				Job:                 &input.Job,
				LocalDir:            flags.local,
				Output:              flags.output,
				ProxyCertPath:       flags.proxyCertPath,
				ProxyImage:          proxyImage,
				PullImages:          flags.pullImages,
				Timeout:             flags.timeout,
				UpdaterImage:        updaterImage,
				Volumes:             flags.volumes,
				Writer:              writer,
				ApiUrl:              flags.apiUrl,
			}); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					log.Fatalf("update timed out after %s", flags.timeout)
				}
				log.Fatalf("updater failure: %v", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.file, "file", "f", "", "path to input file")

	cmd.Flags().StringVarP(&flags.provider, "provider", "p", "github", "provider of the repository")
	cmd.Flags().StringVarP(&flags.branch, "branch", "b", "", "target branch to update")
	cmd.Flags().StringVarP(&flags.directory, "directory", "d", "/", "directory to update")
	cmd.Flags().StringVarP(&flags.commit, "commit", "", "", "commit to update")

	cmd.Flags().StringVarP(&flags.output, "output", "o", "", "write scenario to file")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "cache import/export directory")
	cmd.Flags().StringVar(&flags.local, "local", "", "local directory to use as fetched source")
	cmd.Flags().StringVar(&flags.proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
	cmd.Flags().StringVar(&flags.collectorConfigPath, "collector-config", "", "path to an OpenTelemetry collector config file")
	cmd.Flags().BoolVar(&flags.pullImages, "pull", true, "pull the image if it isn't present")
	cmd.Flags().BoolVar(&flags.debugging, "debug", false, "run an interactive shell inside the updater")
	cmd.Flags().BoolVar(&flags.flamegraph, "flamegraph", false, "generate a flamegraph and other metrics")
	cmd.Flags().StringArrayVarP(&flags.volumes, "volume", "v", nil, "mount volumes in Docker")
	cmd.Flags().StringArrayVar(&flags.extraHosts, "extra-hosts", nil, "Docker extra hosts setting on the proxy")
	cmd.Flags().DurationVarP(&flags.timeout, "timeout", "t", 0, "max time to run an update")
	cmd.Flags().IntVar(&flags.inputServerPort, "input-port", 0, "port to use for securely passing input to the updater")
	cmd.Flags().StringVarP(&flags.apiUrl, "api-url", "a", "", "the api dependabot should connect to.")

	return cmd
}
