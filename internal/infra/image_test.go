package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dependabot/cli/internal/model"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type imageTestDaemon struct {
	cli           *client.Client
	mu            sync.Mutex
	cached        map[string]image.InspectResponse
	available     map[string]image.InspectResponse
	pulls         []string
	created       []string
	copied        int
	inspectStatus int
	pullStatus    int
	pullBody      string
}

func newImageTestDaemon(t *testing.T, cached, available map[string]image.InspectResponse) *imageTestDaemon {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "")
	d := &imageTestDaemon{
		cached:    make(map[string]image.InspectResponse),
		available: maps.Clone(available),
		pullBody:  `{"status":"downloaded"}`,
	}
	maps.Copy(d.cached, cached)
	d.cli = newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1.44/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			ref := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.44/images/"), "/json")
			if d.inspectStatus != 0 {
				w.WriteHeader(d.inspectStatus)
				writeJSON(t, w, map[string]string{"message": "inspection failed"})
			} else if inspect, ok := d.cached[ref]; ok {
				writeJSON(t, w, inspect)
			} else {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(t, w, map[string]string{"message": "image not found"})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/images/create":
			ref := r.URL.Query().Get("fromImage")
			if tag := r.URL.Query().Get("tag"); tag != "" {
				if strings.HasPrefix(tag, "sha256:") {
					ref += "@" + tag
				} else {
					ref += ":" + tag
				}
			}
			d.pulls = append(d.pulls, ref)
			if d.pullStatus != 0 {
				w.WriteHeader(d.pullStatus)
				writeJSON(t, w, map[string]string{"message": "pull failed"})
				return
			}
			if inspect, ok := d.available[ref]; ok {
				d.cached[ref] = inspect
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, d.pullBody)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.44/info":
			writeJSON(t, w, map[string]any{"SecurityOptions": []string{"name=seccomp,profile=builtin"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/containers/create":
			var cfg container.Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				t.Error(err)
			}
			d.created = append(d.created, cfg.Image)
			writeJSON(t, w, container.CreateResponse{ID: fmt.Sprintf("container-%d", len(d.created))})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/archive"):
			d.copied++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1.44/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			writeJSON(t, w, map[string]any{
				"Id": "container-1", "State": map[string]any{"Running": true, "ExitCode": 0},
				"NetworkSettings": map[string]any{"Networks": map[string]any{}},
			})
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	return d
}

func fixtureImageInspect(t *testing.T, reg *attestationTestRegistry, role string) image.InspectResponse {
	t.Helper()
	var manifest v1.Manifest
	if err := json.Unmarshal(attestationFixture(t, role+"-manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	return image.InspectResponse{ID: manifest.Config.Digest.String(), RepoDigests: []string{reg.references[role].Name()}}
}

func verifiedImageTestParams(reg *attestationTestRegistry, pull bool) RunParams {
	return RunParams{
		Job: &model.Job{
			PackageManager: "go_modules",
			Source:         model.Source{Commit: strings.Repeat("0", 40)},
		},
		ProxyImage:     reg.references["proxy"].Context().Tag("latest").Name(),
		UpdaterImage:   reg.references["updater"].Context().Tag("latest").Name(),
		StorageImage:   "unsigned-storage",
		CollectorImage: "unsigned-collector",
		PullImages:     pull,
	}
}

func TestVerifyImagesPinsPullAndContainerReferences(t *testing.T) {
	reg := newAttestationTestRegistry(t, officialImageBundles(t), false)
	available := make(map[string]image.InspectResponse)
	for _, role := range []string{"proxy", "updater"} {
		available[reg.references[role].Name()] = fixtureImageInspect(t, reg, role)
	}
	d := newImageTestDaemon(t, nil, available)
	params := verifiedImageTestParams(reg, true)
	factories := 0
	err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
		factories++
		return testImageVerifier(t), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.manifests["/v2/proxy/manifests/latest"] = attestationFixture(t, "updater-manifest.json")
	reg.manifests["/v2/updater/manifests/latest"] = attestationFixture(t, "proxy-manifest.json")
	reg.mu.Unlock()
	for _, ref := range []string{params.ProxyImage, params.UpdaterImage} {
		if err := pullVerifiedImage(t.Context(), d.cli, ref); err != nil {
			t.Fatal(err)
		}
	}
	proxy, err := NewProxy(t.Context(), d.cli, &params, nil)
	if err != nil {
		t.Fatal(err)
	}
	nets := &Networks{NoInternet: network.CreateResponse{ID: "no-internet"}, noInternetName: "no-internet"}
	if _, err := NewUpdater(t.Context(), d.cli, nets, &params, proxy, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{reg.references["proxy"].Name(), reg.references["updater"].Name()}
	d.mu.Lock()
	pulls, created := append([]string(nil), d.pulls...), append([]string(nil), d.created...)
	d.mu.Unlock()
	if !slices.Equal(pulls, want) || !slices.Equal(created, want) {
		t.Fatalf("pulls = %v, creates = %v, want %v", pulls, created, want)
	}
	if factories != 1 || params.StorageImage != "unsigned-storage" || params.CollectorImage != "unsigned-collector" {
		t.Fatalf("unexpected preparation state: factories=%d, params=%+v", factories, params)
	}
	reg.mu.Lock()
	requests := append([]string(nil), reg.requests...)
	reg.mu.Unlock()
	for _, role := range []string{"proxy", "updater"} {
		tagRequests := 0
		for _, request := range requests {
			if request == "GET /v2/"+role+"/manifests/latest" {
				tagRequests++
			}
		}
		if tagRequests != 1 {
			t.Fatalf("%s tag resolved %d times, want 1", role, tagRequests)
		}
	}
}

func TestVerifyImagesUsesCachedDigestWithoutPull(t *testing.T) {
	reg := newAttestationTestRegistry(t, officialImageBundles(t), false)
	params := verifiedImageTestParams(reg, false)
	cached := make(map[string]image.InspectResponse)
	for _, role := range []string{"proxy", "updater"} {
		inspect := fixtureImageInspect(t, reg, role)
		cached[reg.references[role].Name()] = inspect
		cached[reg.references[role].Context().Tag("latest").Name()] = inspect
	}
	reg.mu.Lock()
	reg.manifests["/v2/proxy/manifests/latest"] = attestationFixture(t, "updater-manifest.json")
	reg.manifests["/v2/updater/manifests/latest"] = attestationFixture(t, "proxy-manifest.json")
	reg.mu.Unlock()
	d := newImageTestDaemon(t, cached, nil)
	if err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
		return testImageVerifier(t), nil
	}); err != nil {
		t.Fatal(err)
	}
	if params.ProxyImage != reg.references["proxy"].Name() || params.UpdaterImage != reg.references["updater"].Name() {
		t.Fatalf("cached image selections changed: %+v", params)
	}
	d.mu.Lock()
	pulls := len(d.pulls)
	d.mu.Unlock()
	if pulls != 0 {
		t.Fatalf("pulled %d images with pulling disabled", pulls)
	}
	reg.mu.Lock()
	requests := append([]string(nil), reg.requests...)
	reg.mu.Unlock()
	for _, request := range requests {
		if strings.HasSuffix(request, "/manifests/latest") {
			t.Fatalf("resolved mutable registry tag with pulling disabled: %s", request)
		}
	}
}

func TestVerifyImagesRejectsInconsistentCachedImage(t *testing.T) {
	reg := newAttestationTestRegistry(t, officialImageBundles(t), false)
	params := verifiedImageTestParams(reg, false)
	original := params
	cached := make(map[string]image.InspectResponse)
	for _, role := range []string{"proxy", "updater"} {
		inspect := fixtureImageInspect(t, reg, role)
		cached[reg.references[role].Context().Tag("latest").Name()] = inspect
		if role == "updater" {
			inspect.ID = "different-image"
		}
		cached[reg.references[role].Name()] = inspect
	}
	d := newImageTestDaemon(t, cached, nil)
	err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
		return testImageVerifier(t), nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the selected cached image") {
		t.Fatalf("verifyImages() = %v, want cached identity error", err)
	}
	if params.ProxyImage != original.ProxyImage || params.UpdaterImage != original.UpdaterImage {
		t.Fatal("failed verification replaced image references")
	}
}

func TestVerifyImagesPropagatesTrustFailure(t *testing.T) {
	reg := newAttestationTestRegistry(t, officialImageBundles(t), false)
	params := verifiedImageTestParams(reg, true)
	d := newImageTestDaemon(t, nil, nil)
	trustErr := errors.New("trusted material unavailable")
	err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
		return nil, trustErr
	})
	if !errors.Is(err, trustErr) {
		t.Fatalf("verifyImages() = %v, want trust failure", err)
	}
}

