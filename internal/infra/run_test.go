package infra

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/server"

	"gopkg.in/yaml.v3"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api/types"
	"github.com/moby/moby/client"
)

func Test_checkCredAccess(t *testing.T) {
	addr := "127.0.0.1:3000"

	startTestServer := func() *http.Server {
		testServer := &http.Server{
			ReadHeaderTimeout: time.Second,
			Addr:              addr,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-OAuth-Scopes", "repo, write:packages")
				_, _ = w.Write([]byte("SUCCESS"))
			}),
		}
		go func() {
			_ = testServer.ListenAndServe()
		}()
		time.Sleep(1 * time.Millisecond) // allow time for the server to start
		return testServer
	}

	t.Run("returns error if the credential has write access", func(t *testing.T) {
		defaultApiEndpoint = "http://127.0.0.1:3000"
		testServer := startTestServer()
		defer func() {
			_ = testServer.Shutdown(context.Background())
		}()

		credentials := []model.Credential{{
			"token": "ghp_fake",
		}}
		err := checkCredAccess(context.Background(), nil, credentials)
		if err != ErrWriteAccess {
			t.Error("unexpected error", err)
		}
	})

	t.Run("it works with GitHub Enterprise", func(t *testing.T) {
		testServer := startTestServer()
		defer func() {
			_ = testServer.Shutdown(context.Background())
		}()

		credentials := []model.Credential{{
			"token": "ghp_fake",
		}}
		apiEndpoint := "http://" + addr
		job := &model.Job{Source: model.Source{APIEndpoint: &apiEndpoint}}
		err := checkCredAccess(context.Background(), job, credentials)
		if err != ErrWriteAccess {
			t.Error("unexpected error", err)
		}
	})
}

func Test_expandEnvironmentVariables(t *testing.T) {
	t.Run("injects environment variables", func(t *testing.T) {
		os.Setenv("ENV1", "value1")
		os.Setenv("ENV2", "value2")
		api := &server.API{}
		params := &RunParams{
			Creds: []model.Credential{{
				"type":     "test",
				"url":      "url",
				"username": "$ENV1",
				"pass":     "$ENV2",
			}},
		}

		expandEnvironmentVariables(api, params)

		if params.Creds[0]["username"] != "value1" {
			t.Error("expected username to be injected", params.Creds[0]["username"])
		}
		if params.Creds[0]["pass"] != "value2" {
			t.Error("expected pass to be injected", params.Creds[0]["pass"])
		}
		if api.Actual.Input.Credentials[0]["username"] != "$ENV1" {
			t.Error("expected username NOT to be injected", api.Actual.Input.Credentials[0]["username"])
		}
		if api.Actual.Input.Credentials[0]["pass"] != "$ENV2" {
			t.Error("expected pass NOT to be injected", api.Actual.Input.Credentials[0]["pass"])
		}
	})
}

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
	_ = addFileToArchive(tw, "/Dockerfile", 0644, dockerFile)
	_ = addFileToArchive(tw, "/test_main.go", 0644, testMain)
	_ = tw.Close()

	UpdaterImageName := "test-updater"
	resp, err := cli.ImageBuild(ctx, &buildContext, types.ImageBuildOptions{Tags: []string{UpdaterImageName}})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	defer func() {
		_, _ = cli.ImageRemove(ctx, UpdaterImageName, types.ImageRemoveOptions{})
	}()

	cred := model.Credential{
		"type":     "git_source",
		"host":     "github.com",
		"username": "x-access-token",
		"password": "$LOCAL_GITHUB_ACCESS_TOKEN",
	}

	os.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "test-token")
	err = Run(RunParams{
		PullImages: true,
		Job: &model.Job{
			PackageManager: "ecosystem",
			Source: model.Source{
				Repo: "org/name",
			},
		},
		Creds:        []model.Credential{cred},
		UpdaterImage: UpdaterImageName,
		Output:       "out.yaml",
	})
	if err != nil {
		t.Error(err)
	}

	f, err := os.Open("out.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var output model.Scenario
	if err = yaml.NewDecoder(f).Decode(&output); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(output.Input.Credentials, []model.Credential{cred}) {
		t.Error("unexpected credentials", output.Input.Credentials)
	}
	if output.Input.Credentials[0]["password"] != "$LOCAL_GITHUB_ACCESS_TOKEN" {
		t.Error("expected password to be masked")
	}
}

const dockerFile = `
FROM golang:1.21

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
