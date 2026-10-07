package infra

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
)

const (
	testStorageContainerID = "123456789012storage"
	testUpdaterContainerID = "updater-container"
	testDockerAPIVersion   = "1.44"
)

type cleanupCallTracker struct {
	mu               sync.Mutex
	containerRemoves map[string]int
	volumeRemoves    map[string]int
}

func newCleanupCallTracker() *cleanupCallTracker {
	return &cleanupCallTracker{
		containerRemoves: make(map[string]int),
		volumeRemoves:    make(map[string]int),
	}
}

func (c *cleanupCallTracker) recordContainerRemove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.containerRemoves[id]++
}

func (c *cleanupCallTracker) recordVolumeRemove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.volumeRemoves[name]++
}

func (c *cleanupCallTracker) containerRemoveCount(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.containerRemoves[id]
}

func (c *cleanupCallTracker) volumeRemoveCount(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.volumeRemoves[name]
}

func testStorageVolumeNames() []string {
	return []string{
		"dpdbot-storage-" + testStorageContainerID[:12],
		"dpdbot-nocase-" + testStorageContainerID[:12],
	}
}

func newTestDockerClient(t *testing.T, handler http.Handler) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion(testDockerAPIVersion),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cli.Close()
	})
	return cli
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("failed to encode Docker API response: %v", err)
	}
}

func handleStorageCleanupRequest(t *testing.T, tracker *cleanupCallTracker, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()

	volumeNames := testStorageVolumeNames()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1.44/volumes":
		writeJSON(t, w, map[string]any{
			"Volumes": []map[string]string{
				{"Name": volumeNames[0]},
				{"Name": volumeNames[1]},
			},
		})
		return true
	case r.Method == http.MethodDelete && r.URL.Path == "/v1.44/volumes/"+volumeNames[0]:
		tracker.recordVolumeRemove(volumeNames[0])
		w.WriteHeader(http.StatusNoContent)
		return true
	case r.Method == http.MethodDelete && r.URL.Path == "/v1.44/volumes/"+volumeNames[1]:
		tracker.recordVolumeRemove(volumeNames[1])
		w.WriteHeader(http.StatusNoContent)
		return true
	case r.Method == http.MethodDelete && r.URL.Path == "/v1.44/containers/"+testStorageContainerID:
		tracker.recordContainerRemove(testStorageContainerID)
		w.WriteHeader(http.StatusNoContent)
		return true
	default:
		return false
	}
}

func caseInsensitiveTestJob() *model.Job {
	return &model.Job{
		PackageManager: "nuget",
		Experiments: model.Experiment{
			"use_case_insensitive_filesystem": true,
		},
	}
}