func TestCachedImageReference(t *testing.T) {
	a := "sha256:" + strings.Repeat("a", 64)
	b := "sha256:" + strings.Repeat("b", 64)
	for _, tt := range []struct {
		name    string
		ref     string
		digests []string
		want    string
	}{
		{"missing", "ghcr.io/dependabot/proxy", nil, ""},
		{"wrong repository", "ghcr.io/dependabot/proxy", []string{"ghcr.io/dependabot/proxy-other@" + a}, ""},
		{"ambiguous", "ghcr.io/dependabot/proxy", []string{"ghcr.io/dependabot/proxy@" + a, "ghcr.io/dependabot/proxy@" + b}, ""},
		{"pinned", "ghcr.io/dependabot/proxy@" + b, []string{"ghcr.io/dependabot/proxy@" + a, "ghcr.io/dependabot/proxy@" + b}, "ghcr.io/dependabot/proxy@" + b},
		{"normalized", "ubuntu", []string{"docker.io/library/ubuntu@" + a}, "index.docker.io/library/ubuntu@" + a},
		{"duplicate", "ghcr.io/dependabot/proxy", []string{"ghcr.io/dependabot/proxy@" + a, "ghcr.io/dependabot/proxy@" + a}, "ghcr.io/dependabot/proxy@" + a},
		{"configuration ID", "ghcr.io/dependabot/proxy", []string{a}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := name.ParseReference(tt.ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := cachedImageReference(ref, tt.digests)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("accepted invalid cached identity: %s", got)
				}
			} else if err != nil || got.Name() != tt.want {
				t.Fatalf("cachedImageReference() = %s, %v, want %s", got, err, tt.want)
			}
		})
	}
}

