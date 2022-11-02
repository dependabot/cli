package infra

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dependabot/cli/internal/server"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api/types"
	"github.com/moby/moby/client"
	"gopkg.in/yaml.v3"
)

type RunParams struct {
	// job definition passed to the updater
	Job *model.Job
	// expectations asserted at the end of a test
	Expected []model.Output
	// credentials passed to the proxy
	Creds []model.Credential
	// local directory used for caching
	CacheDir string
	// write output to a file
	Output string
	// ProxyCertPath is the path to a cert for the proxy to trust
	ProxyCertPath string
	// attempt to pull images if they aren't local?
	PullImages bool
	// run an interactive shell?
	Debug bool
	// Volumes are used to mount directories in Docker
	Volumes []string
	// Timeout specifies an optional maximum duration the CLI will run an update.
	// If Timeout is <= 0 it will never time out.
	Timeout time.Duration
	// ExtraHosts adds /etc/hosts entries to the proxy for testing.
	ExtraHosts []string
}

func Run(params RunParams) error {
	var ctx context.Context
	var cancel func()
	if params.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), params.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
	}()

	api := server.NewAPI(params.Expected)
	defer api.Stop()

	var outFile *os.File
	if params.Output != "" {
		var err error
		// Open a file for writing but don't truncate it yet since an error will delete the test.
		// This is done before the test so if the dir isn't writable it doesn't waste time.
		outFile, err = os.OpenFile(params.Output, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer outFile.Close()
	}

	expandEnvironmentVariables(api, params)

	if err := runContainers(ctx, params, api); err != nil {
		return err
	}

	api.Complete()
	if outFile != nil {
		if err := outFile.Truncate(0); err != nil {
			return fmt.Errorf("failed to truncate output file: %w", err)
		}
		if params.Job.Source.Commit == nil {
			// store the SHA we worked with for reproducible tests
			params.Job.Source.Commit = api.Actual.Input.Job.Source.Commit
		}
		api.Actual.Input.Job = *params.Job

		// ignore conditions help make tests reproducible
		// so they are generated if there aren't any yet
		if len(api.Actual.Input.Job.IgnoreConditions) == 0 && api.Actual.Input.Job.PackageManager != "submodules" {
			if err := generateIgnoreConditions(&params, &api.Actual); err != nil {
				return err
			}
		}
		if err := yaml.NewEncoder(outFile).Encode(api.Actual); err != nil {
			return fmt.Errorf("failed to write output: %v", err)
		}
	}
	if len(api.Errors) > 0 {
		log.Println("The following errors occurred:")

		for _, e := range api.Errors {
			log.Println(e)
		}

		return fmt.Errorf("update failed expectations")
	}

	return nil
}

func expandEnvironmentVariables(api *server.API, params RunParams) {
	api.Actual.Input.Credentials = params.Creds

	// Make a copy of the credentials, so we don't inject them into the output file.
	params.Creds = make([]model.Credential, len(params.Creds))
	copy(params.Creds, api.Actual.Input.Credentials)

	// Add the actual credentials from the environment.
	for _, cred := range params.Creds {
		for key, value := range cred {
			if valueString, ok := value.(string); ok {
				cred[key] = os.ExpandEnv(valueString)
			}
		}
	}
}

func generateIgnoreConditions(params *RunParams, actual *model.Scenario) error {
	for _, out := range actual.Output {
		if out.Type == "create_pull_request" {
			createPR, ok := out.Expect.Data.(model.CreatePullRequest)
			if !ok {
				return fmt.Errorf("failed to decode CreatePullRequest object")
			}

			for _, dep := range createPR.Dependencies {
				if dep.Version == nil {
					// dependency version nil due to it being removed
					continue
				}
				ignore := model.Condition{
					DependencyName:     dep.Name,
					VersionRequirement: fmt.Sprintf(">%v", *dep.Version),
					Source:             params.Output,
				}
				actual.Input.Job.IgnoreConditions = append(actual.Input.Job.IgnoreConditions, ignore)
			}
		}
	}
	return nil
}

func runContainers(ctx context.Context, params RunParams, api *server.API) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	if params.PullImages {
		err = pullImage(ctx, cli, ProxyImageName)
		if err != nil {
			return err
		}

		err = pullImage(ctx, cli, UpdaterImageName)
		if err != nil {
			return err
		}
	}

	networks, err := NewNetworks(ctx, cli)
	if err != nil {
		return fmt.Errorf("failed to create networks: %w", err)
	}
	defer networks.Close()

	prox, err := NewProxy(ctx, cli, &params, networks.NoInternet, networks.Internet)
	if err != nil {
		return err
	}
	defer prox.Close()

	// proxy logs interfere with debugging output
	if !params.Debug {
		go prox.TailLogs(ctx, cli)
	}

	updater, err := NewUpdater(ctx, cli, networks, &params, prox)
	if err != nil {
		return err
	}
	defer updater.Close()

	if err := updater.InstallCertificates(ctx); err != nil {
		return err
	}

	if params.Debug {
		if err := updater.RunShell(ctx, prox.url, api.Port()); err != nil {
			return err
		}
	} else {
		if err := updater.RunUpdate(ctx, prox.url, api.Port()); err != nil {
			return err
		}
	}

	return nil
}

func pullImage(ctx context.Context, cli *client.Client, image string) error {
	var inspect types.ImageInspect

	// check if image exists locally
	inspect, _, err := cli.ImageInspectWithRaw(ctx, image)

	// pull image if necessary
	if err != nil {
		var privilegeFunc types.RequestPrivilegeFunc
		token := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN")
		if token != "" {
			auth := base64.StdEncoding.EncodeToString([]byte("x:" + token))
			privilegeFunc = func() (string, error) {
				return "Basic " + auth, nil
			}
		}

		log.Printf("pulling image: %s\n", image)
		out, err := cli.ImagePull(ctx, image, types.ImagePullOptions{
			PrivilegeFunc: privilegeFunc,
		})
		if err != nil {
			return fmt.Errorf("failed to pull %v: %w", image, err)
		}
		_, _ = io.Copy(io.Discard, out)
		out.Close()

		inspect, _, err = cli.ImageInspectWithRaw(ctx, image)
		if err != nil {
			return fmt.Errorf("failed to inspect %v: %w", image, err)
		}
	}

	log.Printf("using image %v at %s\n", image, inspect.ID)

	return nil
}
