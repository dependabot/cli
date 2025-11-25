package infra

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/goware/prefixer"
	"github.com/moby/moby/pkg/stdcopy"
)

const jobID = "cli"
const (
	root       = "root"
	dependabot = "dependabot"
)

const (
	guestInputDir = "/home/dependabot/dependabot-updater/job.json"
	guestOutput   = "/home/dependabot/dependabot-updater/output.json"
	guestRepoDir  = "/home/dependabot/dependabot-updater/repo"

	caseSensitiveContainerRoot    = "/dpdbot"
	caseSensitiveRepoContentsPath = "/dpdbot/repo"

	caseInsensitiveContainerRoot    = "/nocase"
	caseInsensitiveRepoContentsPath = "/nocase/repo"

	StorageImageName = "ghcr.io/dependabot/dependabot-storage"
	storageUser      = "dpduser"
	storagePass      = "dpdpass"
)

type Updater struct {
	cli                *client.Client
	containerID        string
	storageContainerID string
	storageVolumes     []string

	// ExitCode is set once an Updater command has completed.
	ExitCode *int
}

const (
	certsPath = "/etc/ssl/certs"
	dbotCert  = "/usr/local/share/ca-certificates/dbot-ca.crt"
)

// NewUpdater starts the update container interactively running /bin/sh, so it does not stop.
func NewUpdater(ctx context.Context, cli *client.Client, net *Networks, params *RunParams, prox *Proxy, collector *Collector) (*Updater, error) {
	containerCfg := &container.Config{
		User:  dependabot,
		Image: params.UpdaterImage,
		Cmd:   []string{"/bin/sh"},
		Tty:   true, // prevent container from stopping
	}

	if params.CollectorConfigPath != "" {
		containerCfg.Env = append(
			containerCfg.Env,
			[]string{
				"OTEL_ENABLED=true",
				fmt.Sprintf("OTEL_EXPORTER_OTLP_ENDPOINT=%s", collector.url),
			}...)
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

	storageContainerID := ""
	storageVolumes := []string{}
	if params.Job.UseCaseInsensitiveFileSystem() {
		storageContainerID, storageVolumes, err = createStorageVolumes(hostCfg, ctx, cli, net, params.StorageImage)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage volumes: %w", err)
		}
	}

	var netCfg *network.NetworkingConfig
	// Only configure networking if networks exist (when credentials are present)
	if net != nil {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				net.noInternetName: {
					NetworkID: net.NoInternet.ID,
				},
			},
		}
	}

	updaterContainer, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create updater container: %w", err)
	}

	updater := &Updater{
		cli:                cli,
		containerID:        updaterContainer.ID,
		storageContainerID: storageContainerID,
		storageVolumes:     storageVolumes,
	}

	// Get the cert from proxy if it exists, otherwise use empty string
	cert := ""
	if prox != nil {
		cert = prox.ca.Cert
	}
	if err = putUpdaterInputs(ctx, cli, cert, updaterContainer.ID, params.Job); err != nil {
		updater.Close()
		return nil, err
	}

	if err = cli.ContainerStart(ctx, updaterContainer.ID, container.StartOptions{}); err != nil {
		updater.Close()
		return nil, fmt.Errorf("failed to start updater container: %w", err)
	}

	return updater, nil
}

func createStorageVolumes(hostCfg *container.HostConfig, ctx context.Context, cli *client.Client, net *Networks, storageImageName string) (storageContainerID string, volumeNames []string, err error) {
	log.Printf("Preparing case insensitive filesystem")

	// create container hosting the storage
	storageContainerCfg := &container.Config{
		User:  root,
		Image: storageImageName,
		Tty:   true, // prevent container from stopping
	}
	storageHostCfg := &container.HostConfig{}
	storageNetCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			net.noInternetName: {
				NetworkID: net.NoInternet.ID, // no external access for this container
			},
		},
	}
	storageContainer, err := cli.ContainerCreate(ctx, storageContainerCfg, storageHostCfg, storageNetCfg, nil, "")
	if err != nil {
		err = fmt.Errorf("failed to create storage container: %w", err)
		return
	}
	storageContainerID = storageContainer.ID
	caseSensitiveVolumeName := "dpdbot-storage-" + storageContainer.ID[:12]
	caseInsensitiveVolumeName := "dpdbot-nocase-" + storageContainer.ID[:12]
	volumeNames = []string{caseSensitiveVolumeName, caseInsensitiveVolumeName}

	defer func() {
		if err != nil {
			removeStorageVolume(cli, ctx, caseSensitiveVolumeName)
			removeStorageVolume(cli, ctx, caseInsensitiveVolumeName)
		}
	}()

	// start storage container
	if err = cli.ContainerStart(ctx, storageContainer.ID, container.StartOptions{}); err != nil {
		err = fmt.Errorf("failed to start storage container: %w", err)
		return
	}

	// wait for port 445 to be listening on the storage container
	log.Printf("  waiting for storage container port 445 to be ready")
	err = waitForPort(ctx, cli, storageContainer.ID, 445)
	if err != nil {
		err = fmt.Errorf("failed to wait for storage container port 445: %w", err)
		return
	}

	// add volume mounts from the storage container; container IP is needed because the host is making a direct connection and it has not been given internet access
	inspect, err := cli.ContainerInspect(ctx, storageContainerID)
	if err != nil {
		err = fmt.Errorf("failed to inspect storage container: %w", err)
		return
	}
	storageContainerAddress := inspect.NetworkSettings.Networks[net.noInternetName].IPAddress
	addStorageMounts(hostCfg, storageContainerAddress, caseSensitiveVolumeName, caseSensitiveContainerRoot, caseInsensitiveVolumeName, caseInsensitiveContainerRoot)
	return
}

