package infra

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/server"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/archive"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"github.com/moby/moby/api/types/registry"
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
	// directory to copy into the updater container as the repo
	LocalDir string
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
	// generate performance metrics?
	Flamegraph bool
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
	// CollectorImage is the image to use for the OpenTelemetry collector
	CollectorImage string
	// CollectorConfigPath is the path to the OpenTelemetry collector configuration file
	CollectorConfigPath string
	// Writer is where API calls will be written to
	Writer    io.Writer
	InputName string
	InputRaw  []byte
	ApiUrl    string
}

var gitShaRegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (p *RunParams) Validate() error {
	if p.Job == nil {
		return fmt.Errorf("job is required")
	}
	if p.Job.Source.Commit != "" && !gitShaRegex.MatchString(p.Job.Source.Commit) {
		return fmt.Errorf("commit must be a SHA, or not provided")
	}
	return nil
}

func Run(params RunParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

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
	if err := checkCredAccess(ctx, params.Job, params.Creds); err != nil {
		return err
	}

	if err := setImageNames(&params); err != nil {
		return err
	}

	if params.ApiUrl == "" {
		params.ApiUrl = fmt.Sprintf("http://host.docker.internal:%v", api.Port())
	}
	if err := runContainers(ctx, params); err != nil {
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
	if params.Job.Source.Commit == "" {
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
	defaultApiEndpoint = "https://api.github.com"
	ErrWriteAccess     = fmt.Errorf("for security, credentials used in update are not allowed to have write access to GitHub API")
)

// checkCredAccess returns an error if any of the tokens in the job definition have write access.
// Some package managers can execute arbitrary code during an update. The credentials are not accessible to the updater,
// but the proxy injects them in requests, and the updater could execute arbitrary requests. So to be safe, disallow
// write access on these tokens.
func checkCredAccess(ctx context.Context, job *model.Job, creds []model.Credential) error {
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
		apiEndpoint := defaultApiEndpoint
		if job != nil && job.Source.APIEndpoint != nil && *job.Source.APIEndpoint != "" {
			apiEndpoint = *job.Source.APIEndpoint
		}
		r, err := http.NewRequestWithContext(ctx, "GET", apiEndpoint, http.NoBody)
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
			return fmt.Errorf("failed request to GitHub API to check access: %s", resp.Status)
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
	"swift":          "swift",
	"devcontainers":  "devcontainers",
}

func setImageNames(params *RunParams) error {
	if params.ProxyImage == "" {
		params.ProxyImage = ProxyImageName
	}
	if params.CollectorImage == "" {
		params.CollectorImage = CollectorImageName
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
	if api != nil {
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

func runContainers(ctx context.Context, params RunParams) (err error) {
	var cli *client.Client
	cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	if params.PullImages {
		err = pullImage(ctx, cli, params.ProxyImage)
		if err != nil {
			return err
		}

		if params.CollectorConfigPath != "" {
			err = pullImage(ctx, cli, params.CollectorImage)
			if err != nil {
				return err
			}
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

	prox, err := NewProxy(ctx, cli, &params, networks)
	if err != nil {
		return err
	}
	defer func() {
		if proxyErr := prox.Close(); proxyErr != nil {
			err = proxyErr
		}
	}()

	// proxy logs interfere with debugging output
	if !params.Debug {
		go prox.TailLogs(ctx, cli)
	}

	var collector *Collector
	if params.CollectorConfigPath != "" {
		collector, err = NewCollector(ctx, cli, networks, &params, prox)
		if err != nil {
			return err
		}
		defer collector.Close()
	}

	updater, err := NewUpdater(ctx, cli, networks, &params, prox, collector)
	if err != nil {
		return err
	}
	defer func() {
		if updaterErr := updater.Close(); updaterErr != nil {
			err = updaterErr
		}
	}()

	// put the clone dir in the updater container to be used by during the update
	if params.LocalDir != "" {
		if err = putCloneDir(ctx, cli, updater, params.LocalDir); err != nil {
			return err
		}
	}

	if params.Debug {
		if err := updater.RunShell(ctx, prox.url, params.ApiUrl); err != nil {
			return err
		}
	} else {
		env := userEnv(prox.url, params.ApiUrl)
		if params.Flamegraph {
			env = append(env, "FLAMEGRAPH=1")
		}
		const cmd = "update-ca-certificates && bin/run fetch_files && bin/run update_files"
		if err := updater.RunCmd(ctx, cmd, dependabot, env...); err != nil {
			return err
		}
		if params.Flamegraph {
			getFromContainer(ctx, cli, updater.containerID, "/tmp/dependabot-flamegraph.html")
		}
		// If the exit code is non-zero, error when using the `update` subcommand, but not the `test` subcommand.
		if params.Expected == nil && *updater.ExitCode != 0 {
			return fmt.Errorf("updater exited with code %d", *updater.ExitCode)
		}
	}

	return nil
}

func getFromContainer(ctx context.Context, cli *client.Client, containerID, srcPath string) {
	reader, _, err := cli.CopyFromContainer(ctx, containerID, srcPath)
	if err != nil {
		log.Println("Failed to get from container:", err)
		return
	}
	defer reader.Close()
	outFile, err := os.Create("flamegraph.html")
	if err != nil {
		log.Println("Failed to create file while getting from container:", err)
		return
	}
	defer outFile.Close()
	tarReader := tar.NewReader(reader)
	tarReader.Next()
	_, err = io.Copy(outFile, tarReader)
	if err != nil {
		log.Printf("Failed copy while getting from container %v: %v\n", srcPath, err)
	}
}

func putCloneDir(ctx context.Context, cli *client.Client, updater *Updater, dir string) error {
	// Docker won't create the directory, so we have to do it first.
	const cmd = "mkdir -p " + guestRepoDir
	err := updater.RunCmd(ctx, cmd, dependabot)
	if err != nil {
		return fmt.Errorf("failed to create clone dir: %w", err)
	}

	r, err := archive.TarWithOptions(dir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("failed to tar clone dir: %w", err)
	}

	opt := types.CopyToContainerOptions{}
	err = cli.CopyToContainer(ctx, updater.containerID, guestRepoDir, r, opt)
	if err != nil {
		return fmt.Errorf("failed to copy clone dir to container: %w", err)
	}

	err = updater.RunCmd(ctx, "chown -R dependabot "+guestRepoDir, root)
	if err != nil {
		return fmt.Errorf("failed to initialize clone dir: %w", err)
	}

	// The directory needs to be a git repo, so we need to initialize it.
	commands := []string{
		"cd " + guestRepoDir,
		"git config --global init.defaultBranch main",
		"git init",
		"git config user.email 'dependabot@github.com'",
		"git config user.name 'dependabot'",
		"git add .",
		"git commit --quiet -m 'initial commit'",
	}
	err = updater.RunCmd(ctx, strings.Join(commands, " && "), dependabot)
	if err != nil {
		return fmt.Errorf("failed to initialize clone dir: %w", err)
	}

	return nil
}

func pullImage(ctx context.Context, cli *client.Client, image string) error {
	var inspect types.ImageInspect

	// check if image exists locally
	inspect, _, err := cli.ImageInspectWithRaw(ctx, image)

	// pull image if necessary
	if err != nil {
		var imagePullOptions types.ImagePullOptions

		if strings.HasPrefix(image, "ghcr.io/") {

			token := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN")
			if token != "" {
				auth := base64.StdEncoding.EncodeToString([]byte("x:" + token))
				imagePullOptions = types.ImagePullOptions{
					RegistryAuth: fmt.Sprintf("Basic %s", auth),
				}
			} else {
				log.Println("Failed to find credentials for GitHub container registry.")
			}
		} else if strings.Contains(image, ".azurecr.io/") {
			username := os.Getenv("AZURE_REGISTRY_USERNAME")
			password := os.Getenv("AZURE_REGISTRY_PASSWORD")

			registryName := strings.Split(image, "/")[0]

			if username != "" && password != "" {
				authConfig := registry.AuthConfig{
					Username:      username,
					Password:      password,
					ServerAddress: registryName,
				}

				encodedJSON, _ := json.Marshal(authConfig)
				authStr := base64.URLEncoding.EncodeToString(encodedJSON)

				imagePullOptions = types.ImagePullOptions{
					RegistryAuth: authStr,
				}
			} else {
				log.Println("Failed to find credentials for Azure container registry.")
			}
		} else {
			log.Printf("Failed to find credentials for pulling image: %s\n.", image)
		}

		log.Printf("pulling image: %s\n", image)
		out, err := cli.ImagePull(ctx, image, imagePullOptions)
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
