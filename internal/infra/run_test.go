package infra

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func Test_generateIgnoreConditions(t *testing.T) {
	const (
		outputFileName = "test_output"
		dependencyName = "dep1"
		version        = "1.0.0"
	)

	t.Run("generates ignore conditions", func(t *testing.T) {
		runParams := &RunParams{
			Output: outputFileName,
		}
		v := "1.0.0"
		actual := &model.Scenario{
			Output: []model.Output{{
				Type: "create_pull_request",
				Expect: model.UpdateWrapper{Data: model.CreatePullRequest{
					Dependencies: []model.Dependency{{
						Name:    dependencyName,
						Version: &v,
					}},
				}},
			}},
		}
		if err := generateIgnoreConditions(runParams, actual); err != nil {
			t.Fatal(err)
		}
		if len(actual.Input.Job.IgnoreConditions) != 1 {
			t.Error("expected 1 ignore condition to be generated, got", len(actual.Input.Job.IgnoreConditions))
		}
		ignore := actual.Input.Job.IgnoreConditions[0]
		if reflect.DeepEqual(ignore, &model.Condition{
			DependencyName:     dependencyName,
			Source:             outputFileName,
			VersionRequirement: ">" + version,
		}) {
			t.Error("unexpected ignore condition", ignore)
		}
	})

	t.Run("handles removed dependency", func(t *testing.T) {
		runParams := &RunParams{
			Output: outputFileName,
		}
		actual := &model.Scenario{
			Output: []model.Output{{
				Type: "create_pull_request",
				Expect: model.UpdateWrapper{Data: model.CreatePullRequest{
					Dependencies: []model.Dependency{{
						Name:    dependencyName,
						Removed: true,
					}},
				}},
			}},
		}
		if err := generateIgnoreConditions(runParams, actual); err != nil {
			t.Fatal(err)
		}
		if len(actual.Input.Job.IgnoreConditions) != 0 {
			t.Error("expected 0 ignore condition to be generated, got", len(actual.Input.Job.IgnoreConditions))
		}
	})
}

func Test_checkLocalGitRepo(t *testing.T) {
	t.Run("fills in the commit and branch when blank", func(t *testing.T) {
		params := RunParams{
			Job: &model.Job{},
		}
		if err := checkLocalGitRepo(&params); err != nil {
			t.Fatal(err)
		}
		if params.Job.Source.Branch == nil || params.Job.Source.Commit == nil {
			t.Error("Failed to get commit or branch")
		}
	})
	t.Run("errors since the branch doesn't match", func(t *testing.T) {
		branch := "branch"
		params := RunParams{
			Job: &model.Job{
				Source: model.Source{
					Branch: &branch,
				},
			},
		}
		if err := checkLocalGitRepo(&params); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("makes no change when commit is set", func(t *testing.T) {
		commit := "commit"
		params := RunParams{
			Job: &model.Job{
				Source: model.Source{
					Commit: &commit,
				},
			},
		}
		if err := checkLocalGitRepo(&params); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	var buildContext bytes.Buffer
	tw := tar.NewWriter(&buildContext)
	addFileToArchive(tw, "/Dockerfile", 0644, dockerFile)
	addFileToArchive(tw, "/test_main.go", 0644, testMain)
	tw.Close()

	UpdaterImageName = "test-updater"
	resp, err := cli.ImageBuild(ctx, &buildContext, types.ImageBuildOptions{Tags: []string{UpdaterImageName}})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	defer func() {
		_, _ = cli.ImageRemove(ctx, UpdaterImageName, types.ImageRemoveOptions{})
	}()

	err = Run(RunParams{
		PullImages: true,
		Job: &model.Job{
			PackageManager: "ecosystem",
			Source: model.Source{
				Repo: "org/name",
			},
		},
		TempDir: "/tmp",
	})
	if err != nil {
		t.Error(err)
	}
}

func addFileToArchive(tw *tar.Writer, path string, mode int64, content string) {
	header := &tar.Header{
		Name: path,
		Size: int64(len(content)),
		Mode: mode,
	}

	err := tw.WriteHeader(header)
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(tw, bytes.NewReader([]byte(content)))
	if err != nil {
		panic(err)
	}
}

const dockerFile = `
FROM golang:1.19

# needed to run update-ca-certificates
RUN apt-get update && apt-get install -y ca-certificates
# cli will try to start as dependabot
RUN useradd -ms /bin/bash dependabot

# need to be the user for permissions to work
USER dependabot
WORKDIR /home/dependabot
COPY *.go .
# cli runs bin/run (twice) to do an update, so put exe there
RUN go mod init cli_test && go mod tidy && go build -o bin/run
`

const testMain = `package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func main() {
	if os.Args[1] == "update_files" {
		return
	}
	var wg sync.WaitGroup

	// print the line that fails
	log.SetFlags(log.Lshortfile)

	go connectivityCheck(&wg)
	checkIfRoot()
	proxyCheck()

	wg.Wait()
}

func checkIfRoot() {
	buf := &bytes.Buffer{}
	cmd := exec.Command("id", "-u")
	cmd.Stdout = buf
	if err := cmd.Run(); err != nil {
		log.Fatalln(err)
	}
	userID := strings.TrimSpace(buf.String())
	if userID == "0" {
		log.Fatalln("User is root")
	}
}

func connectivityCheck(wg *sync.WaitGroup) {
	wg.Add(1)

	var d net.Dialer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := d.DialContext(ctx, "tcp", "1.1.1.1:22")
	if err != nil && err.Error() != "dial tcp 1.1.1.1:22: i/o timeout" {
		log.Fatalln(err)
	}
	if err == nil {
		log.Fatalln("a connection shouldn't be possible")
	}

	wg.Done()
}

func proxyCheck() {
	res, err := http.Get("https://example.com")
	if err != nil {
		log.Fatalln(err)
	}
	defer res.Body.Close()
	var v any
	if err = xml.NewDecoder(res.Body).Decode(&v); err != nil {
		log.Fatalln(err)
	}
}
`
