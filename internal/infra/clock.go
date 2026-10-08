package infra

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
)

const guestClockDir = "/opt/dependabot-cli-clock"

//go:embed clock/clock.sh
var clockShell string

//go:embed clock/clock.rb
var clockRuby string

//go:embed clock/clock.cjs
var clockNode string

func (u *Updater) installClock(ctx context.Context, recordedAt time.Time) error {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range []struct{ name, content string }{
		{"clock.sh", fmt.Sprintf(clockShell, recordedAt.UnixMilli())},
		{"clock.rb", clockRuby},
		{"clock.cjs", clockNode},
	} {
		if err := addFileToArchive(writer, guestClockDir+"/"+file.name, 0555, file.content); err != nil {
			return fmt.Errorf("failed to archive replay clock: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close replay clock archive: %w", err)
	}
	if err := u.cli.CopyToContainer(ctx, u.containerID, "/", &archive, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("failed to copy replay clock to container: %w", err)
	}
	u.recordedClock = true
	return nil
}

func (u *Updater) command(shell string, args ...string) []string {
	offset := 0
	if u.recordedClock {
		offset = 1
	}
	command := make([]string, len(args)+1+offset)
	if u.recordedClock {
		command[0] = guestClockDir + "/clock.sh"
	}
	command[offset] = shell
	copy(command[offset+1:], args)
	return command
}
