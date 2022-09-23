package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const jobID = "cli"
const dependabot = "dependabot"

// UpdaterImageName is the docker image used by the updater
var UpdaterImageName = "ghcr.io/dependabot/dependabot-updater:latest"

const (
	fetcherOutputFile = "output.json"
	fetcherRepoDir    = "repo"
	guestInputDir     = "/home/dependabot/dependabot-updater/job.json"
	guestOutputDir    = "/home/dependabot/dependabot-updater/output"
	guestRepoDir      = "/home/dependabot/dependabot-updater/repo"
)

type Updater struct {
	cli         *client.Client
	containerID string
	outputDir   string
	RepoDir     string
	inputPath   string
}

const (
	certsPath = "/etc/ssl/certs"
	dbotCert  = "/usr/local/share/ca-certificates/dbot-ca.crt"
)

// NewUpdater starts the update container interactively running /bin/sh, so it does not stop.
func NewUpdater(ctx context.Context, cli *client.Client, net *Networks, params *RunParams, prox *Proxy) (*Updater, error) {
	f := FileFetcherJobFile{Job: *params.Job}
	inputPath, err := WriteContainerInput(params.TempDir, f)
	if err != nil {
		return nil, fmt.Errorf("failed to write fetcher input: %w", err)
	}

	containerCfg := &container.Config{
		User:  dependabot,
		Image: UpdaterImageName,
		Cmd:   []string{"/bin/sh"},
		Tty:   true, // prevent container from stopping
	}
	outputDir, err := SetupOutputDir(params.TempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to setup fetcher output dir: %w", err)
	}

	repoDir := filepath.Join(outputDir, fetcherRepoDir)
	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: inputPath,
			Target: guestInputDir,
		}, {
			Type:   mount.TypeBind,
			Source: outputDir,
			Target: guestOutputDir,
		}, {
			Type:     mount.TypeBind,
			Source:   prox.CertPath,
			Target:   dbotCert,
			ReadOnly: true,
		}},
	}
	for _, v := range params.Volumes {
		local, remote, _ := strings.Cut(v, ":")
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: local,
			Target: remote,
		})
	}
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			net.noInternetName: {
				NetworkID: net.NoInternet.ID,
			},
		},
	}

	updaterContainer, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create updater container: %w", err)
	}

	if err := cli.ContainerStart(ctx, updaterContainer.ID, types.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start updater container: %w", err)
	}

	updater := &Updater{
		cli:         cli,
		containerID: updaterContainer.ID,
		outputDir:   outputDir,
		RepoDir:     repoDir,
		inputPath:   inputPath,
	}

	return updater, nil
}

// InstallCertificates runs update-ca-certificates as root, blocks until complete.
func (u *Updater) InstallCertificates(ctx context.Context) error {
	execCreate, err := u.cli.ContainerExecCreate(ctx, u.containerID, types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		User:         "root",
		Cmd:          []string{"update-ca-certificates"},
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	execResp, err := u.cli.ContainerExecAttach(ctx, execCreate.ID, types.ExecStartCheck{})
	if err != nil {
		return fmt.Errorf("failed to start exec: %w", err)
	}
	defer execResp.Close()

	// block until certs are installed or ctl-c
	ch := make(chan struct{})
	go func() {
		_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, execResp.Reader)
		ch <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
	}

	return nil
}

func userEnv(proxyURL string, apiPort int) []string {
	return []string{
		fmt.Sprintf("http_proxy=%s", proxyURL),
		fmt.Sprintf("HTTP_PROXY=%s", proxyURL),
		fmt.Sprintf("https_proxy=%s", proxyURL),
		fmt.Sprintf("HTTPS_PROXY=%s", proxyURL),
		fmt.Sprintf("DEPENDABOT_JOB_ID=%v", jobID),
		fmt.Sprintf("DEPENDABOT_JOB_TOKEN=%v", ""),
		fmt.Sprintf("DEPENDABOT_JOB_PATH=%v", guestInputDir),
		fmt.Sprintf("DEPENDABOT_OUTPUT_PATH=%v", filepath.Join(guestOutputDir, fetcherOutputFile)),
		fmt.Sprintf("DEPENDABOT_REPO_CONTENTS_PATH=%v", guestRepoDir),
		fmt.Sprintf("DEPENDABOT_API_URL=http://host.docker.internal:%v", apiPort),
		fmt.Sprintf("SSL_CERT_FILE=%v/ca-certificates.crt", certsPath),
		"UPDATER_ONE_CONTAINER=true",
		"UPDATER_DETERMINISTIC=true",
	}
}

