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

	inputServerPort int
)

var updateCmd = &cobra.Command{
	Use:   "update [<package_manager> <repo> | -f <input.yml>] [flags]",
	Short: "Perform an update job",
	Example: heredoc.Doc(`
		    $ dependabot update go_modules rsc/quote
		    $ dependabot update -f input.yml
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

		input, err := extractInput(cmd)
		if err != nil {
			return err
		}

		processInput(input)

		var writer io.Writer
		if !debugging {
			writer = os.Stdout
		}

		if err := infra.Run(infra.RunParams{
			CacheDir:      cache,
			Creds:         input.Credentials,
			Debug:         debugging,
			Expected:      nil, // update subcommand doesn't use expectations
			ExtraHosts:    extraHosts,
			InputName:     file,
			Job:           &input.Job,
			Output:        output,
			ProxyCertPath: proxyCertPath,
			ProxyImage:    proxyImage,
			PullImages:    pullImages,
			Timeout:       timeout,
			UpdaterImage:  updaterImage,
			Writer:        writer,
			Volumes:       volumes,
		}); err != nil {
			log.Fatalf("failed to run updater: %v", err)
		}

		return nil
	},
}

func extractInput(cmd *cobra.Command) (*model.Input, error) {
	hasFile := file != ""
	hasArguments := len(cmd.Flags().Args()) > 0
	hasServer := inputServerPort != 0
	hasStdin := doesStdinHaveData()

	var count int
	for _, b := range []bool{hasFile, hasArguments, hasServer, hasStdin} {
		if b {
			count++
		}
	}
	if count > 1 {
		return nil, errors.New("can only use one of: input file, arguments, server, or stdin")
	}

	if hasFile {
		return readInputFile(file)
	}

	if hasArguments {
		input, err := readArguments(cmd)
		return input, err
	}

	if hasServer {
		return server.Input(inputServerPort), nil
	}

	if hasStdin {
		input, err := readStdin()
		return input, err
	}

	return nil, fmt.Errorf("requires input as arguments, input file, or stdin")
}

func readStdin() (*model.Input, error) {
	in := &bytes.Buffer{}
	_, err := io.Copy(in, os.Stdin)
	if err != nil {
		return nil, err
	}
	data := in.Bytes()
	input := &model.Input{}
	if err = json.Unmarshal(data, &input); err != nil {
		if err = yaml.Unmarshal(data, &input); err != nil {
			return nil, fmt.Errorf("failed to decode input file: %w", err)
		}
	}
	return nil, nil
}

func readArguments(cmd *cobra.Command) (*model.Input, error) {
	if len(cmd.Flags().Args()) != 2 {
		return nil, errors.New("requires a package manager and repo argument")
	}

	packageManager = cmd.Flags().Args()[0]
	if packageManager == "" {
		return nil, errors.New("requires a package manager argument")
	}

	repo = cmd.Flags().Args()[1]
	if repo == "" {
		return nil, errors.New("requires a repo argument")
	}

	input := &model.Input{
		Job: model.Job{
			PackageManager: packageManager,
			AllowedUpdates: []model.Allowed{{
				UpdateType: "all",
			}},
			DependencyGroups:           nil,
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
		},
	}
	return input, nil
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

	// As a convenience, fill in a git_source if credentials are in the environment and a git_source
	// doesn't already exist. This way the user doesn't run out of calls from being anonymous.
	hasLocalToken := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN") != ""
	var isGitSourceInCreds bool
	for _, cred := range input.Credentials {
		if cred["type"] == "git_source" {
			isGitSourceInCreds = true
			break
		}
	}
	if hasLocalToken && !isGitSourceInCreds {
		log.Println("Inserting $LOCAL_GITHUB_ACCESS_TOKEN into credentials")
		input.Credentials = append(input.Credentials, model.Credential{
			"type":     "git_source",
			"host":     "github.com",
			"username": "x-access-token",
			"password": "$LOCAL_GITHUB_ACCESS_TOKEN",
		})
		if len(input.Job.CredentialsMetadata) > 0 {
			// Add the metadata since the next section will be skipped.
			input.Job.CredentialsMetadata = append(input.Job.CredentialsMetadata, map[string]any{
				"type": "git_source",
				"host": "github.com",
			})
		}
	}

	// As a convenience, fill credentials-metadata if credentials are provided
	// which is what happens in production. This way the user doesn't have to
	// specify credentials-metadata in the scenario file unless they want to.
	if len(input.Job.CredentialsMetadata) == 0 {
		log.Println("Adding missing credentials-metadata into job definition")
		for _, credential := range input.Credentials {
			entry := map[string]any{
				"type": credential["type"],
			}
			if credential["host"] != nil {
				entry["host"] = credential["host"]
			}
			if credential["url"] != nil {
				entry["url"] = credential["url"]
			}
			if credential["replaces-base"] != nil {
				entry["replaces-base"] = credential["replaces-base"]
			}
			input.Job.CredentialsMetadata = append(input.Job.CredentialsMetadata, entry)
		}
	}
}

func doesStdinHaveData() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		log.Println("file.Stat()", err)
	}
	return fi.Size() > 0
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringVarP(&file, "file", "f", "", "path to scenario file")

	updateCmd.Flags().StringVarP(&provider, "provider", "p", "github", "provider of the repository")
	updateCmd.Flags().StringVarP(&directory, "directory", "d", "/", "directory to update")

	updateCmd.Flags().StringVarP(&output, "output", "o", "", "write scenario to file")
	updateCmd.Flags().StringVar(&cache, "cache", "", "cache import/export directory")
	updateCmd.Flags().StringVar(&proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
	updateCmd.Flags().BoolVar(&pullImages, "pull", true, "pull the image if it isn't present")
	updateCmd.Flags().BoolVar(&debugging, "debug", false, "run an interactive shell inside the updater")
	updateCmd.Flags().StringArrayVarP(&volumes, "volume", "v", nil, "mount volumes in Docker")
	updateCmd.Flags().StringArrayVar(&extraHosts, "extra-hosts", nil, "Docker extra hosts setting on the proxy")
	updateCmd.Flags().DurationVarP(&timeout, "timeout", "t", 0, "max time to run an update")
	updateCmd.Flags().IntVar(&inputServerPort, "input-port", 0, "port to use for securely passing input to the updater")
}
