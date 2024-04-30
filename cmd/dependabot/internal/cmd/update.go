package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/server"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var updateCmd = NewUpdateCommand()

func init() {
	rootCmd.AddCommand(updateCmd)
}

type UpdateFlags struct {
	SharedFlags
	provider        string
	directory       string
	branch          string
	local           string
	commit          string
	dependencies    []string
	inputServerPort int
	apiUrl          string
}

// A map of package manager names to credential type
var azureArtifactsPackageManagerCredentialType = map[string]string{
	"gradle":       "maven_repository",
	"maven":        "maven_repository",
	"npm_and_yarn": "npm_registry",
	"nuget":        "nuget_feed",
	"pip":          "python_index",
}

func NewUpdateCommand() *cobra.Command {
	var flags UpdateFlags

	cmd := &cobra.Command{
		Use:   "update [<package_manager> <repo> | -f <input.yml>] [flags]",
		Short: "Perform an update job",
		Example: heredoc.Doc(`
		    $ dependabot update go_modules rsc/quote
		    $ dependabot update -f input.yml
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

			var writer io.Writer
			if !flags.debugging {
				writer = os.Stdout
			}

			if err := infra.Run(infra.RunParams{
				CacheDir:              flags.cache,
				CollectorConfigPath:   flags.collectorConfigPath,
				CollectorImage:        collectorImage,
				Creds:                 input.Credentials,
				Debug:                 flags.debugging,
				Flamegraph:            flags.flamegraph,
				Expected:              nil, // update subcommand doesn't use expectations
				ExtraHosts:            flags.extraHosts,
				InputName:             flags.file,
				Job:                   &input.Job,
				LocalDir:              flags.local,
				Output:                flags.output,
				ProxyCertPath:         flags.proxyCertPath,
				ProxyImage:            proxyImage,
				PullImages:            flags.pullImages,
				VerifyImageSignatures: flags.verifyImageSignatures,
				Timeout:               flags.timeout,
				UpdaterImage:          updaterImage,
				Volumes:               flags.volumes,
				Writer:                writer,
				ApiUrl:                flags.apiUrl,
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
	cmd.Flags().StringArrayVarP(&flags.dependencies, "dep", "", nil, "dependencies to update")

	cmd.Flags().StringVarP(&flags.output, "output", "o", "", "write scenario to file")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "cache import/export directory")
	cmd.Flags().StringVar(&flags.local, "local", "", "local directory to use as fetched source")
	cmd.Flags().StringVar(&flags.proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
	cmd.Flags().StringVar(&flags.collectorConfigPath, "collector-config", "", "path to an OpenTelemetry collector config file")
	cmd.Flags().BoolVar(&flags.pullImages, "pull", true, "pull the image if it isn't present")
	cmd.Flags().BoolVar(&flags.verifyImageSignatures, "verify-signatures", true, "verify image signatures using cosign")
	cmd.Flags().BoolVar(&flags.debugging, "debug", false, "run an interactive shell inside the updater")
	cmd.Flags().BoolVar(&flags.flamegraph, "flamegraph", false, "generate a flamegraph and other metrics")
	cmd.Flags().StringArrayVarP(&flags.volumes, "volume", "v", nil, "mount volumes in Docker")
	cmd.Flags().StringArrayVar(&flags.extraHosts, "extra-hosts", nil, "Docker extra hosts setting on the proxy")
	cmd.Flags().DurationVarP(&flags.timeout, "timeout", "t", 0, "max time to run an update")
	cmd.Flags().IntVar(&flags.inputServerPort, "input-port", 0, "port to use for securely passing input to the updater")
	cmd.Flags().StringVarP(&flags.apiUrl, "api-url", "a", "", "the api dependabot should connect to.")

	return cmd
}

func extractInput(cmd *cobra.Command, flags *UpdateFlags) (*model.Input, error) {
	hasFile := flags.file != ""
	hasArguments := len(cmd.Flags().Args()) > 0
	hasServer := flags.inputServerPort != 0
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
		return readInputFile(flags.file)
	}

	if hasArguments {
		return readArguments(cmd, flags)
	}

	if hasServer {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", flags.inputServerPort))
		if err != nil {
			return nil, fmt.Errorf("failed to create listener: %w", err)
		}
		return server.Input(l)
	}

	if hasStdin {
		return readStdin()
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
	return input, nil
}

func readArguments(cmd *cobra.Command, flags *UpdateFlags) (*model.Input, error) {
	if len(cmd.Flags().Args()) != 2 {
		return nil, errors.New("requires a package manager and repo argument")
	}

	packageManager := cmd.Flags().Args()[0]
	if packageManager == "" {
		return nil, errors.New("requires a package manager argument")
	}

	repo := cmd.Flags().Args()[1]
	if repo == "" {
		return nil, errors.New("requires a repo argument")
	}

	allowed := []model.Allowed{{UpdateType: "all"}}
	if len(flags.dependencies) > 0 {
		allowed = allowed[:0]
		for _, dep := range flags.dependencies {
			allowed = append(allowed, model.Allowed{DependencyName: dep})
		}
	}

	if flags.branch != "" && flags.commit != "" {
		return nil, errors.New("cannot specify both branch and commit")
	}

	input := &model.Input{
		Job: model.Job{
			PackageManager:             packageManager,
			AllowedUpdates:             allowed,
			DependencyGroups:           nil,
			Dependencies:               nil,
			ExistingPullRequests:       [][]model.ExistingPR{},
			IgnoreConditions:           []model.Condition{},
			LockfileOnly:               false,
			RequirementsUpdateStrategy: nil,
			SecurityAdvisories:         []model.Advisory{},
			SecurityUpdatesOnly:        false,
			Source: model.Source{
				Provider:    flags.provider,
				Repo:        repo,
				Directory:   flags.directory,
				Commit:      flags.commit,
				Branch:      flags.branch,
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

func processInput(input *model.Input, flags *UpdateFlags) {
	job := &input.Job
	// a few of the fields need to be initialized instead of null,
	// it would be nice if the updater didn't care
	if job.AllowedUpdates == nil {
		job.AllowedUpdates = []model.Allowed{{
			UpdateType: "all",
		}}
	}
	if job.ExistingPullRequests == nil {
		job.ExistingPullRequests = [][]model.ExistingPR{}
	}
	if job.IgnoreConditions == nil {
		job.IgnoreConditions = []model.Condition{}
	}
	if job.SecurityAdvisories == nil {
		job.SecurityAdvisories = []model.Advisory{}
	}
	if job.ExistingGroupPullRequests == nil {
		job.ExistingGroupPullRequests = []model.ExistingGroupPR{}
	}
	if job.DependencyGroups == nil {
		job.DependencyGroups = []model.Group{}
	}

	azureRepo := model.NewAzureRepo(input.Job.PackageManager, input.Job.Source.Repo, input.Job.Source.Directory)

	// As a convenience, fill in a git_source if credentials are in the environment and a git_source
	// doesn't already exist. This way the user doesn't run out of calls from being anonymous.
	hasLocalToken := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN") != ""
	hasLocalAzureToken := os.Getenv("LOCAL_AZURE_ACCESS_TOKEN") != ""

	var isGitSourceInCreds bool
	for _, cred := range input.Credentials {
		if cred["type"] == "git_source" {
			isGitSourceInCreds = true
			break
		}
	}
	if hasLocalAzureToken && flags != nil && flags.apiUrl != "" && azureRepo != nil {
		u, _ := url.Parse(flags.apiUrl)
		input.Credentials = append(input.Credentials, model.Credential{
			"type":     "git_source",
			"host":     u.Hostname(),
			"username": azureRepo.Org,
			"password": "$LOCAL_AZURE_ACCESS_TOKEN",
		})
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

	if hasLocalAzureToken && !isGitSourceInCreds && azureRepo != nil {
		log.Println("Inserting $LOCAL_AZURE_ACCESS_TOKEN into credentials")
		log.Printf("Inserting artifacts credentials for %s organization.", azureRepo.Org)

		// add both `dev.azure.com` and `org.visualstudio.com` credentials
		input.Credentials = append(input.Credentials, model.Credential{
			"type":     "git_source",
			"host":     "dev.azure.com",
			"username": "x-access-token",
			"password": "$LOCAL_AZURE_ACCESS_TOKEN",
		})
		input.Credentials = append(input.Credentials, model.Credential{
			"type":     "git_source",
			"host":     fmt.Sprintf("%s.visualstudio.com", azureRepo.Org),
			"username": "x-access-token",
			"password": "$LOCAL_AZURE_ACCESS_TOKEN",
		})
	}

	// Calculate the credentials-metadata as it cannot be provided by the user anymore.
	input.Job.CredentialsMetadata = []model.Credential{}
	for _, credential := range input.Credentials {
		entry := make(map[string]any)
		for k, v := range credential {
			// Updater does not get credentials.
			if k != "username" && k != "token" && k != "password" && k != "key" && k != "auth-key" {
				entry[k] = v
			}
		}
		input.Job.CredentialsMetadata = append(input.Job.CredentialsMetadata, entry)
	}

	if hasLocalAzureToken && azureRepo != nil {
		// Add the Azure Artifacts credentials for each host if the package manager is supported.
		if _, ok := azureArtifactsPackageManagerCredentialType[input.Job.PackageManager]; ok {
			// All Azure Artifacts hosts
			azureArtifactsHosts := []string{
				"pkgs.dev.azure.com",
				fmt.Sprintf("%s.pkgs.visualstudio.com", azureRepo.Org),
			}
			for _, host := range azureArtifactsHosts {
				input.Credentials = append(input.Credentials, model.Credential{
					"type":     azureArtifactsPackageManagerCredentialType[input.Job.PackageManager],
					"host":     host,
					"username": "x-access-token",
					"password": "$LOCAL_AZURE_ACCESS_TOKEN",
				})
			}
		} else {
			log.Printf("Skipping Azure Artifacts credentials for %s package manager.", input.Job.PackageManager)
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
