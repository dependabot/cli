package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/goware/prefixer"
	"io"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/moby/moby/client"
	"github.com/moby/moby/pkg/namesgenerator"
	"github.com/moby/moby/pkg/stdcopy"
)

const proxyCertPath = "/usr/local/share/ca-certificates/custom-ca-cert.crt"

func init() {
	// needed for namesgenerator.GetRandomName
	rand.Seed(time.Now().UnixNano())
}

// ProxyImageName is the default Docker image used by the proxy
const ProxyImageName = "ghcr.io/github/dependabot-update-job-proxy/dependabot-update-job-proxy:latest"

type Proxy struct {
	cli           *client.Client
	containerID   string
	containerName string
	url           string
	ca            CertificateAuthority
}

func NewProxy(ctx context.Context, cli *client.Client, params *RunParams, nets ...types.NetworkCreateResponse) (*Proxy, error) {
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
		AutoRemove: true,
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
	}
	hostCfg.ExtraHosts = append(hostCfg.ExtraHosts, params.ExtraHosts...)
	if params.ProxyCertPath != "" {
		if !strings.HasPrefix(params.ProxyCertPath, "/") {
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
			"JOB_ID=" + jobID,
			"PROXY_CACHE=true",
		},
		Entrypoint: []string{
			"sh", "-c", "update-ca-certificates && /update-job-proxy",
		},
	}
	hostName := namesgenerator.GetRandomName(1)
	proxyContainer, err := cli.ContainerCreate(ctx, config, hostCfg, nil, nil, hostName)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy container: %w", err)
	}

	proxy := &Proxy{
		cli:           cli,
		containerID:   proxyContainer.ID,
		containerName: hostName,
		url:           fmt.Sprintf("http://%s:1080", hostName),
		ca:            ca,
	}

	if err = putProxyConfig(ctx, cli, proxyConfig, proxyContainer.ID); err != nil {
		_ = proxy.Close()
		return nil, fmt.Errorf("failed to connect to network: %w", err)
	}

	for _, n := range nets {
		if err = cli.NetworkConnect(ctx, n.ID, proxyContainer.ID, &network.EndpointSettings{}); err != nil {
			_ = proxy.Close()
			return nil, fmt.Errorf("failed to connect to network: %w", err)
		}
	}

	if err = cli.ContainerStart(ctx, proxyContainer.ID, types.ContainerStartOptions{}); err != nil {
		_ = proxy.Close()
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return proxy, nil
}

func putProxyConfig(ctx context.Context, cli *client.Client, config *Config, id string) error {
	opt := types.CopyToContainerOptions{}

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
	out, err := cli.ContainerLogs(ctx, p.containerID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return
	}

	r, w := io.Pipe()
	go func() {
		_, _ = io.Copy(os.Stderr, prefixer.New(r, "proxy | "))
	}()
	_, _ = stdcopy.StdCopy(w, w, out)
}

func (p *Proxy) Close() error {
	timeout := 5
	_ = p.cli.ContainerStop(context.Background(), p.containerID, container.StopOptions{Timeout: &timeout})

	err := p.cli.ContainerRemove(context.Background(), p.containerID, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		return fmt.Errorf("failed to remove proxy container: %w", err)
	}
	return nil
}