func removeStorageVolume(cli *client.Client, ctx context.Context, name string) error {
	listOptions := volume.ListOptions{
		Filters: filters.NewArgs(
			filters.KeyValuePair{Key: "name", Value: name},
		),
	}
	ls, err := cli.VolumeList(ctx, listOptions)
	if err != nil {
		return err
	}

	for _, v := range ls.Volumes {
		if v.Name == name {
			err = cli.VolumeRemove(ctx, v.Name, true)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func addStorageMounts(hostCfg *container.HostConfig, storageContainerAddress string, caseSensitiveVolumeName, caseSensitiveContainerRoot, caseInsensitiveVolumeName, caseInsensitiveContainerRoot string) {
	const cifsVolumeType = "cifs"
	localShareName := fmt.Sprintf("//%s/dpdbot", storageContainerAddress)
	connectionOptions := fmt.Sprintf("username=%s,password=%s,uid=1000,gid=1000", storageUser, storagePass)

	// create case-sensitive layer
	hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
		Type:   mount.TypeVolume,
		Source: caseSensitiveVolumeName,
		Target: caseSensitiveContainerRoot,
		VolumeOptions: &mount.VolumeOptions{
			DriverConfig: &mount.Driver{
				Name: "local",
				Options: map[string]string{
					"type":   cifsVolumeType,
					"device": localShareName,
					"o":      connectionOptions,
				},
			},
		},
	})

	// create case-insensitive layer
	hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
		Type:   mount.TypeVolume,
		Source: caseInsensitiveVolumeName,
		Target: caseInsensitiveContainerRoot,
		VolumeOptions: &mount.VolumeOptions{
			DriverConfig: &mount.Driver{
				Name: "local",
				Options: map[string]string{
					"type":   cifsVolumeType,
					"device": localShareName,
					"o":      fmt.Sprintf("nocase,%s", connectionOptions),
				},
			},
		},
	})
}

