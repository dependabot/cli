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

	"github.com/docker/docker/api/types/container"

	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/server"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"github.com/moby/moby/api/types/registry"
	"gopkg.in/yaml.v3"
)

type RunCommand int

const (
	UpdateFilesCommand RunCommand = iota
	UpdateGraphCommand
)

var runCmds = map[RunCommand]string{
	UpdateFilesCommand: "bin/run fetch_files && bin/run update_files",
	UpdateGraphCommand: "bin/run fetch_files && bin/run update_graph",
}

type RunParams struct {
	// Input file
	Input string
	// Which command to use, this will default to UpdateCommand
	Command RunCommand
	// job definition passed to the updater
	Job *model.Job
	// expectations asserted at the end of a test
	Expected []model.Output
	// if true, the containers will be stopped once the dependencies are listed
	ListDependencies bool
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
	// StorageImage is the image to use for the storage service
	StorageImage string
	// Writer is where API calls will be written to
	Writer    io.Writer
	InputName string
	InputRaw  []byte
	ApiUrl    string
	// UpdaterEnvironmentVariables are additional environment variables to set in the update container
	UpdaterEnvironmentVariables []string
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

	if params.ListDependencies {
		go func() {
			dependencyList := <-api.UpdateDependencyList
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			err := encoder.Encode(dependencyList.Dependencies)
			if err != nil {
				log.Printf("failed to write dependency list: %v\n", err)
			}
			cancel()
		}()
	}

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

	// run the containers, but don't return the error until AFTER the output is generated.
	// this ensures that the output is always written in the smoke test where there are multiple outputs,
	// some that succeed and some that fail; we still want to see the output of the successful ones.
	runContainersErr := runContainers(ctx, params)

	api.Complete()

	// write the output to a file
	output, err := generateOutput(params, api, outFile)
	if err != nil {
		return err
	}

	if len(api.Errors) > 0 {
		return diff(params, outFile, output)
	}

	return runContainersErr
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
	"bun":            "bun",
	"bundler":        "bundler",
	"cargo":          "cargo",
	"composer":       "composer",
	"pub":            "pub",
	"docker":         "docker",
	"docker_compose": "docker-compose",
	"dotnet_sdk":     "dotnet-sdk",
	"elm":            "elm",
	"github_actions": "github-actions",
	"submodules":     "gitsubmodule",
	"go_modules":     "gomod",
	"gradle":         "gradle",
	"maven":          "maven",
	"helm":           "helm",
	"hex":            "mix",
	"nuget":          "nuget",
	"npm_and_yarn":   "npm",
	"pip":            "pip",
	"terraform":      "terraform",
	"swift":          "swift",
	"devcontainers":  "devcontainers",
	"uv":             "uv",
	"vcpkg":          "vcpkg",
	"rust_toolchain": "rust-toolchain",
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

func generateIgnoreConditions(params *RunParams, actual *model.SmokeTest) error {
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
				fmt.Println("Failed to pull OpenTelemetry collector image:", err)
			}
		}

		err = pullImage(ctx, cli, params.UpdaterImage)
		if err != nil {
			return err
		}

		if params.Job.UseCaseInsensitiveFileSystem() {
			err = pullImage(ctx, cli, params.StorageImage)
			if err != nil {
				return err
			}
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
			fmt.Println("Failed to create OpenTelemetry collector:", err)
		}
		if !params.Debug {
			go collector.TailLogs(ctx, cli)
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
		containerDir := guestRepoDir
		if params.Job.UseCaseInsensitiveFileSystem() {
			// since the updater is using the storage container, we need to populate the repo on that device because that's the directory that will be used for the update
			containerDir = caseSensitiveRepoContentsPath
		}
		if err = putCloneDir(ctx, cli, updater, params.LocalDir, containerDir); err != nil {
			return err
		}
	}

	if params.Debug {
		if err := updater.RunShell(ctx, prox.url, params.ApiUrl, params.Job, params.UpdaterEnvironmentVariables); err != nil {
			return err
		}
	} else {
		// First, update CA certificates as root
		if err := updater.RunCmd(ctx, "update-ca-certificates", root); err != nil {
			return err
		}

		// Then run the dependabot commands as the dependabot user
		env := userEnv(prox.url, params.ApiUrl, params.Job, params.UpdaterEnvironmentVariables)
		if params.Flamegraph {
			env = append(env, "FLAMEGRAPH=1")
		}
		if err := updater.RunCmd(ctx, runCmds[params.Command], dependabot, env...); err != nil {
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

func putCloneDir(ctx context.Context, cli *client.Client, updater *Updater, localDir, containerDir string) error {
	// Docker won't create the directory, so we have to do it first.
	cmd := fmt.Sprintf("mkdir -p %s", containerDir)
	err := updater.RunCmd(ctx, cmd, dependabot)
	if err != nil {
		return fmt.Errorf("failed to create clone dir: %w", err)
	}

	r, err := archive.TarWithOptions(localDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("failed to tar clone dir: %w", err)
	}

	opt := container.CopyToContainerOptions{}
	err = cli.CopyToContainer(ctx, updater.containerID, containerDir, r, opt)
	if err != nil {
		return fmt.Errorf("failed to copy clone dir to container: %w", err)
	}

	err = updater.RunCmd(ctx, "chown -R dependabot "+containerDir, root)
	if err != nil {
		return fmt.Errorf("failed to initialize clone dir: %w", err)
	}

	// The directory needs to be a git repo, so we need to initialize it.
	commands := []string{
		"cd " + containerDir,
		"git config --global init.defaultBranch main",
		"git init",
		"git config user.email 'dependabot@github.com'",
		"git config user.name 'dependabot'",
		"git add .",
		"git commit --quiet -m 'Dependabot CLI automated commit'",
	}
	err = updater.RunCmd(ctx, strings.Join(commands, " && "), dependabot)
	if err != nil {
		return fmt.Errorf("failed to initialize clone dir: %w", err)
	}

	return nil
}

func pullImage(ctx context.Context, cli *client.Client, imageName string) error {
	inspect, _, err := cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		// Image doesn't exist locally, pull it
		err = pullImageWithAuth(ctx, cli, imageName)
		if err != nil {
			return fmt.Errorf("failed to pull image %v: %w", imageName, err)
		}

		inspect, _, err = cli.ImageInspectWithRaw(ctx, imageName)
		if err != nil {
			return fmt.Errorf("failed to inspect image %v after pull: %w", imageName, err)
		}
	} else {
		// Image doesn't exist remotely, don't bother pulling it
		if inspect.RepoDigests == nil || len(inspect.RepoDigests) == 0 || inspect.RepoDigests[0] == "" {
			return nil
		}

		client := NewRegistryClient(imageName)
		exists, err := client.DigestExists(inspect.RepoDigests)
		if err != nil {
			log.Printf("failed to get digest for image %v: %v", imageName, err)
			return nil
		}

		// If the digest doesn't exist remotely, don't bother pulling the image
		if !exists {
			log.Printf("digest %v for image %v does not exist remotely\n", inspect.ID, imageName)
			return nil
		}

		latestDigest, err := client.GetLatestDigest(imageName)
		if err != nil {
			log.Printf("failed to get latest digest for image %v: %v", imageName, err)
			return nil
		}

		isLatest := false
		for _, digest := range inspect.RepoDigests {
			if strings.HasSuffix(digest, latestDigest) {
				isLatest = true
				break
			}
		}

		if !isLatest {
			err = pullImageWithAuth(ctx, cli, imageName)
			if err != nil {
				return fmt.Errorf("image %v is outdated, failed to pull update: %w", imageName, err)
			}
		} else {
			log.Printf("image %v is already up to date\n", imageName)
		}
	}

	log.Printf("using image %v at %s\n", imageName, inspect.ID)
	return nil
}

func pullImageWithAuth(ctx context.Context, cli *client.Client, imageName string) error {
	var imagePullOptions image.PullOptions

	if strings.HasPrefix(imageName, "ghcr.io/") {

		token := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN")
		if token != "" {
			auth := base64.StdEncoding.EncodeToString([]byte("x:" + token))
			imagePullOptions = image.PullOptions{
				RegistryAuth: fmt.Sprintf("Basic %s", auth),
			}
		} else {
			log.Println("Failed to find credentials for GitHub container registry.")
		}
	} else if strings.Contains(imageName, ".azurecr.io/") {
		username := os.Getenv("AZURE_REGISTRY_USERNAME")
		password := os.Getenv("AZURE_REGISTRY_PASSWORD")

		registryName := strings.Split(imageName, "/")[0]

		if username != "" && password != "" {
			authConfig := registry.AuthConfig{
				Username:      username,
				Password:      password,
				ServerAddress: registryName,
			}

			encodedJSON, _ := json.Marshal(authConfig)
			authStr := base64.URLEncoding.EncodeToString(encodedJSON)

			imagePullOptions = image.PullOptions{
				RegistryAuth: authStr,
			}
		} else {
			log.Println("Failed to find credentials for Azure container registry.")
		}
	} else {
		log.Printf("Failed to find credentials for pulling image: %s\n.", imageName)
	}

	log.Printf("pulling image: %s\n", imageName)
	out, err := cli.ImagePull(ctx, imageName, imagePullOptions)
	if err != nil {
		return fmt.Errorf("failed to pull %v: %w", imageName, err)
	}
	_, _ = io.Copy(io.Discard, out)
	out.Close()

	return nil
}
