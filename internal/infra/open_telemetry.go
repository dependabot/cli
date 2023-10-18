package infra

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/moby/moby/client"
	"os"
	"path"
	"path/filepath"
)

// CollectorImageName is the default Docker image used
const CollectorImageName = "ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:latest"

const CollectorConfigPath = "/etc/otelcol-contrib/config.yaml"

const sslCertificates = "/etc/ssl/certs/ca-certificates.crt"

type Collector struct {
	cli         *client.Client
	containerID string
}

// NewCollector starts the OpenTelemetry collector container.
func NewCollector(ctx context.Context, cli *client.Client, net *Networks, params *RunParams, proxy *Proxy) (*Collector, error) {
	hostCfg := &container.HostConfig{
		AutoRemove: false,
	}

	containerCfg := &container.Config{
		Image: params.CollectorImage,
		Env: []string{
			fmt.Sprintf("HTTP_PROXY=%s", proxy.url),
			fmt.Sprintf("HTTPS_PROXY=%s", proxy.url),
		},
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			net.noInternetName: {
				NetworkID: net.NoInternet.ID,
			},
		},
	}

	if params.CollectorConfigPath != "" {
		if !filepath.IsAbs(params.CollectorConfigPath) {
			// needs to be absolute, assume it is relative to the working directory
			var dir string
			dir, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("couldn't get working directory: %w", err)
			}
			params.CollectorConfigPath = path.Join(dir, params.CollectorConfigPath)
		}
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   params.CollectorConfigPath,
			Target:   CollectorConfigPath,
			ReadOnly: true,
		})
	}

	collectorContainer, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create collector container: %w", err)
	}

	collector := &Collector{
		cli:         cli,
		containerID: collectorContainer.ID,
	}

	opt := types.CopyToContainerOptions{}
	if t, err := tarball(sslCertificates, proxy.ca.Cert); err != nil {
		return nil, fmt.Errorf("failed to create cert tarball: %w", err)
	} else if err = cli.CopyToContainer(ctx, collector.containerID, "/", t, opt); err != nil {
		return nil, fmt.Errorf("failed to copy cert to container: %w", err)
	}

	if err = cli.ContainerStart(ctx, collectorContainer.ID, types.ContainerStartOptions{}); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to start collector container: %w", err)
	}

	return collector, nil

}

// Close kills and deletes the container and deletes updater mount paths related to the run.
func (u *Collector) Close() error {
	return u.cli.ContainerRemove(context.Background(), u.containerID, types.ContainerRemoveOptions{
		Force: true,
	})
}
