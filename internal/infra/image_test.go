package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
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
	tagStatus     int
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
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1.44/images/") && strings.HasSuffix(r.URL.Path, "/tag"):
			source := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.44/images/"), "/tag")
			if _, err := name.NewDigest(source); err != nil {
				t.Errorf("tag source is not an immutable reference: %s", source)
			}
			if d.tagStatus != 0 {
				w.WriteHeader(d.tagStatus)
				writeJSON(t, w, map[string]string{"message": "tag failed"})
				return
			}
			inspect, ok := d.cached[source]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(t, w, map[string]string{"message": "tag source not found"})
				return
			}
			d.cached[r.URL.Query().Get("repo")+":"+r.URL.Query().Get("tag")] = inspect
			w.WriteHeader(http.StatusCreated)
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
				"NetworkSettings": map[string]any{"Networks": map[string]any{
					"no-internet": map[string]string{"IPAddress": "192.0.2.2"},
				}},
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
	id := manifest.Config.Digest.String()
	if manifest.MediaType == types.DockerManifestList || manifest.MediaType == types.OCIImageIndex {
		id = "local-" + role
	}
	return image.InspectResponse{ID: id, RepoDigests: []string{reg.references[role].Name()}}
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
	requested := params
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
	if err := pullVerifiedImage(t.Context(), d.cli, params.ProxyImage, requested.ProxyImage); err != nil {
		t.Fatal(err)
	}
	if err := pullVerifiedImage(t.Context(), d.cli, params.UpdaterImage, requested.UpdaterImage); err != nil {
		t.Fatal(err)
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

func TestVerifyImagesCollector(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprintf("configured=%v", configured), func(t *testing.T) {
			bundles := officialImageBundles(t)
			bundles["collector"] = [][]byte{attestationFixture(t, "collector.json")}
			reg := newAttestationTestRegistry(t, bundles, false)
			cached := make(map[string]image.InspectResponse)
			for _, role := range []string{"proxy", "updater"} {
				cached[reg.references[role].Name()] = fixtureImageInspect(t, reg, role)
			}
			ref := reg.references["collector"]
			d := newImageTestDaemon(t, cached, map[string]image.InspectResponse{ref.Name(): fixtureImageInspect(t, reg, "collector")})
			params := verifiedImageTestParams(reg, true)
			params.CollectorImage = ref.Context().Tag("latest").Name()
			if configured {
				params.CollectorConfigPath = filepath.Join(t.TempDir(), "collector.yaml")
			}
			requested := params.CollectorImage
			if err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
				return testImageVerifier(t), nil
			}); err != nil {
				t.Fatal(err)
			}
			if !configured {
				if params.CollectorImage != requested {
					t.Fatal("replaced the disabled Collector image")
				}
				reg.mu.Lock()
				requests := slices.Clone(reg.requests)
				reg.mu.Unlock()
				for _, request := range requests {
					if strings.Contains(request, "/collector/") {
						t.Fatalf("requested disabled Collector metadata: %s", request)
					}
				}
				return
			}
			if params.CollectorImage != ref.Name() {
				t.Fatalf("Collector image = %s, want verified digest %s", params.CollectorImage, ref)
			}
			reg.mu.Lock()
			reg.manifests["/v2/collector/manifests/latest"] = attestationFixture(t, "updater-manifest.json")
			reg.mu.Unlock()
			if err := pullVerifiedImage(t.Context(), d.cli, params.CollectorImage, requested); err != nil {
				t.Fatal(err)
			}
			proxy, err := NewProxy(t.Context(), d.cli, &params, nil)
			if err != nil {
				t.Fatal(err)
			}
			nets := &Networks{NoInternet: network.CreateResponse{ID: "no-internet"}, noInternetName: "no-internet"}
			if _, err := NewCollector(t.Context(), d.cli, nets, &params, proxy); err != nil {
				t.Fatal(err)
			}
			d.mu.Lock()
			pulls, created := slices.Clone(d.pulls), slices.Clone(d.created)
			d.mu.Unlock()
			if !slices.Equal(pulls, []string{ref.Name()}) || !slices.Equal(created, []string{params.ProxyImage, ref.Name()}) {
				t.Fatalf("pulls = %v, creates = %v, want the verified Collector index", pulls, created)
			}
		})
	}
}

