package infra

import (
	"context"
	"fmt"
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
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/docker/docker/pkg/stdcopy"
)

const proxyCertPath = "/usr/local/share/ca-certificates/custom-ca-cert.crt"

func init() {
	// needed for namesgenerator.GetRandomName
	rand.Seed(time.Now().UnixNano())
}

// ProxyImageName is the docker image used by the proxy
var ProxyImageName = "ghcr.io/github/dependabot-update-job-proxy/dependabot-update-job-proxy:latest"

type Proxy struct {
	cli             *client.Client
	containerID     string
	CertPath        string
	proxyConfigPath string
	containerName   string
	url             string
}

func NewProxy(ctx context.Context, cli *client.Client, params *RunParams, nets ...types.NetworkCreateResponse) (*Proxy, error) {
	// Generate secrets:
	ca, err := GenerateCertificateAuthority()
	if err != nil {
		return nil, fmt.Errorf("failed to generate cert: %w", err)
	}
	certPath := filepath.Join(TempDir(params.TempDir), "cert.crt")
	os.Remove(certPath)

	err = os.WriteFile(certPath, []byte(ca.Cert), 0777)
	if err != nil {
		return nil, fmt.Errorf("failed to write cert: %w", err)
	}

	// Generate and write configuration to disk:
	proxyConfig := &Config{
		Credentials: params.Creds,
		CA:          ca,
	}
	proxyConfigPath, err := StoreProxyConfig(params.TempDir, proxyConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to store proxy config: %w", err)
	}

	hostCfg := &container.HostConfig{
		AutoRemove: true,
		Mounts: []mount.Mount{{
			Type:     mount.TypeBind,
			Source:   proxyConfigPath,
			Target:   ConfigFilePath,
			ReadOnly: true,
		}},
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
		Image: ProxyImageName,
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
	for _, n := range nets {
		if err := cli.NetworkConnect(ctx, n.ID, proxyContainer.ID, &network.EndpointSettings{}); err != nil {
			return nil, fmt.Errorf("failed to connect to network: %w", err)
		}
	}

	if err := cli.ContainerStart(ctx, proxyContainer.ID, types.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return &Proxy{
		cli:             cli,
		containerID:     proxyContainer.ID,
		containerName:   hostName,
		url:             fmt.Sprintf("http://%s:1080", hostName),
		CertPath:        certPath,
		proxyConfigPath: proxyConfigPath,
	}, nil
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

	_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, out)
}

func (p *Proxy) Close() error {
	defer os.Remove(p.CertPath)
	defer os.Remove(p.proxyConfigPath)

	timeout := 5 * time.Second
	_ = p.cli.ContainerStop(context.Background(), p.containerID, &timeout)

	err := p.cli.ContainerRemove(context.Background(), p.containerID, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		return fmt.Errorf("failed to remove proxy container: %w", err)
	}
	return nil
}
