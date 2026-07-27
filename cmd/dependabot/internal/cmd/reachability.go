package cmd

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
	"github.com/spf13/cobra"
)

// executeReachability is a seam so tests can capture the params without Docker,
// mirroring the `test` command's executeTestJob pattern.
var executeReachability = infra.RunReachability

type ReachabilityFlags struct {
	file              string
	inputDir          string
	annotations       string
	codeqlPath        string
	reachabilityImage string
	proxyCertPath     string
	cache             string
	extraHosts        []string
	pullImages        bool
	timeout           time.Duration
}

var reachabilityCmd = NewReachabilityCommand()

func init() {
	rootCmd.AddCommand(reachabilityCmd)
}

func NewReachabilityCommand() *cobra.Command {
	var flags ReachabilityFlags

	cmd := &cobra.Command{
		Use:   "reachability --input-dir <dir> --reachability-image <image> [-f <input.yml>] [flags]",
		Short: "[Experimental] Decide which vulnerable dependencies are reachable from first-party code",
		Long: heredoc.Doc(`
		    NOTE: This command is a work in progress.

		    It runs the dependabot-reachability-cli as a Dependabot job: it starts
		    the proxy and a reachability container (networked so dependency fetches
		    go through the proxy), then runs the CodeQL reachability analysis over a
		    pre-provided set of inputs.

		    The inputs are provided in --input-dir (not fetched here): the target/
		    checkout, alerts.json, an optional sbom.json (SBOM-first inventory), and
		    the annotations feed. Outputs (refined.csv, paths.json) are written back
		    into --input-dir.

		    $ dependabot reachability --input-dir ./reach-run \
		        --reachability-image ghcr.io/dependabot/dependabot-reachability \
		        -f input.yml
	    `),
		RunE: func(cmd *cobra.Command, args []string) error {
			var input model.Input
			if flags.file != "" {
				in, err := readInputFile(flags.file)
				if err != nil {
					return err
				}
				input = *in
			}
			// A package manager is needed so the proxy configures credential
			// injection; default to npm while npm is the only supported ecosystem.
			if input.Job.PackageManager == "" {
				input.Job.PackageManager = "npm_and_yarn"
			}

			if err := executeReachability(infra.ReachabilityParams{
				Job:               &input.Job,
				Creds:             input.Credentials,
				ReachabilityImage: flags.reachabilityImage,
				ProxyImage:        proxyImage,
				InputDir:          flags.inputDir,
				Annotations:       flags.annotations,
				CodeqlPath:        flags.codeqlPath,
				PullImages:        flags.pullImages,
				Timeout:           flags.timeout,
				ExtraHosts:        flags.extraHosts,
				ProxyCertPath:     flags.proxyCertPath,
				CacheDir:          flags.cache,
			}); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					log.Fatalf("reachability timed out after %s", flags.timeout)
				}
				log.Fatalf("reachability failure: %v", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.file, "file", "f", "", "path to input file (job source + credentials for the proxy)")
	cmd.Flags().StringVar(&flags.inputDir, "input-dir", "", "host dir holding the reachability inputs (target/, alerts.json, sbom.json, annotations/)")
	cmd.Flags().StringVar(&flags.annotations, "annotations", "annotations", "annotation feed path relative to --input-dir")
	cmd.Flags().StringVar(&flags.codeqlPath, "codeql", "", "path to the codeql binary inside the reachability image")
	cmd.Flags().StringVar(&flags.reachabilityImage, "reachability-image", "", "container image bundling reach + codeql")
	cmd.Flags().StringVar(&flags.proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "cache import/export directory")
	cmd.Flags().StringArrayVar(&flags.extraHosts, "extra-hosts", nil, "Docker extra hosts setting on the proxy")
	cmd.Flags().BoolVar(&flags.pullImages, "pull", true, "pull the images if not present")
	cmd.Flags().DurationVarP(&flags.timeout, "timeout", "t", 0, "max time to run the reachability check")

	_ = cmd.MarkFlagRequired("input-dir")
	_ = cmd.MarkFlagRequired("reachability-image")

	return cmd
}
