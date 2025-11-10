package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/goware/prefixer"
	"github.com/moby/moby/pkg/namesgenerator"
	"github.com/moby/moby/pkg/stdcopy"
)

const proxyCertPath = "/usr/local/share/ca-certificates/custom-ca-cert.crt"

// ProxyImageName is the default Docker image used by the proxy
const ProxyImageName = "ghcr.io/dependabot/proxy:latest"

type Proxy struct {
	cli         *client.Client
	containerID string
	url         string
	ca          CertificateAuthority
}

func NewProxy(ctx context.Context, cli *client.Client, params *RunParams, nets *Networks) (*Proxy, error) {
	// Generate secrets:
	ca, err := GenerateCertificateAuthority()
	if err != nil {
		return nil, fmt.Errorf("failed to generate cert: %w", err)
	}

	// Generate and write configuration to disk:
	proxyConfig := &Config{
		Credentials: params.Creds,
		CA:          ca,
	}

	hostCfg := &container.HostConfig{
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
	}
	hostCfg.ExtraHosts = append(hostCfg.ExtraHosts, params.ExtraHosts...)
	if params.ProxyCertPath != "" {
		if !path.IsAbs(params.ProxyCertPath) {
			// needs to be absolute, assume it is relative to the working directory
			var dir string
			dir, err = os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("couldn't get working directory: %w", err)
			}
			params.ProxyCertPath = path.Join(dir, params.ProxyCertPath)
		}
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   params.ProxyCertPath,
			Target:   proxyCertPath,
			ReadOnly: true,
		})
	}
	hostCfg.ExtraHosts = append(hostCfg.ExtraHosts, params.ExtraHosts...)
	if params.CacheDir != "" {
		_ = os.MkdirAll(params.CacheDir, 0744)
		cacheDir, _ := filepath.Abs(params.CacheDir)
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: cacheDir,
			Target: "/cache",
		})
	}
	config := &container.Config{
		Image: params.ProxyImage,
		Env: []string{
			"HTTP_PROXY=" + os.Getenv("HTTP_PROXY"),
			"HTTPS_PROXY=" + os.Getenv("HTTPS_PROXY"),
			"NO_PROXY=" + os.Getenv("NO_PROXY"),
			"JOB_ID=" + jobID,
			"PROXY_CACHE=true",
			"LOG_RESPONSE_BODY_ON_AUTH_FAILURE=true",
			"ACTIONS_ID_TOKEN_REQUEST_TOKEN=" + os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"),
			"ACTIONS_ID_TOKEN_REQUEST_URL=" + os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"),
		},
		Entrypoint: []string{
			"sh", "-c", "update-ca-certificates && /dependabot-proxy",
		},
	}
	hostName := namesgenerator.GetRandomName(1)
	proxyContainer, err := cli.ContainerCreate(ctx, config, hostCfg, nil, nil, hostName)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy container: %w", err)
	}

	proxy := &Proxy{
		cli:         cli,
		containerID: proxyContainer.ID,
		ca:          ca,
	}

	if err = putProxyConfig(ctx, cli, proxyConfig, proxyContainer.ID); err != nil {
		_ = proxy.Close()
		return nil, fmt.Errorf("failed to connect to network: %w", err)
	}

	// nil check since tests don't always need networks
	if nets != nil {
		if err = cli.NetworkConnect(ctx, nets.NoInternet.ID, proxyContainer.ID, &network.EndpointSettings{}); err != nil {
			_ = proxy.Close()
			return nil, fmt.Errorf("failed to connect to internal network: %w", err)
		}
		if err = cli.NetworkConnect(ctx, nets.Internet.ID, proxyContainer.ID, &network.EndpointSettings{}); err != nil {
			_ = proxy.Close()
			return nil, fmt.Errorf("failed to connect to external network: %w", err)
		}
	}

	if err = cli.ContainerStart(ctx, proxyContainer.ID, container.StartOptions{}); err != nil {
		_ = proxy.Close()
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	containerInfo, err := cli.ContainerInspect(ctx, proxyContainer.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect proxy container: %w", err)
	}
	if nets != nil {
		proxy.url = fmt.Sprintf("http://%s:1080", containerInfo.NetworkSettings.Networks[nets.noInternetName].IPAddress)
	} else {
		// This should only happen during testing, adding a warning in case
		log.Println("Warning: no-internet network not found")
	}

	return proxy, nil
}

func putProxyConfig(ctx context.Context, cli *client.Client, config *Config, id string) error {
	opt := container.CopyToContainerOptions{}

	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if t, err := tarball(ConfigFilePath, string(data)); err != nil {
		return fmt.Errorf("failed to create cert tarball: %w", err)
	} else if err = cli.CopyToContainer(ctx, id, "/", t, opt); err != nil {
		return fmt.Errorf("failed to copy cert to container: %w", err)
	}
	return nil
}

func (p *Proxy) TailLogs(ctx context.Context, cli *client.Client) {
	out, err := cli.ContainerLogs(ctx, p.containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return
	}

	r, w := io.Pipe()
	go func() {
		_, _ = io.Copy(os.Stderr, prefixer.New(r, "  proxy | "))
	}()
	_, _ = stdcopy.StdCopy(w, w, out)
}

func (p *Proxy) Close() (err error) {
	defer func() {
		removeErr := p.cli.ContainerRemove(context.Background(), p.containerID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			err = fmt.Errorf("failed to remove proxy container: %w", removeErr)
		}
	}()

	// Check the error code if the container has already exited, so we can pass it along to the caller. If the proxy
	//crashes we want the CLI to error out.
	containerInfo, inspectErr := p.cli.ContainerInspect(context.Background(), p.containerID)
	if inspectErr != nil {
		return fmt.Errorf("failed to inspect proxy container: %w", inspectErr)
	}
	if containerInfo.State.ExitCode != 0 {
		return fmt.Errorf("proxy container exited with non-zero exit code: %d", containerInfo.State.ExitCode)
	}

	timeout := 5
	_ = p.cli.ContainerStop(context.Background(), p.containerID, container.StopOptions{Timeout: &timeout})

	return err
}
