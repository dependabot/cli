package infra

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/moby/moby/client"
	"github.com/moby/moby/pkg/stdcopy"
)

const jobID = "cli"
const dependabot = "dependabot"

// UpdaterImageName is the docker image used by the updater
var UpdaterImageName = "ghcr.io/dependabot/dependabot-updater:latest"

const (
	guestInputDir = "/home/dependabot/dependabot-updater/job.json"
	guestOutput   = "/home/dependabot/dependabot-updater/output.json"
	guestRepoDir  = "/home/dependabot/dependabot-updater/repo"
)

type Updater struct {
	cli         *client.Client
	containerID string
}

const (
	certsPath = "/etc/ssl/certs"
	dbotCert  = "/usr/local/share/ca-certificates/dbot-ca.crt"
)

// NewUpdater starts the update container interactively running /bin/sh, so it does not stop.
func NewUpdater(ctx context.Context, cli *client.Client, net *Networks, params *RunParams, prox *Proxy) (*Updater, error) {
	containerCfg := &container.Config{
		User:  dependabot,
		Image: UpdaterImageName,
		Cmd:   []string{"/bin/sh"},
		Tty:   true, // prevent container from stopping
	}
	hostCfg := &container.HostConfig{}
	var err error
	for _, v := range params.Volumes {
		var local, remote string
		var readOnly bool
		local, remote, readOnly, err = mountOptions(v)
		if err != nil {
			return nil, err
		}

		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   local,
			Target:   remote,
			ReadOnly: readOnly,
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

	updater := &Updater{
		cli:         cli,
		containerID: updaterContainer.ID,
	}

	if err = putUpdaterInputs(ctx, cli, prox.ca.Cert, updaterContainer.ID, params.Job); err != nil {
		updater.Close()
		return nil, err
	}

	if err = cli.ContainerStart(ctx, updaterContainer.ID, types.ContainerStartOptions{}); err != nil {
		updater.Close()
		return nil, fmt.Errorf("failed to start updater container: %w", err)
	}

	return updater, nil
}

func putUpdaterInputs(ctx context.Context, cli *client.Client, cert, id string, job *model.Job) error {
	opt := types.CopyToContainerOptions{}
	if t, err := tarball(dbotCert, cert); err != nil {
		return fmt.Errorf("failed to create cert tarball: %w", err)
	} else if err = cli.CopyToContainer(ctx, id, "/", t, opt); err != nil {
		return fmt.Errorf("failed to copy cert to container: %w", err)
	}

	data, err := json.Marshal(FileFetcherJobFile{Job: job})
	if err != nil {
		return fmt.Errorf("failed to marshal job file: %w", err)
	}
	if t, err := tarball(guestInputDir, string(data)); err != nil {
		return fmt.Errorf("failed create input tarball: %w", err)
	} else if err = cli.CopyToContainer(ctx, id, "/", t, opt); err != nil {
		return fmt.Errorf("failed to copy input to container: %w", err)
	}
	return nil
}

var ErrInvalidVolume = fmt.Errorf("invalid volume syntax")

func mountOptions(v string) (local, remote string, readOnly bool, err error) {
	parts := strings.Split(v, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return "", "", false, ErrInvalidVolume
	}
	local = parts[0]
	remote = parts[1]
	if len(parts) == 3 {
		if parts[2] != "ro" {
			return "", "", false, ErrInvalidVolume
		}
		readOnly = true
	}
	if !path.IsAbs(local) {
		wd, _ := os.Getwd()
		local = filepath.Clean(filepath.Join(wd, local))
	}
	return local, remote, readOnly, nil
}

func userEnv(proxyURL string, apiPort int) []string {
	return []string{
		"GITHUB_ACTIONS=true", // sets exit code when fetch fails
		fmt.Sprintf("http_proxy=%s", proxyURL),
		fmt.Sprintf("HTTP_PROXY=%s", proxyURL),
		fmt.Sprintf("https_proxy=%s", proxyURL),
		fmt.Sprintf("HTTPS_PROXY=%s", proxyURL),
		fmt.Sprintf("DEPENDABOT_JOB_ID=%v", jobID),
		fmt.Sprintf("DEPENDABOT_JOB_TOKEN=%v", ""),
		fmt.Sprintf("DEPENDABOT_JOB_PATH=%v", guestInputDir),
		fmt.Sprintf("DEPENDABOT_OUTPUT_PATH=%v", guestOutput),
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
		Env:          append(userEnv(proxyURL, apiPort), "DEBUG=1"),
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
		Cmd:          []string{"/bin/sh", "-c", "update-ca-certificates && bin/run fetch_files && bin/run update_files"},
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
	return u.cli.ContainerRemove(context.Background(), u.containerID, types.ContainerRemoveOptions{
		Force: true,
	})
}

// FileFetcherJobFile  is the payload passed to file updater containers.
type FileFetcherJobFile struct {
	Job *model.Job `json:"job"`
}

func tarball(name, contents string) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	t := tar.NewWriter(&buf)
	if err := addFileToArchive(t, name, 0777, contents); err != nil {
		return nil, fmt.Errorf("adding file to archive: %w", err)
	}
	return &buf, t.Flush()
}

func addFileToArchive(tw *tar.Writer, name string, mode int64, content string) error {
	header := &tar.Header{
		Name: name,
		Size: int64(len(content)),
		Mode: mode,
	}

	err := tw.WriteHeader(header)
	if err != nil {
		return err
	}

	_, err = tw.Write([]byte(content))
	if err != nil {
		return err
	}

	return nil
}
