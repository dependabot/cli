package infra

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	archive "github.com/moby/go-archive"
)

// guestReachInputDir is where the reachability input set lives inside the
// container. It mirrors `reach run`'s -workdir contract: it holds the target/
// checkout, alerts.json, an optional sbom.json, and the annotations feed, and
// reach writes its outputs (refined.csv, paths.json) here. The input set is
// copied IN and the outputs copied back OUT (not bind-mounted), so this works
// even when dependabot-cli itself runs inside a container (e.g. `act` or a
// containerized runner) where a host bind mount would not resolve.
const guestReachInputDir = "/home/dependabot/reach-run"

// ReachabilityParams configures a reachability job. It reuses the proxy and the
// isolated networking of a normal update job - so the reachability-cli's
// dependency fetches are credential-injected by the proxy, and the container
// trusts the proxy's MITM CA (via update-ca-certificates + SSL_CERT_FILE,
// exactly as the updater does) - but runs the dependabot-reachability-cli
// (`reach run`) in place of an ecosystem updater. The reachability image plays
// the "updater" role in the update/graph pattern.
type ReachabilityParams struct {
	// Job carries the repo Source + package manager; it drives proxy/credential
	// setup (reach itself never sees the credentials).
	Job *model.Job
	// Creds are the registry credentials the proxy injects into fetches.
	Creds []model.Credential
	// ReachabilityImage bundles `reach` + the CodeQL CLI + the query pack.
	// TODO: publish this image (reach + codeql + node) and set a default.
	ReachabilityImage string
	// ProxyImage is the proxy container image.
	ProxyImage string
	// InputDir is the local directory whose contents are copied into the
	// container at guestReachInputDir (and whose outputs are copied back). It must
	// contain the target/ checkout, alerts.json, and the annotations feed; an
	// sbom.json enables the SBOM-first inventory (else target/package-lock.json).
	InputDir string
	// Annotations is the annotation-feed path RELATIVE to InputDir (e.g. "annotations").
	Annotations string
	// CodeqlPath is the path to the codeql binary inside the reachability image.
	CodeqlPath string
	// PullImages pulls the proxy + reachability images if absent.
	PullImages bool
	// Timeout optionally bounds the whole run.
	Timeout time.Duration
	// ExtraHosts adds /etc/hosts entries to the proxy (testing).
	ExtraHosts []string
	// ProxyCertPath is an extra certificate for the proxy to trust.
	ProxyCertPath string
	// CacheDir optionally caches proxy requests.
	CacheDir string
	// ApiUrl is passed to the proxy; reachability has no fake API server, so a
	// placeholder is fine.
	ApiUrl string
}