// RunShell executes an interactive shell, blocks until complete.
func (u *Updater) RunShell(ctx context.Context, proxyURL string, apiPort int) error {
	execCreate, err := u.cli.ContainerExecCreate(ctx, u.containerID, types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		User:         dependabot,
		Env:          userEnv(proxyURL, apiPort),
		Cmd:          []string{"/bin/bash"},
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	execResp, err := u.cli.ContainerExecAttach(ctx, execCreate.ID, types.ExecStartCheck{})
	if err != nil {
		return fmt.Errorf("failed to start exec: %w", err)
	}

	ch := make(chan struct{})

	out := streams.NewOut(os.Stdout)
	_ = out.SetRawTerminal()
	in := streams.NewIn(os.Stdin)
	_ = in.SetRawTerminal()
	defer func() {
		out.RestoreTerminal()
		in.RestoreTerminal()
		in.Close()
	}()

	go func() {
		_, _ = stdcopy.StdCopy(out, os.Stderr, execResp.Reader)
		ch <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(execResp.Conn, in)
		ch <- struct{}{}
	}()

	_ = MonitorTtySize(ctx, out, u.cli, execCreate.ID, true)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		cancel()
	}

	return nil
}

// RunUpdate executes the update scripts as the dependabot user, blocks until complete.
func (u *Updater) RunUpdate(ctx context.Context, proxyURL string, apiPort int) error {
	execCreate, err := u.cli.ContainerExecCreate(ctx, u.containerID, types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		User:         dependabot,
		Env:          userEnv(proxyURL, apiPort),
		Cmd:          []string{"/bin/sh", "-c", "bin/run fetch_files && bin/run update_files"},
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	execResp, err := u.cli.ContainerExecAttach(ctx, execCreate.ID, types.ExecStartCheck{})
	if err != nil {
		return fmt.Errorf("failed to start exec: %w", err)
	}
	// blocks until update is complete or ctl-c
	ch := make(chan struct{})
	go func() {
		_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, execResp.Reader)
		ch <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
	}

	return nil
}

// Wait blocks until the condition is true.
func (u *Updater) Wait(ctx context.Context, condition container.WaitCondition) error {
	wait, errCh := u.cli.ContainerWait(ctx, u.containerID, condition)
	select {
	case v := <-wait:
		if v.StatusCode != 0 {
			return fmt.Errorf("updater exited with code: %v", v.StatusCode)
		}
	case err := <-errCh:
		return fmt.Errorf("updater error while waiting: %w", err)
	}
	return nil
}

// Close kills and deletes the container and deletes updater mount paths related to the run.
func (u *Updater) Close() error {
	defer os.Remove(u.inputPath)
	defer os.RemoveAll(u.outputDir)

	return u.cli.ContainerRemove(context.Background(), u.containerID, types.ContainerRemoveOptions{
		Force: true,
	})
}

// FileFetcherJobFile  is the payload passed to file updater containers.
type FileFetcherJobFile struct {
	Job model.Job `json:"job"`
}

func WriteContainerInput(tempDir string, input interface{}) (string, error) {
	// create file:
	out, err := os.CreateTemp(TempDir(tempDir), "containers-input-*.json")
	if err != nil {
		return "", fmt.Errorf("creating container input: %w", err)
	}
	defer out.Close()
	fn := out.Name()

	// TODO why does actions require this?
	_ = os.Chmod(fn, 0777)

	// fill with json:
	if err := json.NewEncoder(out).Encode(input); err != nil {
		_ = os.RemoveAll(fn)
		return "", fmt.Errorf("writing container input: %w", err)
	}
	return fn, nil
}

func SetupOutputDir(tempDir string) (string, error) {
	outputDir, err := os.MkdirTemp(TempDir(tempDir), "fetcher-output-")
	if err != nil {
		return "", fmt.Errorf("creating output tempdir: %w", err)
	}

	repoDir := filepath.Join(outputDir, fetcherRepoDir)
	if err := os.Mkdir(repoDir, 0777); err != nil {
		return "", fmt.Errorf("creating repo tempdir: %w", err)
	}
	_ = os.Chmod(outputDir, 0777)
	return outputDir, nil
}
