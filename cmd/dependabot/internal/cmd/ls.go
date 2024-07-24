package cmd

import (
	"context"
	"errors"
	"github.com/MakeNowJust/heredoc"
	"github.com/dependabot/cli/internal/infra"
	"github.com/spf13/cobra"
	"log"
)

var listCmd = NewListCommand()

func init() {
	rootCmd.AddCommand(listCmd)
}

func NewListCommand() *cobra.Command {
	var flags UpdateFlags

	cmd := &cobra.Command{
		Use:   "ls [<package_manager> <repo>] [flags]",
		Short: "List the dependencies of a manifest/lockfile",
		Example: heredoc.Doc(`
		    $ dependabot ls go_modules rsc/quote
		    $ dependabot ls go_modules --local .
	    `),
		RunE: func(cmd *cobra.Command, args []string) error {
			input, err := readArguments(cmd, &flags)
			if err != nil {
				return err
			}

			processInput(input, &flags)

			input.Job.Source.Provider = "github" // TODO why isn't this being set?

			if err := infra.Run(infra.RunParams{
				CacheDir:            flags.cache,
				CollectorConfigPath: flags.collectorConfigPath,
				CollectorImage:      collectorImage,
				Creds:               input.Credentials,
				Debug:               flags.debugging,
				Flamegraph:          flags.flamegraph,
				Expected:            nil, // update subcommand doesn't use expectations
				ExtraHosts:          flags.extraHosts,
				InputName:           flags.file,
				Job:                 &input.Job,
				ListDependencies:    true, // list dependencies, then exit
				LocalDir:            flags.local,
				Output:              flags.output,
				ProxyCertPath:       flags.proxyCertPath,
				ProxyImage:          proxyImage,
				PullImages:          flags.pullImages,
				Timeout:             flags.timeout,
				UpdaterImage:        updaterImage,
				Volumes:             flags.volumes,
				Writer:              nil, // prevent outputting all API responses to stdout, we only want dependencies
			}); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					log.Fatalf("update timed out after %s", flags.timeout)
				}
				// HACK: we cancel context to stop the containers, so we don't know if there was a failure.
				// A correct solution would involve changes with dependabot-core, which is good, but
				// I am just hacking this together right now.
				log.Printf("HACK: suppressing updater failure: %v", err)
				return nil
			}

			return nil
		},
	}

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

	return cmd
}
