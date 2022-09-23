package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/server"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	packageManager string
	provider       string
	repo           string
	directory      string

	dryRun          bool
	inputServerPort int
)

var updateCmd = &cobra.Command{
	Use:   "update <package_manager> <repo> [flags]",
	Short: "Perform update job",
	Example: heredoc.Doc(`
		    $ dependabot update go_modules rsc/quote --dry-run
	    `),
	RunE: func(cmd *cobra.Command, args []string) error {
		var outFile *os.File
		if output != "" {
			var err error
			outFile, err = os.Create(output)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outFile.Close()
		}

		input := &model.Input{}

		if file != "" {
			var err error
			input, err = readInputFile(file)
			if err != nil {
				return err
			}
		}

		if len(cmd.Flags().Args()) > 0 {
			packageManager = cmd.Flags().Args()[0]
			if packageManager == "" {
				return errors.New("requires a package manager argument")
			}

			repo = cmd.Flags().Args()[1]
			if repo == "" {
				return errors.New("requires a repo argument")
			}

			input.Job = model.Job{
				PackageManager: packageManager,
				AllowedUpdates: []model.Allowed{{
					UpdateType: "all",
				}},
				Dependencies:               nil,
				ExistingPullRequests:       [][]model.ExistingPR{},
				IgnoreConditions:           []model.Condition{},
				LockfileOnly:               false,
				RequirementsUpdateStrategy: nil,
				SecurityAdvisories:         []model.Advisory{},
				SecurityUpdatesOnly:        false,
				Source: model.Source{
					Provider:    provider,
					Repo:        repo,
					Directory:   directory,
					Branch:      nil,
					Hostname:    nil,
					APIEndpoint: nil,
				},
				UpdateSubdependencies: false,
				UpdatingAPullRequest:  false,
			}
		}

		if inputServerPort != 0 {
			input = server.Input(inputServerPort)
		}

		if doesStdinHaveData() {
			in := &bytes.Buffer{}
			_, err := io.Copy(in, os.Stdin)
			if err != nil {
				return err
			}
			data := in.Bytes()
			if err = json.Unmarshal(data, &input); err != nil {
				if err = yaml.Unmarshal(data, &input); err != nil {
					return fmt.Errorf("failed to decode input file: %w", err)
				}
			}
		}

		processInput(input)

		if err := infra.Run(infra.RunParams{
			CacheDir:   cache,
			Creds:      input.Credentials,
			Debug:      debugging,
			Expected:   nil, // update subcommand doesn't use expectations
			Job:        &input.Job,
			Output:     output,
			PullImages: pullImages,
			TempDir:    tempDir,
			Timeout:    timeout,
			Volumes:    volumes,
		}); err != nil {
			log.Fatalf("failed to run updater: %v", err)
		}

		return nil
	},
}

func readInputFile(file string) (*model.Input, error) {
	var input model.Input

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to open input file: %w", err)
	}
	if err = json.Unmarshal(data, &input); err != nil {
		if err = yaml.Unmarshal(data, &input); err != nil {
			return nil, fmt.Errorf("failed to decode input file: %w", err)
		}
	}

	return &input, nil
}

func processInput(input *model.Input) {
	job := &input.Job
	// a few of the fields need to be initialized instead of null,
	// it would be nice if the updater didn't care
	if job.ExistingPullRequests == nil {
		job.ExistingPullRequests = [][]model.ExistingPR{}
	}
	if job.IgnoreConditions == nil {
		job.IgnoreConditions = []model.Condition{}
	}
	if job.SecurityAdvisories == nil {
		job.SecurityAdvisories = []model.Advisory{}
	}

	// Process environment variables in the scenario file
	for _, cred := range input.Credentials {
		for key, value := range cred {
			cred[key] = os.ExpandEnv(value)
		}
	}
}

func doesStdinHaveData() bool {
	file := os.Stdin
	fi, err := file.Stat()
	if err != nil {
		fmt.Println("file.Stat()", err)
	}
	return fi.Size() > 0
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringVarP(&file, "file", "f", "", "path to scenario file")

	updateCmd.Flags().StringVarP(&provider, "provider", "p", "github", "provider of the repository")
	updateCmd.Flags().StringVarP(&directory, "directory", "d", "/", "directory to update")

	updateCmd.Flags().BoolVar(&dryRun, "dry-run", true, "perform update as a dry run")
	_ = updateCmd.MarkFlagRequired("dry-run")

	updateCmd.Flags().StringVarP(&output, "output", "o", "", "write scenario to file")
	updateCmd.Flags().StringVar(&cache, "cache", "", "cache import/export directory")
	updateCmd.Flags().BoolVar(&pullImages, "pull", true, "pull the image if it isn't present")
	updateCmd.Flags().BoolVar(&debugging, "debug", false, "run an interactive shell inside the updater")
	updateCmd.Flags().StringArrayVarP(&volumes, "volume", "v", nil, "mount volumes in Docker")
	updateCmd.Flags().DurationVarP(&timeout, "timeout", "t", 0, "max time to run an update")
	updateCmd.Flags().IntVar(&inputServerPort, "input-port", 0, "port to use for securely passing input to the updater")
}