func TestVerifyImagesCollectorVersion(t *testing.T) {
	for _, tt := range []struct {
		tag     string
		wantErr string
		digest  bool
	}{
		{tag: "latest"},
		{tag: "stable"},
		{tag: "0.161.0"},
		{tag: "v0.161.0"},
		{tag: "digest"},
		{tag: "0.160.0", wantErr: "refs/tags/v0.160.0"},
		{tag: "0.161.0-rc.1", wantErr: "stable release version"},
		{tag: "00.161.0", wantErr: "stable release version"},
		{tag: "0.161.0", digest: true},
		{tag: "0.160.0", wantErr: "refs/tags/v0.160.0", digest: true},
		{tag: "0.161.0-rc.1", wantErr: "stable release version", digest: true},
	} {
		testName := tt.tag
		if tt.digest {
			testName += "@digest"
		}
		t.Run(testName, func(t *testing.T) {
			bundles := officialImageBundles(t)
			bundles["collector"] = [][]byte{attestationFixture(t, "collector.json")}
			reg := newAttestationTestRegistry(t, bundles, false)
			ref := reg.references["collector"]
			params := verifiedImageTestParams(reg, true)
			params.CollectorConfigPath = filepath.Join(t.TempDir(), "collector.yaml")
			params.CollectorImage = ref.Context().Tag(tt.tag).Name()
			if tt.tag == "digest" {
				params.CollectorImage = ref.Name()
			} else if tt.digest {
				params.CollectorImage += "@" + ref.DigestStr()
			}
			reg.mu.Lock()
			reg.manifests["/v2/collector/manifests/"+tt.tag] = attestationFixture(t, "collector-manifest.json")
			reg.mu.Unlock()
			original := params
			d := newImageTestDaemon(t, nil, nil)
			err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
				return testImageVerifier(t), nil
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("verifyImages() = %v, want %q", err, tt.wantErr)
				}
				if params.ProxyImage != original.ProxyImage || params.UpdaterImage != original.UpdaterImage || params.CollectorImage != original.CollectorImage {
					t.Fatal("failed Collector verification replaced protected image references")
				}
			} else if err != nil || params.CollectorImage != ref.Name() {
				t.Fatalf("Collector selection = %s, %v, want %s", params.CollectorImage, err, ref)
			}
		})
	}
}