func TestNewUpdaterContainerCreateSecurity(t *testing.T) {
	tests := []struct {
		name            string
		apiVersion      string
		securityOptions []string
		infoPath        string
		createPath      string
	}{
		{
			name:            "modern seccomp",
			apiVersion:      "1.44",
			securityOptions: []string{"name=seccomp,profile=builtin"},
			infoPath:        "/v1.44/info",
			createPath:      "/v1.44/containers/create",
		},
		{
			name:            "legacy API 1.24 seccomp",
			apiVersion:      api.MinSupportedAPIVersion,
			securityOptions: []string{"seccomp"},
			infoPath:        "/v1.24/info",
			createPath:      "/v1.24/containers/create",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var createRequest container.CreateRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == test.infoPath:
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(system.Info{
						SecurityOptions: test.securityOptions,
					})
				case r.Method == http.MethodPost && r.URL.Path == test.createPath:
					if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
						t.Errorf("failed to decode container create request: %v", err)
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(container.CreateResponse{ID: "updater-id"})
				case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/containers/updater-id/archive"):
					_, _ = io.Copy(io.Discard, r.Body)
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/updater-id/start"):
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected Docker API request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			cli, err := client.NewClientWithOpts(
				client.WithHost(server.URL),
				client.WithVersion(test.apiVersion),
				client.WithHTTPClient(server.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()

			networks := &Networks{
				NoInternet:     network.CreateResponse{ID: "no-internet-id"},
				noInternetName: "no-internet",
			}
			params := &RunParams{
				Job:          &model.Job{},
				UpdaterImage: "updater-image",
				Volumes:      []string{"fixture:/workspace:ro"},
			}
			proxy := &Proxy{ca: CertificateAuthority{Cert: "test certificate"}}

			if _, err := NewUpdater(context.Background(), cli, networks, params, proxy, nil); err != nil {
				t.Fatal(err)
			}

			if createRequest.Config == nil {
				t.Fatal("container create request did not include Config")
			}
			if createRequest.HostConfig == nil {
				t.Fatal("container create request did not include HostConfig")
			}
			if createRequest.NetworkingConfig == nil {
				t.Fatal("container create request did not include NetworkingConfig")
			}

			if createRequest.Config.User != dependabot {
				t.Errorf("updater user = %q, want %q", createRequest.Config.User, dependabot)
			}
			if createRequest.Config.Image != params.UpdaterImage {
				t.Errorf("updater image = %q, want %q", createRequest.Config.Image, params.UpdaterImage)
			}
			if !createRequest.Config.Tty {
				t.Error("updater TTY is disabled")
			}
			if !reflect.DeepEqual(createRequest.Config.Cmd, strslice.StrSlice{"/bin/sh"}) {
				t.Errorf("updater command = %v, want [/bin/sh]", createRequest.Config.Cmd)
			}

			hostCfg := createRequest.HostConfig
			if !reflect.DeepEqual(hostCfg.CapDrop, strslice.StrSlice{"ALL"}) {
				t.Errorf("CapDrop = %v, want [ALL]", hostCfg.CapDrop)
			}
			if len(hostCfg.CapAdd) != 0 {
				t.Errorf("CapAdd = %v, want no added capabilities", hostCfg.CapAdd)
			}
			if !reflect.DeepEqual(hostCfg.SecurityOpt, []string{"no-new-privileges=true", "seccomp=builtin"}) {
				t.Errorf("SecurityOpt = %v, want explicit no-new-privileges and built-in seccomp", hostCfg.SecurityOpt)
			}
			for _, option := range hostCfg.SecurityOpt {
				if strings.Contains(option, "unconfined") {
					t.Errorf("SecurityOpt must not disable seccomp: %v", hostCfg.SecurityOpt)
				}
			}

			if len(hostCfg.Mounts) != 1 {
				t.Fatalf("mount count = %d, want 1", len(hostCfg.Mounts))
			}
			if hostCfg.Mounts[0].Target != "/workspace" || !hostCfg.Mounts[0].ReadOnly {
				t.Errorf("updater mount = %+v, want read-only /workspace mount", hostCfg.Mounts[0])
			}

			endpoints := createRequest.NetworkingConfig.EndpointsConfig
			if len(endpoints) != 1 {
				t.Fatalf("network endpoint count = %d, want 1", len(endpoints))
			}
			endpoint, ok := endpoints[networks.noInternetName]
			if !ok {
				t.Fatalf("updater is not attached to %q: %v", networks.noInternetName, endpoints)
			}
			if endpoint.NetworkID != networks.NoInternet.ID {
				t.Errorf("network ID = %q, want %q", endpoint.NetworkID, networks.NoInternet.ID)
			}
		})
	}
}

func TestNewUpdaterRejectsUnavailableSeccomp(t *testing.T) {
	var containerCreateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(system.Info{
				SecurityOptions: []string{"name=cgroupns"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			containerCreateCalls++
			http.Error(w, "updater container must not be created", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.44"),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	_, err = NewUpdater(
		context.Background(),
		cli,
		&Networks{},
		&RunParams{Job: &model.Job{}, UpdaterImage: "updater-image"},
		&Proxy{},
		nil,
	)
	if !errors.Is(err, ErrSeccompUnavailable) {
		t.Fatalf("NewUpdater error = %v, want %v", err, ErrSeccompUnavailable)
	}
	if containerCreateCalls != 0 {
		t.Errorf("ContainerCreate called %d times, want 0", containerCreateCalls)
	}
}

func TestNewUpdaterRejectsMalformedSecurityOptions(t *testing.T) {
	var containerCreateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(system.Info{
				SecurityOptions: []string{"name=seccomp,profile"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			containerCreateCalls++
			http.Error(w, "updater container must not be created", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.44"),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	_, err = NewUpdater(
		context.Background(),
		cli,
		&Networks{},
		&RunParams{Job: &model.Job{}, UpdaterImage: "updater-image"},
		&Proxy{},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "failed to decode Docker security options") {
		t.Fatalf("NewUpdater error = %v, want decoding context", err)
	}
	if containerCreateCalls != 0 {
		t.Errorf("ContainerCreate called %d times, want 0", containerCreateCalls)
	}
}

func TestCreateStorageVolumesCleansUpAfterFailure(t *testing.T) {
	tests := []struct {
		name         string
		startFails   bool
		waitFails    bool
		inspectFails bool
		errorText    string
	}{
		{
			name:       "start",
			startFails: true,
			errorText:  "failed to start storage container",
		},
		{
			name:      "readiness",
			waitFails: true,
			errorText: "failed to wait for storage container port 445",
		},
		{
			name:         "inspect",
			inspectFails: true,
			errorText:    "failed to inspect storage container",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracker := newCleanupCallTracker()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cli := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
					writeJSON(t, w, container.CreateResponse{ID: testStorageContainerID})
				case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/"+testStorageContainerID+"/start":
					if test.startFails {
						http.Error(w, "start failed", http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/"+testStorageContainerID+"/json":
					if test.inspectFails {
						http.Error(w, "inspect failed", http.StatusInternalServerError)
						return
					}
					writeJSON(t, w, map[string]any{
						"NetworkSettings": map[string]any{
							"Networks": map[string]any{
								"no-internet": map[string]string{"IPAddress": "192.0.2.10"},
							},
						},
					})
				default:
					if !handleStorageCleanupRequest(t, tracker, w, r) {
						t.Errorf("unexpected Docker API request: %s %s", r.Method, r.URL.Path)
						http.NotFound(w, r)
					}
				}
			}))

			waitForStoragePort := storagePortWaiter(func(context.Context, *client.Client, string, int) error {
				if test.waitFails {
					cancel()
					return errors.New("storage is not ready")
				}
				return nil
			})

			_, _, err := createStorageVolumes(
				&container.HostConfig{},
				ctx,
				cli,
				&Networks{
					NoInternet:     network.CreateResponse{ID: "no-internet-id"},
					noInternetName: "no-internet",
				},
				"storage-image",
				waitForStoragePort,
			)
			if err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("createStorageVolumes error = %v, want %q", err, test.errorText)
			}
			if got := tracker.containerRemoveCount(testStorageContainerID); got != 1 {
				t.Errorf("storage container removals = %d, want 1", got)
			}
			for _, name := range testStorageVolumeNames() {
				if got := tracker.volumeRemoveCount(name); got != 1 {
					t.Errorf("storage volume %q removals = %d, want 1", name, got)
				}
			}
		})
	}
}

func TestNewUpdaterContainerCreateFailureCleansUpStorageResources(t *testing.T) {
	tracker := newCleanupCallTracker()
	var storageCreateCalls, updaterCreateCalls int
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/info":
			writeJSON(t, w, system.Info{SecurityOptions: []string{"name=seccomp,profile=builtin"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
			var request container.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("failed to decode container create request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			switch request.Config.Image {
			case "storage-image":
				storageCreateCalls++
				writeJSON(t, w, container.CreateResponse{ID: testStorageContainerID})
			case "updater-image":
				updaterCreateCalls++
				http.Error(w, "updater create failed", http.StatusInternalServerError)
			default:
				t.Errorf("unexpected container image: %q", request.Config.Image)
				http.Error(w, "unexpected image", http.StatusBadRequest)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/"+testStorageContainerID+"/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/"+testStorageContainerID+"/json":
			writeJSON(t, w, map[string]any{
				"NetworkSettings": map[string]any{
					"Networks": map[string]any{
						"no-internet": map[string]string{"IPAddress": "192.0.2.10"},
					},
				},
			})
		default:
			if !handleStorageCleanupRequest(t, tracker, w, r) {
				t.Errorf("unexpected Docker API request: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}
		}
	}))

	_, err := newUpdater(
		ctx,
		cli,
		&Networks{
			NoInternet:     network.CreateResponse{ID: "no-internet-id"},
			noInternetName: "no-internet",
		},
		&RunParams{
			Job:          caseInsensitiveTestJob(),
			UpdaterImage: "updater-image",
			StorageImage: "storage-image",
		},
		&Proxy{},
		nil,
		func(context.Context, *client.Client, string, int) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "failed to create updater container") {
		t.Fatalf("newUpdater error = %v, want updater create failure", err)
	}
	if storageCreateCalls != 1 || updaterCreateCalls != 1 {
		t.Errorf("container create calls = storage %d, updater %d; want 1 each", storageCreateCalls, updaterCreateCalls)
	}
	if got := tracker.containerRemoveCount(testStorageContainerID); got != 1 {
		t.Errorf("storage container removals = %d, want 1", got)
	}
	for _, name := range testStorageVolumeNames() {
		if got := tracker.volumeRemoveCount(name); got != 1 {
			t.Errorf("storage volume %q removals = %d, want 1", name, got)
		}
	}
}

func TestNewUpdaterTransfersStorageCleanupOwnership(t *testing.T) {
	tracker := newCleanupCallTracker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/info":
			writeJSON(t, w, system.Info{SecurityOptions: []string{"name=seccomp,profile=builtin"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
			var request container.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("failed to decode container create request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			switch request.Config.Image {
			case "storage-image":
				writeJSON(t, w, container.CreateResponse{ID: testStorageContainerID})
			case "updater-image":
				writeJSON(t, w, container.CreateResponse{ID: testUpdaterContainerID})
			default:
				t.Errorf("unexpected container image: %q", request.Config.Image)
				http.Error(w, "unexpected image", http.StatusBadRequest)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/"+testStorageContainerID+"/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/"+testStorageContainerID+"/json":
			writeJSON(t, w, map[string]any{
				"NetworkSettings": map[string]any{
					"Networks": map[string]any{
						"no-internet": map[string]string{"IPAddress": "192.0.2.10"},
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1.44/containers/"+testUpdaterContainerID+"/archive":
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/"+testUpdaterContainerID+"/start":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/containers/"+testUpdaterContainerID+"/json":
			writeJSON(t, w, map[string]any{
				"State": map[string]any{"ExitCode": 0},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.44/containers/"+testUpdaterContainerID:
			tracker.recordContainerRemove(testUpdaterContainerID)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1.44/volumes/"):
			tracker.recordVolumeRemove(strings.TrimPrefix(r.URL.Path, "/v1.44/volumes/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			if !handleStorageCleanupRequest(t, tracker, w, r) {
				t.Errorf("unexpected Docker API request: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}
		}
	}))

	updater, err := newUpdater(
		ctx,
		cli,
		&Networks{
			NoInternet:     network.CreateResponse{ID: "no-internet-id"},
			noInternetName: "no-internet",
		},
		&RunParams{
			Job:          caseInsensitiveTestJob(),
			UpdaterImage: "updater-image",
			StorageImage: "storage-image",
		},
		&Proxy{ca: CertificateAuthority{Cert: "test certificate"}},
		nil,
		func(context.Context, *client.Client, string, int) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := tracker.containerRemoveCount(testStorageContainerID); got != 0 {
		t.Fatalf("storage container removed before ownership transfer completed: %d", got)
	}
	for _, name := range testStorageVolumeNames() {
		if got := tracker.volumeRemoveCount(name); got != 0 {
			t.Fatalf("storage volume %q removed before Updater.Close: %d", name, got)
		}
	}

	if err := updater.Close(); err != nil {
		t.Fatal(err)
	}
	if got := tracker.containerRemoveCount(testUpdaterContainerID); got != 1 {
		t.Errorf("updater container removals = %d, want 1", got)
	}
	if got := tracker.containerRemoveCount(testStorageContainerID); got != 1 {
		t.Errorf("storage container removals = %d, want 1", got)
	}
	for _, name := range testStorageVolumeNames() {
		if got := tracker.volumeRemoveCount(name); got != 1 {
			t.Errorf("storage volume %q removals = %d, want 1", name, got)
		}
	}
}

func TestAddStorageMountsPreservesUpdaterSecurity(t *testing.T) {
	hostCfg := newUpdaterHostConfig()

	addStorageMounts(
		hostCfg,
		"192.0.2.1",
		"case-sensitive-volume",
		caseSensitiveContainerRoot,
		"case-insensitive-volume",
		caseInsensitiveContainerRoot,
	)

	if !reflect.DeepEqual(hostCfg.CapDrop, strslice.StrSlice{"ALL"}) {
		t.Errorf("CapDrop = %v, want [ALL]", hostCfg.CapDrop)
	}
	if len(hostCfg.CapAdd) != 0 {
		t.Errorf("CapAdd = %v, want no added capabilities", hostCfg.CapAdd)
	}
	if !reflect.DeepEqual(hostCfg.SecurityOpt, []string{"no-new-privileges=true", "seccomp=builtin"}) {
		t.Errorf("SecurityOpt = %v", hostCfg.SecurityOpt)
	}
	if len(hostCfg.Mounts) != 2 {
		t.Fatalf("mount count = %d, want 2", len(hostCfg.Mounts))
	}
	if hostCfg.Mounts[0].Target != caseSensitiveContainerRoot {
		t.Errorf("case-sensitive mount target = %q", hostCfg.Mounts[0].Target)
	}
	if got := hostCfg.Mounts[0].VolumeOptions.DriverConfig.Options["o"]; !strings.HasSuffix(got, ",noperm") {
		t.Errorf("case-sensitive mount options = %q, want noperm suffix", got)
	}
	if hostCfg.Mounts[1].Target != caseInsensitiveContainerRoot {
		t.Errorf("case-insensitive mount target = %q", hostCfg.Mounts[1].Target)
	}
	if got := hostCfg.Mounts[1].VolumeOptions.DriverConfig.Options["o"]; !strings.HasPrefix(got, "nocase,") || !strings.HasSuffix(got, ",noperm") {
		t.Errorf("case-insensitive mount options = %q, want nocase prefix and noperm suffix", got)
	}
}

func Test_mountOptions(t *testing.T) {
	wd, _ := os.Getwd()
	tests := []struct {
		input            string
		expectedLocal    string
		expectedRemote   string
		expectedReadOnly bool
		expectedErr      error
	}{
		{
			input:       "",
			expectedErr: ErrInvalidVolume,
		}, {
			input:          "local:remote",
			expectedLocal:  filepath.Join(wd, "local"),
			expectedRemote: "remote",
		}, {
			input:            "local:remote:ro",
			expectedLocal:    filepath.Join(wd, "local"),
			expectedRemote:   "remote",
			expectedReadOnly: true,
		}, {
			input:            ".:remote:ro",
			expectedLocal:    wd,
			expectedRemote:   "remote",
			expectedReadOnly: true,
		}, {
			input:       "local:remote:ro:hi",
			expectedErr: ErrInvalidVolume,
		}, {
			input:       "local:remote:wo",
			expectedErr: ErrInvalidVolume,
		},
	}

	for _, test := range tests {
		local, remote, readOnly, err := mountOptions(test.input)
		if local != test.expectedLocal || remote != test.expectedRemote || readOnly != test.expectedReadOnly || err != test.expectedErr {
			t.Errorf("For input '%v' got '%v' '%v' '%v' '%v'", test.input, local, remote, readOnly, err)
		}
	}
}

func TestJobFile_ToJSON(t *testing.T) {
	t.Run("empty commit doesn't pass in empty string", func(t *testing.T) {
		job := JobFile{
			Job: &model.Job{
				Source: model.Source{
					Commit: "",
				},
			},
		}
		json, err := job.ToJSON()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(json, `"commit"`) {
			t.Errorf("expected JSON to not contain commit: %v", json)
		}
	})
}