func putUpdaterInputs(ctx context.Context, cli *client.Client, cert, id string, job *model.Job) error {
	opt := container.CopyToContainerOptions{}
	// Only copy cert if it exists (when proxy is present)
	if cert != "" {
		if t, err := tarball(dbotCert, cert); err != nil {
			return fmt.Errorf("failed to create cert tarball: %w", err)
		} else if err = cli.CopyToContainer(ctx, id, "/", t, opt); err != nil {
			return fmt.Errorf("failed to copy cert to container: %w", err)
		}
	}

	data, err := JobFile{Job: job}.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal job file: %w", err)
	}
	if t, err := tarball(guestInputDir, data); err != nil {
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

func userEnv(proxyURL string, apiUrl string, job *model.Job, additionalEnvVars []string) []string {
	envVars := []string{
		"GITHUB_ACTIONS=true", // sets exit code when fetch fails
		fmt.Sprintf("http_proxy=%s", proxyURL),
		fmt.Sprintf("HTTP_PROXY=%s", proxyURL),
		fmt.Sprintf("https_proxy=%s", proxyURL),
		fmt.Sprintf("HTTPS_PROXY=%s", proxyURL),
		fmt.Sprintf("DEPENDABOT_JOB_ID=%v", firstNonEmpty(os.Getenv("DEPENDABOT_JOB_ID"), jobID)),
		fmt.Sprintf("DEPENDABOT_JOB_TOKEN=%v", ""),
		fmt.Sprintf("DEPENDABOT_JOB_PATH=%v", guestInputDir),
		fmt.Sprintf("DEPENDABOT_OUTPUT_PATH=%v", guestOutput),
		fmt.Sprintf("DEPENDABOT_API_URL=%s", apiUrl),
		fmt.Sprintf("SSL_CERT_FILE=%v/ca-certificates.crt", certsPath),
		"UPDATER_ONE_CONTAINER=true",
		"UPDATER_DETERMINISTIC=true",
	}

	if job.UseCaseInsensitiveFileSystem() {
		envVars = append(envVars, fmt.Sprintf("DEPENDABOT_CASE_INSENSITIVE_REPO_CONTENTS_PATH=%s", caseInsensitiveRepoContentsPath))
		envVars = append(envVars, fmt.Sprintf("DEPENDABOT_REPO_CONTENTS_PATH=%s", caseSensitiveRepoContentsPath))
	} else {
		envVars = append(envVars, fmt.Sprintf("DEPENDABOT_REPO_CONTENTS_PATH=%s", guestRepoDir))
	}

	envVars = append(envVars, additionalEnvVars...)

	return envVars
}

// RunShell executes an interactive shell, blocks until complete.
func (u *Updater) RunShell(ctx context.Context, proxyURL string, apiUrl string, job *model.Job, additionalEnvVars []string) error {
	execCreate, err := u.cli.ContainerExecCreate(ctx, u.containerID, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		User:         dependabot,
		Env:          append(userEnv(proxyURL, apiUrl, job, additionalEnvVars), "DEBUG=1"),
		Cmd:          []string{"/bin/bash"},
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	execResp, err := u.cli.ContainerExecAttach(ctx, execCreate.ID, container.ExecAttachOptions{})
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

// RunCmd executes the update scripts as the dependabot user, blocks until complete.
func (u *Updater) RunCmd(ctx context.Context, cmd, user string, env ...string) error {
	execCreate, err := u.cli.ContainerExecCreate(ctx, u.containerID, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		User:         user,
		Env:          env,
		Cmd:          []string{"/bin/sh", "-c", cmd},
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	execResp, err := u.cli.ContainerExecAttach(ctx, execCreate.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("failed to start exec: %w", err)
	}

	r, w := io.Pipe()
	go func() {
		_, _ = io.Copy(os.Stderr, prefixer.New(r, "updater | "))
	}()

	ch := make(chan struct{})
	go func() {
		_, _ = stdcopy.StdCopy(w, w, execResp.Reader)
		ch <- struct{}{}
	}()

	// blocks until update is complete or ctl-c
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
	}

	// check the exit code of the command
	execInspect, err := u.cli.ContainerExecInspect(ctx, execCreate.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	u.ExitCode = &execInspect.ExitCode

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
func (u *Updater) Close() (err error) {
	defer func() {
		removeErr := u.cli.ContainerRemove(context.Background(), u.containerID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			err = fmt.Errorf("failed to remove proxy container: %w", removeErr)
		}

		for _, v := range u.storageVolumes {
			removeErr = u.cli.VolumeRemove(context.Background(), v, true)
			if removeErr != nil {
				err = fmt.Errorf("failed to remove storage volume %s: %w", v, removeErr)
			}
		}

		if u.storageContainerID != "" {
			removeErr = u.cli.ContainerRemove(context.Background(), u.storageContainerID, container.RemoveOptions{Force: true})
			if removeErr != nil {
				err = fmt.Errorf("failed to remove storage container: %w", removeErr)
			}
		}
	}()

	// Handle non-zero exit codes.
	containerInfo, inspectErr := u.cli.ContainerInspect(context.Background(), u.containerID)
	if inspectErr != nil {
		return fmt.Errorf("failed to inspect proxy container: %w", inspectErr)
	}
	if containerInfo.State.ExitCode != 0 {
		return fmt.Errorf("updater container exited with non-zero exit code: %d", containerInfo.State.ExitCode)
	}

	return
}

// JobFile  is the payload passed to file updater containers.
type JobFile struct {
	Job *model.Job `json:"job"`
}

func (j JobFile) ToJSON() (string, error) {
	data, err := json.Marshal(j)
	return string(data), err
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

func waitForPort(ctx context.Context, cli *client.Client, containerID string, port int) error {
	const maxAttempts = 5
	const sleepDuration = time.Second

	// check /proc/net/tcp for the requested port; n.b., it is hex encoded and 4 characters wide
	testCmd := fmt.Sprintf("test -f /proc/net/tcp && grep ' *\\d+: [A-F0-9]{8}:%04X ' /proc/net/tcp >/dev/null 2>&1", port)

	for i := range maxAttempts {
		execCreate, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
			AttachStdout: false,
			AttachStderr: false,
			User:         root,
			Cmd:          []string{"/bin/sh", "-c", testCmd},
		})
		if err != nil {
			return fmt.Errorf("failed to create exec for port check: %w", err)
		}

		execResp, err := cli.ContainerExecAttach(ctx, execCreate.ID, container.ExecAttachOptions{})
		if err != nil {
			return fmt.Errorf("failed to attach to exec for port check: %w", err)
		}

		// wait for completion and check the exit code
		execResp.Close()
		execInspect, err := cli.ContainerExecInspect(ctx, execCreate.ID)
		if err != nil {
			return fmt.Errorf("failed to inspect exec: %w", err)
		}

		if execInspect.ExitCode == 0 {
			// port is listening
			log.Printf("  port %d is listening after %d attempts", port, i+1)

			// in a few instances, the port is open but the service isn't yet ready for connections
			// no more reliable method has been found, other than a short delay
			time.Sleep(sleepDuration)
			return nil
		}

		if i < maxAttempts-1 {
			time.Sleep(sleepDuration)
		}
	}

	return fmt.Errorf("port %d is not listening after %d attempts", port, maxAttempts)
}