func TestVerifyImagesUsesCachedDigestWithoutPull(t *testing.T) {
	bundles := officialImageBundles(t)
	bundles["collector"] = [][]byte{attestationFixture(t, "collector.json")}
	reg := newAttestationTestRegistry(t, bundles, false)
	params := verifiedImageTestParams(reg, false)
	params.CollectorConfigPath = filepath.Join(t.TempDir(), "collector.yaml")
	params.CollectorImage = reg.references["collector"].Context().Tag("latest").Name()
	cached := make(map[string]image.InspectResponse)
	for _, role := range []string{"proxy", "updater", "collector"} {
		inspect := fixtureImageInspect(t, reg, role)
		cached[reg.references[role].Name()] = inspect
		cached[reg.references[role].Context().Tag("latest").Name()] = inspect
	}
	reg.mu.Lock()
	reg.manifests["/v2/proxy/manifests/latest"] = attestationFixture(t, "updater-manifest.json")
	reg.manifests["/v2/updater/manifests/latest"] = attestationFixture(t, "proxy-manifest.json")
	reg.manifests["/v2/collector/manifests/latest"] = attestationFixture(t, "proxy-manifest.json")
	reg.mu.Unlock()
	d := newImageTestDaemon(t, cached, nil)
	if err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
		return testImageVerifier(t), nil
	}); err != nil {
		t.Fatal(err)
	}
	if params.ProxyImage != reg.references["proxy"].Name() || params.UpdaterImage != reg.references["updater"].Name() ||
		params.CollectorImage != reg.references["collector"].Name() {
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
	for _, mismatchedRole := range []string{"updater", "collector"} {
		t.Run(mismatchedRole, func(t *testing.T) {
			bundles := officialImageBundles(t)
			bundles["collector"] = [][]byte{attestationFixture(t, "collector.json")}
			reg := newAttestationTestRegistry(t, bundles, false)
			params := verifiedImageTestParams(reg, false)
			params.CollectorConfigPath = filepath.Join(t.TempDir(), "collector.yaml")
			params.CollectorImage = reg.references["collector"].Context().Tag("latest").Name()
			original := params
			cached := make(map[string]image.InspectResponse)
			for _, role := range []string{"proxy", "updater", "collector"} {
				inspect := fixtureImageInspect(t, reg, role)
				cached[reg.references[role].Context().Tag("latest").Name()] = inspect
				if role == mismatchedRole {
					inspect.ID = "different-image"
				}
				cached[reg.references[role].Name()] = inspect
			}
			d := newImageTestDaemon(t, cached, nil)
			err := verifyImages(t.Context(), d.cli, &params, func(context.Context) (*imageVerifier, error) {
				return testImageVerifier(t), nil
			})
			if err == nil || !strings.Contains(err.Error(), mismatchedRole+" image") || !strings.Contains(err.Error(), "does not match the selected cached image") {
				t.Fatalf("verifyImages() = %v, want cached identity error", err)
			}
			if params.ProxyImage != original.ProxyImage || params.UpdaterImage != original.UpdaterImage || params.CollectorImage != original.CollectorImage {
				t.Fatal("failed verification replaced image references")
			}
		})
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
			err := pullVerifiedImage(t.Context(), d.cli, ref.Name(), ref.Name())
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

func TestPullVerifiedImageRefreshesRequestedTag(t *testing.T) {
	ref, err := name.NewDigest("ghcr.io/dependabot/proxy@sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	requested := ref.Context().Tag("latest")
	verified := image.InspectResponse{ID: "verified-image", RepoDigests: []string{ref.Name()}}
	for _, tt := range []struct {
		name      string
		staleTag  bool
		cached    bool
		tagStatus int
	}{
		{name: "missing tag"},
		{name: "stale tag", staleTag: true},
		{name: "cached digest", staleTag: true, cached: true},
		{name: "tag failure", tagStatus: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cached := make(map[string]image.InspectResponse)
			if tt.staleTag {
				cached[requested.Name()] = image.InspectResponse{
					ID: "older-image", RepoDigests: []string{ref.Context().Digest("sha256:" + strings.Repeat("b", 64)).Name()},
				}
			}
			if tt.cached {
				cached[ref.Name()] = verified
			}
			d := newImageTestDaemon(t, cached, map[string]image.InspectResponse{ref.Name(): verified})
			d.tagStatus = tt.tagStatus
			err := pullVerifiedImage(t.Context(), d.cli, ref.Name(), requested.Name())
			if tt.tagStatus != 0 {
				if err == nil || !strings.Contains(err.Error(), "tag failed") {
					t.Fatalf("pullVerifiedImage() = %v, want tag error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			selected, err := selectImage(t.Context(), d.cli, requested.Name(), false)
			if err != nil || selected.reference != ref || selected.localID != verified.ID {
				t.Fatalf("subsequent --pull=false selected %s (%s), %v, want %s (%s)",
					selected.reference, selected.localID, err, ref, verified.ID)
			}
			d.mu.Lock()
			pulls := append([]string(nil), d.pulls...)
			d.mu.Unlock()
			wantPulls := 1
			if tt.cached {
				wantPulls = 0
			}
			if len(pulls) != wantPulls {
				t.Fatalf("pulls = %v, want %d", pulls, wantPulls)
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
