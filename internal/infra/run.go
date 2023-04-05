package infra

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/server"
	"github.com/docker/docker/api/types"
	"github.com/moby/moby/client"
	"gopkg.in/yaml.v3"
)

type RunParams struct {
	// Input file
	Input string
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
	// UpdaterImage is the image to use for the updater
	UpdaterImage string
	// ProxyImage is the image to use for the proxy
	ProxyImage string
	// Writer is where API calls will be written to
	Writer    io.Writer
	InputName string
	InputRaw  []byte
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

	api := server.NewAPI(params.Expected, params.Writer)
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

	expandEnvironmentVariables(api, &params)
	if err := checkCredAccess(ctx, params.Creds); err != nil {
		return err
	}

	if err := setImageNames(&params); err != nil {
		return err
	}

	if err := runContainers(ctx, params, api); err != nil {
		return err
	}

	api.Complete()

	output, err := generateOutput(params, api, outFile)
	if err != nil {
		return err
	}

	if len(api.Errors) > 0 {
		return diff(params, outFile, output)
	}

	return nil
}

func generateOutput(params RunParams, api *server.API, outFile *os.File) ([]byte, error) {
	if params.Job.Source.Commit == nil {
		// store the SHA we worked with for reproducible tests
		params.Job.Source.Commit = api.Actual.Input.Job.Source.Commit
	}
	api.Actual.Input.Job = *params.Job

	// ignore conditions help make tests reproducible, so they are generated if there aren't any yet
	if len(api.Actual.Input.Job.IgnoreConditions) == 0 && api.Actual.Input.Job.PackageManager != "submodules" {
		if err := generateIgnoreConditions(&params, &api.Actual); err != nil {
			return nil, err
		}
	}

	output, err := yaml.Marshal(api.Actual)
	if err != nil {
		return nil, fmt.Errorf("failed to write output: %v", err)
	}

	if outFile != nil {
		if err := outFile.Truncate(0); err != nil {
			return nil, fmt.Errorf("failed to truncate output file: %w", err)
		}
		n, err := outFile.Write(output)
		if err != nil {
			return nil, fmt.Errorf("failed to write output: %w", err)
		}
		if n != len(output) {
			return nil, fmt.Errorf("failed to write complete output: %w", io.ErrShortWrite)
		}
	}
	return output, nil
}

func diff(params RunParams, outFile *os.File, output []byte) error {
	inName := "input.yml"
	outName := "output.yml"
	if params.InputName != "" {
		inName = params.InputName
	}
	if outFile != nil {
		outName = outFile.Name()
	}
	aString := string(params.InputRaw)
	edits := myers.ComputeEdits(span.URIFromPath(inName), aString, string(output))
	_, _ = fmt.Fprintln(os.Stderr, gotextdiff.ToUnified(inName, outName, aString, edits))

	return fmt.Errorf("update failed expectations")
}

var (
	authEndpoint   = "https://api.github.com"
	ErrWriteAccess = fmt.Errorf("for security, credentials used in update are not allowed to have write access to GitHub API")
)

// checkCredAccess returns an error if any of the tokens in the job definition have write access.
// Some package managers can execute arbitrary code during an update. The credentials are not accessible to the updater,
// but the proxy injects them in requests, and the updater could execute arbitrary requests. So to be safe, disallow
// write access on these tokens.
func checkCredAccess(ctx context.Context, creds []model.Credential) error {
	for _, cred := range creds {
		var credential string
		if password, ok := cred["password"]; ok && password != "" {
			credential, _ = password.(string)
		}
		if token, ok := cred["token"]; ok && token != "" {
			credential, _ = token.(string)
		}
		if !strings.HasPrefix(credential, "ghp_") {
			continue
		}
		r, err := http.NewRequestWithContext(ctx, "GET", authEndpoint, http.NoBody)
		if err != nil {
			return fmt.Errorf("failed creating request: %w", err)
		}
		r.Header.Set("Authorization", fmt.Sprintf("token %s", credential))
		r.Header.Set("User-Agent", "dependabot-cli")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			return fmt.Errorf("failed making request: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed request to GitHub API: %s", resp.Status)
		}
		scopes := resp.Header.Get("X-OAuth-Scopes")
		if strings.Contains(scopes, "write") || strings.Contains(scopes, "delete") {
			return ErrWriteAccess
		}
	}
	return nil
}

var packageManagerLookup = map[string]string{
	"bundler":        "bundler",
	"cargo":          "cargo",
	"composer":       "composer",
	"pub":            "pub",
	"docker":         "docker",
	"elm":            "elm",
	"github_actions": "github-actions",
	"submodules":     "gitsubmodule",
	"go_modules":     "gomod",
	"gradle":         "gradle",
	"maven":          "maven",
	"hex":            "mix",
	"nuget":          "nuget",
	"npm_and_yarn":   "npm",
	"pip":            "pip",
	"terraform":      "terraform",
}

func setImageNames(params *RunParams) error {
	if params.ProxyImage == "" {
		params.ProxyImage = ProxyImageName
	}
	if params.UpdaterImage == "" {
		pm, ok := packageManagerLookup[params.Job.PackageManager]
		if !ok {
			return fmt.Errorf("unknown package manager: %s", params.Job.PackageManager)
		}
		params.UpdaterImage = "ghcr.io/dependabot/dependabot-updater-" + pm
	}
	return nil
}

func expandEnvironmentVariables(api *server.API, params *RunParams) {
	api.Actual.Input.Credentials = params.Creds

	// Make a copy of the credentials, so we don't inject them into the output file.
	params.Creds = []model.Credential{}
	for _, cred := range api.Actual.Input.Credentials {
		newCred := model.Credential{}
		for k, v := range cred {
			newCred[k] = v
		}
		params.Creds = append(params.Creds, newCred)
	}

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

		err = pullImage(ctx, cli, params.UpdaterImage)
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
