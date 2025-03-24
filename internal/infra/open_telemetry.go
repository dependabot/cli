package infra

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/goware/prefixer"
	"github.com/moby/moby/client"
	"github.com/moby/moby/pkg/stdcopy"
)

// CollectorImageName is the default Docker image used
const CollectorImageName = "ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:latest"

const CollectorConfigPath = "/etc/otelcol-contrib/config.yaml"

const sslCertificates = "/etc/ssl/certs/ca-certificates.crt"

type Collector struct {
	cli         *client.Client
	containerID string
	url         string
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

	if err = cli.ContainerStart(ctx, collectorContainer.ID, container.StartOptions{}); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to start collector container: %w", err)
	}

	containerInfo, err := cli.ContainerInspect(ctx, collector.containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect collector container: %w", err)
	}
	if net != nil {
		collector.url = fmt.Sprintf("http://%s:4318", containerInfo.NetworkSettings.Networks[net.noInternetName].IPAddress)
	} else {
		// This should only happen during testing, adding a warning in case
		log.Println("Warning: no-internet network not found")
	}

	return collector, nil
}

func (c *Collector) TailLogs(ctx context.Context, cli *client.Client) {
	out, err := cli.ContainerLogs(ctx, c.containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return
	}

	r, w := io.Pipe()
	go func() {
		_, _ = io.Copy(os.Stderr, prefixer.New(r, "   otel | "))
	}()
	_, _ = stdcopy.StdCopy(w, w, out)
}

// Close stops and removes the container.
func (c *Collector) Close() error {
	timeout := 30
	_ = c.cli.ContainerStop(context.Background(), c.containerID, container.StopOptions{Timeout: &timeout})

	err := c.cli.ContainerRemove(context.Background(), c.containerID, container.RemoveOptions{Force: true})
	if err != nil {
		return fmt.Errorf("failed to remove collector container: %w", err)
	}
	return nil
}
