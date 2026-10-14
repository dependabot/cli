package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

func hostVolumes(ctx context.Context, volumes []string) ([]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create Docker client for volume translation: %w", err)
	}
	defer cli.Close()
	id, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("identify Action container: %w", err)
	}
	self, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspect Action container mounts: %w", err)
	}
	return translateVolumes(volumes, self.Mounts)
}

func translateVolumes(volumes []string, mounts []container.MountPoint) ([]string, error) {
	translated := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		parts := strings.Split(volume, ":")
		if len(parts) < 2 || len(parts) > 3 || !filepath.IsAbs(parts[0]) || !filepath.IsAbs(parts[1]) ||
			(len(parts) == 3 && parts[2] != "ro") {
			return nil, fmt.Errorf("volumes must be absolute source:destination[:ro] paths")
		}
		source := filepath.Clean(parts[0])
		var best *container.MountPoint
		var relative string
		for i := range mounts {
			m := &mounts[i]
			rel, err := filepath.Rel(m.Destination, source)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if best == nil || len(m.Destination) > len(best.Destination) {
				best, relative = m, rel
			}
		}
		if best == nil || best.Type != mount.TypeBind {
			return nil, fmt.Errorf("volume source %q is not inside an Action bind mount", source)
		}
		if !best.RW && len(parts) != 3 {
			return nil, fmt.Errorf("volume source %q is read-only in the Action container", source)
		}
		// Sibling containers resolve bind sources on the daemon host, not in this container.
		parts[0] = filepath.Join(best.Source, relative)
		translated = append(translated, strings.Join(parts, ":"))
	}
	return translated, nil
}
