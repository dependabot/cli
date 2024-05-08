package infra

import (
	"context"
	"github.com/docker/docker/api/types/container"
	"log"
	"os"
	gosignal "os/signal"
	"runtime"
	"time"

	"github.com/docker/cli/cli/streams"
	"github.com/moby/moby/client"
	"github.com/moby/sys/signal"
)

// NOTE: This file was copied from https://github.com/docker/cli/command/container/tty.go
// and adapted to not pull in the whole docker CLI object.

// resizeTtyTo resizes tty to specific height and width
func resizeTtyTo(ctx context.Context, c *client.Client, id string, height, width uint, isExec bool) error {
	if height == 0 && width == 0 {
		return nil
	}

	options := container.ResizeOptions{
		Height: height,
		Width:  width,
	}

	var err error
	if isExec {
		err = c.ContainerExecResize(ctx, id, options)
	} else {
		err = c.ContainerResize(ctx, id, options)
	}

	if err != nil {
		log.Printf("Error resize: %s\r", err)
	}
	return err
}

// resizeTty is to resize the tty with cli out's tty size
func resizeTty(ctx context.Context, out *streams.Out, cli *client.Client, id string, isExec bool) error {
	height, width := out.GetTtySize()
	return resizeTtyTo(ctx, cli, id, height, width, isExec)
}

// initTtySize is to init the tty's size to the same as the window, if there is an error, it will retry 10 times.
func initTtySize(ctx context.Context, out *streams.Out, cli *client.Client, id string, isExec bool, resizeTtyFunc func(context.Context, *streams.Out, *client.Client, string, bool) error) {
	rttyFunc := resizeTtyFunc
	if rttyFunc == nil {
		rttyFunc = resizeTty
	}
	if err := rttyFunc(ctx, out, cli, id, isExec); err != nil {
		go func() {
			var err error
			for retry := 0; retry < 10; retry++ {
				time.Sleep(time.Duration(retry+1) * 10 * time.Millisecond)
				if err = rttyFunc(ctx, out, cli, id, isExec); err == nil {
					break
				}
			}
			if err != nil {
				log.Println("failed to resize tty, using default size")
			}
		}()
	}
}

// MonitorTtySize updates the container tty size when the terminal tty changes size
func MonitorTtySize(ctx context.Context, out *streams.Out, cli *client.Client, id string, isExec bool) error {
	initTtySize(ctx, out, cli, id, isExec, resizeTty)
	if runtime.GOOS == "windows" {
		go func() {
			prevH, prevW := out.GetTtySize()
			for {
				time.Sleep(time.Millisecond * 250)
				h, w := out.GetTtySize()

				if prevW != w || prevH != h {
					_ = resizeTty(ctx, out, cli, id, isExec)
				}
				prevH = h
				prevW = w
			}
		}()
	} else {
		sigchan := make(chan os.Signal, 1)
		gosignal.Notify(sigchan, signal.SIGWINCH)
		go func() {
			for range sigchan {
				_ = resizeTty(ctx, out, cli, id, isExec)
			}
		}()
	}
	return nil
}