// RunReachability runs a reachability job. It starts the proxy and the
// reachability container on isolated networks (the reachability container on
// no-internet, the proxy bridging to the internet), makes the container trust
// the proxy CA, and runs `reach run` against the bind-mounted input set. reach's
// dependency fetches therefore go through the proxy (credential injection for
// private registries) with the proxy CA trusted - the same isolation the
// updater gets. Outputs land in InputDir via the bind mount.
func RunReachability(params ReachabilityParams) (err error) {
	if params.Job == nil {
		return fmt.Errorf("job is required (repo source + credentials for the proxy)")
	}
	if params.ReachabilityImage == "" {
		return fmt.Errorf("reachability image is required")
	}
	if params.InputDir == "" {
		return fmt.Errorf("input dir is required (target/, alerts.json, sbom.json, annotations/)")
	}
	// Record the job type; the reachability image plays the updater role.
	params.Job.Command = model.ReachabilityCommand

	absInput, err := filepath.Abs(params.InputDir)
	if err != nil {
		return fmt.Errorf("input dir: %w", err)
	}

	var ctx context.Context
	var cancel func()
	if params.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), params.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
	}()

	// Reuse the proxy machinery by mapping onto a RunParams; the credential
	// expansion + proxy setup are then identical to a normal job.
	rp := &RunParams{
		Job:           params.Job,
		Creds:         params.Creds,
		ProxyImage:    firstNonEmpty(params.ProxyImage, ProxyImageName),
		ProxyCertPath: params.ProxyCertPath,
		CacheDir:      params.CacheDir,
		ExtraHosts:    params.ExtraHosts,
		ApiUrl:        firstNonEmpty(params.ApiUrl, "http://host.docker.internal:0"),
	}
	for _, cred := range rp.Creds {
		for key, value := range cred {
			if s, ok := value.(string); ok {
				cred[key] = os.ExpandEnv(s)
			}
		}
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	if params.PullImages {
		if err = pullImage(ctx, cli, rp.ProxyImage); err != nil {
			return err
		}
		if err = pullImage(ctx, cli, params.ReachabilityImage); err != nil {
			return err
		}
	}

	networks, err := NewNetworks(ctx, cli)
	if err != nil {
		return fmt.Errorf("failed to create networks: %w", err)
	}
	defer networks.Close()

	prox, err := NewProxy(ctx, cli, rp, networks)
	if err != nil {
		return err
	}
	defer func() {
		if e := prox.Close(); e != nil {
			err = e
		}
	}()
	go prox.TailLogs(ctx, cli)

	// Start the reachability container: /bin/sh + Tty so it stays up, connected
	// only to the no-internet network so every fetch egresses through the proxy.
	// The input set is copied IN and the outputs OUT (not bind-mounted), so this
	// works even when dependabot-cli runs inside a container (act / a
	// containerized runner), where a host bind mount would not resolve.
	containerCfg := &container.Config{
		User:  dependabot,
		Image: params.ReachabilityImage,
		Cmd:   []string{"/bin/sh"},
		Tty:   true,
	}
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networks.noInternetName: {NetworkID: networks.NoInternet.ID},
		},
	}
	created, err := cli.ContainerCreate(ctx, containerCfg, nil, netCfg, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create reachability container: %w", err)
	}
	reach := &Updater{cli: cli, containerID: created.ID}
	defer func() {
		if e := reach.Close(); e != nil {
			err = e
		}
	}()

	// Copy the proxy CA into the container so update-ca-certificates trusts it
	// (same cert the updater path installs).
	if t, terr := tarball(dbotCert, prox.ca.Cert); terr != nil {
		return fmt.Errorf("failed to create cert tarball: %w", terr)
	} else if err = cli.CopyToContainer(ctx, created.ID, "/", t, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("failed to copy proxy CA to container: %w", err)
	}

	if err = cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start reachability container: %w", err)
	}

	// Copy the input set into the container (owned by dependabot so reach can
	// write its outputs alongside them).
	if err = copyDirIntoContainer(ctx, cli, reach, absInput, guestReachInputDir); err != nil {
		return err
	}

	if err = reach.RunCmd(ctx, "update-ca-certificates", root); err != nil {
		return err
	}

	// userEnv sets http(s)_proxy=<proxy> and SSL_CERT_FILE, so reach's registry
	// fetches route through the proxy with the proxy CA trusted - no extra cert
	// handling needed.
	env := userEnv(prox.url, rp.ApiUrl, rp.Job, nil)
	if err = reach.RunCmd(ctx, reachRunCommand(params), dependabot, env...); err != nil {
		return err
	}
	if reach.ExitCode != nil && *reach.ExitCode != 0 {
		return fmt.Errorf("reachability run exited with code %d", *reach.ExitCode)
	}

	// Copy the outputs back out to the input dir so the caller can read them.
	// refined.csv is required; paths.json / verdicts.csv are best-effort (paths
	// only exists when something was reachable).
	if err = copyOutFile(ctx, cli, created.ID, path.Join(guestReachInputDir, "refined.csv"), filepath.Join(absInput, "refined.csv")); err != nil {
		return fmt.Errorf("copy out refined.csv: %w", err)
	}
	for _, name := range []string{"paths.json", "verdicts.csv"} {
		_ = copyOutFile(ctx, cli, created.ID, path.Join(guestReachInputDir, name), filepath.Join(absInput, name))
	}
	return nil
}

// copyDirIntoContainer copies the contents of localDir into containerDir inside
// the running container and chowns them to the dependabot user, so reach (which
// runs as dependabot) can read the inputs and write its outputs there. Modeled
// on dependabot-cli's putCloneDir, minus the git-repo initialization.
func copyDirIntoContainer(ctx context.Context, cli *client.Client, u *Updater, localDir, containerDir string) error {
	if err := u.RunCmd(ctx, "mkdir -p "+containerDir, dependabot); err != nil {
		return fmt.Errorf("create %s in container: %w", containerDir, err)
	}
	r, err := archive.TarWithOptions(localDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("tar %s: %w", localDir, err)
	}
	if err := cli.CopyToContainer(ctx, u.containerID, containerDir, r, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("copy inputs into container: %w", err)
	}
	if err := u.RunCmd(ctx, "chown -R dependabot "+containerDir, root); err != nil {
		return fmt.Errorf("chown %s in container: %w", containerDir, err)
	}
	return nil
}

// copyOutFile copies a single file out of the container to dst on the local
// filesystem (CopyFromContainer returns a tar stream with the one entry).
func copyOutFile(ctx context.Context, cli *client.Client, containerID, src, dst string) error {
	reader, _, err := cli.CopyFromContainer(ctx, containerID, src)
	if err != nil {
		return err
	}
	defer reader.Close()
	tr := tar.NewReader(reader)
	if _, err := tr.Next(); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(tr, 1<<30)) //nolint:gosec // outputs are small CSV/JSON
	return err
}

// reachRunCommand builds the `reach run` invocation executed inside the
// reachability container. Inputs are read from the copied-in input dir; the
// annotation feed and the codeql binary are located from the params.
func reachRunCommand(params ReachabilityParams) string {
	annotations := path.Join(guestReachInputDir, firstNonEmpty(params.Annotations, "annotations"))
	cmd := fmt.Sprintf("reach run -workdir %s -annotations %s", guestReachInputDir, annotations)
	if params.CodeqlPath != "" {
		cmd += " -codeql " + params.CodeqlPath
	}
	return cmd
}