func TestSelectImageKeepsIndexDigest(t *testing.T) {
	reg := newAttestationTestRegistry(t, officialImageBundles(t), false)
	digest, err := v1.NewHash(reg.references["updater"].DigestStr())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v1.IndexManifest{
		SchemaVersion: 2, MediaType: types.OCIImageIndex,
		Manifests: []v1.Descriptor{{
			MediaType: types.OCIManifestSchema1, Digest: digest,
			Platform: &v1.Platform{OS: "linux", Architecture: "amd64"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.manifests["/v2/updater/manifests/latest"] = data
	reg.mu.Unlock()
	d := newImageTestDaemon(t, nil, nil)
	selected, err := selectImage(t.Context(), d.cli, reg.references["updater"].Context().Tag("latest").Name(), true)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := v1.SHA256(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if selected.reference.DigestStr() != want.String() || selected.reference == reg.references["updater"] {
		t.Fatalf("selected %s, want index digest %s", selected.reference, want)
	}
}

func TestPullVerifiedImage(t *testing.T) {
	ref, err := name.NewDigest("ghcr.io/dependabot/proxy@sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	inspect := image.InspectResponse{ID: "image-id", RepoDigests: []string{ref.Name()}}
	for _, tt := range []struct {
		name          string
		cached        bool
		available     bool
		inspectStatus int
		pullStatus    int
		body          string
		wantErr       string
		wantPulls     int
	}{
		{name: "download", available: true, wantPulls: 1},
		{name: "warm cache", cached: true},
		{name: "stream error", available: true, body: `{"errorDetail":{"message":"denied"},"error":"denied"}`, wantErr: "denied", wantPulls: 1},
		{name: "malformed stream", available: true, body: "not JSON", wantErr: "invalid character", wantPulls: 1},
		{name: "pull error", pullStatus: http.StatusInternalServerError, wantErr: "pull failed", wantPulls: 1},
		{name: "post-pull missing", wantErr: "after pull", wantPulls: 1},
		{name: "inspection error", inspectStatus: http.StatusInternalServerError, wantErr: "inspection failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cached, available := make(map[string]image.InspectResponse), make(map[string]image.InspectResponse)
			if tt.cached {
				cached[ref.Name()] = inspect
			}
			if tt.available {
				available[ref.Name()] = inspect
			}
			d := newImageTestDaemon(t, cached, available)
			d.inspectStatus, d.pullStatus = tt.inspectStatus, tt.pullStatus
			if tt.body != "" {
				d.pullBody = tt.body
			}
			err := pullVerifiedImage(t.Context(), d.cli, ref.Name())
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("pullVerifiedImage() = %v, want %q", err, tt.wantErr)
			}
			d.mu.Lock()
			pulls := append([]string(nil), d.pulls...)
			d.mu.Unlock()
			if len(pulls) != tt.wantPulls {
				t.Fatalf("pulls = %v, want %d", pulls, tt.wantPulls)
			}
			for _, pulled := range pulls {
				if pulled != ref.Name() {
					t.Fatalf("pulled %s, want exact digest %s", pulled, ref)
				}
			}
		})
	}
}

func TestImageRegistryAuthScopesEnvironmentCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("LOCAL_GITHUB_ACCESS_TOKEN", "test-github-token")
	t.Setenv("AZURE_REGISTRY_USERNAME", "test-registry-user")
	t.Setenv("AZURE_REGISTRY_PASSWORD", "test-registry-password")
	for _, tt := range []struct {
		host     string
		username string
		password string
	}{
		{"ghcr.io", "x-access-token", "test-github-token"},
		{"registry.azurecr.io", "test-registry-user", "test-registry-password"},
		{"ghcr.io.untrusted.invalid", "", ""},
		{"registry.azurecr.io.untrusted.invalid", "", ""},
	} {
		t.Run(tt.host, func(t *testing.T) {
			ref, err := name.ParseReference(tt.host + "/owner/image")
			if err != nil {
				t.Fatal(err)
			}
			auth, err := imageRegistryAuth(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			config, err := authn.Authorization(t.Context(), auth)
			if err != nil {
				t.Fatal(err)
			}
			if config.Username != tt.username || config.Password != tt.password {
				t.Fatal("registry environment credentials were not scoped to the expected host")
			}
		})
	}
}
