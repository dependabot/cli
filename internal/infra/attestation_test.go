package infra

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

func attestationFixture(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "attestations", filename))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testImageVerifier(t *testing.T) *imageVerifier {
	t.Helper()
	trusted, err := sigroot.NewTrustedRootFromJSON(attestationFixture(t, "trusted-root.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &imageVerifier{trustedMaterial: trusted}
}

func officialImageBundles(t *testing.T) map[string][][]byte {
	t.Helper()
	return map[string][][]byte{
		"updater": {attestationFixture(t, "updater.json")},
		"proxy":   {attestationFixture(t, "proxy.json")},
	}
}

type attestationTestRegistry struct {
	server     *httptest.Server
	references map[string]name.Digest
	mu         sync.Mutex
	manifests  map[string][]byte
	indexes    map[string][]byte
	requests   []string
	fallback   bool
}

func newAttestationTestRegistry(t *testing.T, bundles map[string][][]byte, fallback bool) *attestationTestRegistry {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	r := &attestationTestRegistry{
		references: make(map[string]name.Digest),
		manifests:  make(map[string][]byte),
		indexes:    make(map[string][]byte),
		fallback:   fallback,
	}
	backing := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.requests = append(r.requests, req.Method+" "+req.URL.Path)
		data, found := r.manifests[req.URL.Path]
		parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/v2/"), "/")
		var index []byte
		if len(parts) == 3 && parts[1] == "referrers" {
			index = r.indexes[parts[0]]
		}
		r.mu.Unlock()
		if (req.Method == http.MethodGet || req.Method == http.MethodHead) && found {
			writeRegistryFixture(t, w, req, data)
			return
		}
		if len(parts) == 3 && parts[1] == "referrers" {
			if fallback || index == nil {
				http.NotFound(w, req)
			} else {
				writeRegistryFixture(t, w, req, index)
			}
			return
		}
		backing.ServeHTTP(w, req)
	}))
	t.Cleanup(r.server.Close)
	for _, role := range []string{"updater", "proxy"} {
		repo, err := name.NewRepository(strings.TrimPrefix(r.server.URL, "http://") + "/" + role)
		if err != nil {
			t.Fatal(err)
		}
		manifest := attestationFixture(t, role+"-manifest.json")
		hash := fmt.Sprintf("sha256:%x", sha256.Sum256(manifest))
		r.references[role] = repo.Digest(hash)
		r.mu.Lock()
		r.manifests["/v2/"+role+"/manifests/latest"] = manifest
		r.manifests["/v2/"+role+"/manifests/"+hash] = manifest
		r.mu.Unlock()
		var index v1.ImageIndex = mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
		for _, data := range bundles[role] {
			image, err := mutate.AppendLayers(empty.Image, static.NewLayer(data, types.MediaType(sigstoreBundleType+".v0.3+json")))
			if err != nil {
				t.Fatal(err)
			}
			image = mutate.MediaType(image, types.OCIManifestSchema1)
			index = mutate.AppendManifests(index, mutate.IndexAddendum{
				Add:        image,
				Descriptor: v1.Descriptor{ArtifactType: sigstoreBundleType + ".v0.3+json"},
			})
		}
		data, err := index.RawManifest()
		if err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		r.indexes[role] = data
		r.mu.Unlock()
		if err := remote.WriteIndex(repo.Tag(strings.Replace(hash, ":", "-", 1)), index); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	r.requests = nil
	r.mu.Unlock()
	return r
}

func writeRegistryFixture(t *testing.T, w http.ResponseWriter, req *http.Request, data []byte) {
	t.Helper()
	var header struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		t.Error(err)
		http.Error(w, "invalid fixture", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", header.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Docker-Content-Digest", fmt.Sprintf("sha256:%x", sha256.Sum256(data)))
	if req.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func TestImageVerifier(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback=%v", fallback), func(t *testing.T) {
			reg := newAttestationTestRegistry(t, officialImageBundles(t), fallback)
			verifier := testImageVerifier(t)
			for _, tt := range []struct {
				role   imageRole
				commit string
			}{
				{updaterImageRole, "d98dcb4010f598cfd59ba80b2e8dd6c9d63b750b"},
				{proxyImageRole, "a2f35c37ddd9a1811265ff641225f5dd5d50605e"},
			} {
				t.Run(tt.role.name, func(t *testing.T) {
					ref := reg.references[tt.role.name]
					got, err := verifier.Verify(t.Context(), ref, tt.role)
					if err != nil {
						t.Fatal(err)
					}
					if got.reference != ref || got.producerCommit != tt.commit {
						t.Fatalf("Verify() = %+v, want %s at %s", got, ref, tt.commit)
					}
				})
			}
		})
	}
}

func TestImageVerifierRejectsUnexpectedProvenance(t *testing.T) {
	reg := newAttestationTestRegistry(t, officialImageBundles(t), false)
	verifier := testImageVerifier(t)
	for _, field := range []string{"issuer", "repository", "workflow", "source ref", "subject digest"} {
		t.Run(field, func(t *testing.T) {
			role := updaterImageRole
			ref := reg.references["updater"]
			switch field {
			case "issuer":
				role.issuer = "https://unexpected.example"
			case "repository":
				role.repository = "https://github.com/unexpected/repository"
			case "workflow":
				role.workflow = "https://github.com/dependabot/dependabot-core/.github/workflows/other.yml@refs/heads/main"
			case "source ref":
				role.sourceRef = "refs/heads/other"
			case "subject digest":
				ref = ref.Context().Digest("sha256:" + strings.Repeat("a", 64))
			}
			if _, err := verifier.Verify(t.Context(), ref, role); err == nil {
				t.Fatalf("accepted unexpected %s", field)
			}
		})
	}
}

func TestImageVerifierRejectsInvalidBundles(t *testing.T) {
	tampered := &bundle.Bundle{}
	if err := tampered.UnmarshalJSON(attestationFixture(t, "updater.json")); err != nil {
		t.Fatal(err)
	}
	tampered.GetDsseEnvelope().Signatures[0].Sig[0] ^= 1
	tamperedBytes, err := tampered.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		bundles [][]byte
	}{
		{"missing", nil},
		{"malformed", [][]byte{[]byte(`{"invalid":true}`)}},
		{"tampered", [][]byte{tamperedBytes}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reg := newAttestationTestRegistry(t, map[string][][]byte{"updater": tt.bundles}, false)
			if _, err := testImageVerifier(t).Verify(t.Context(), reg.references["updater"], updaterImageRole); err == nil {
				t.Fatalf("accepted %s bundle", tt.name)
			}
		})
	}
}

func TestImageVerifierAcceptsMatchingCandidate(t *testing.T) {
	reg := newAttestationTestRegistry(t, map[string][][]byte{
		"updater": {[]byte(`{"invalid":true}`), attestationFixture(t, "updater.json")},
	}, false)
	var index v1.IndexManifest
	if err := json.Unmarshal(reg.indexes["updater"], &index); err != nil {
		t.Fatal(err)
	}
	unrelated := v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("e", 64)}
	index.Manifests = append([]v1.Descriptor{{
		MediaType: types.OCIManifestSchema1, Digest: unrelated, ArtifactType: "application/spdx+json",
	}}, index.Manifests...)
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.indexes["updater"] = data
	reg.mu.Unlock()
	if _, err := testImageVerifier(t).Verify(t.Context(), reg.references["updater"], updaterImageRole); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	requests := append([]string(nil), reg.requests...)
	reg.mu.Unlock()
	for _, request := range requests {
		if strings.Contains(request, unrelated.String()) {
			t.Fatalf("fetched unrelated referrer: %s", request)
		}
	}
}

func TestVerifiedProducerCommit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		predicate string
		commit    string
	}{
		{"wrong predicate", "https://sigstore.dev/cosign/sign/v1", "producer"},
		{"missing commit", provenancePredicate, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := fmt.Sprintf(`{"statement":{"predicateType":%q},"signature":{"certificate":{"sourceRepositoryDigest":%q}}}`, tt.predicate, tt.commit)
			var result verify.VerificationResult
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				t.Fatal(err)
			}
			if _, err := verifiedProducerCommit(&result); err == nil {
				t.Fatalf("accepted %s", tt.name)
			}
		})
	}
}

func TestImageVerifierCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	started := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ref, err := name.NewDigest(strings.TrimPrefix(server.URL, "http://") + "/updater@sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	verifier := testImageVerifier(t)
	go func() {
		_, err := verifier.Verify(ctx, ref, updaterImageRole)
		done <- err
	}()
	select {
	case <-started:
		cancel()
	case err := <-done:
		t.Fatalf("request ended before cancellation: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("registry request did not start")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Verify() = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("verification ignored cancellation")
	}
}

func TestReadImageBundleLimit(t *testing.T) {
	if _, err := readImageBundle(io.NopCloser(bytes.NewReader(bytes.Repeat([]byte(" "), maxBundleBytes+1)))); err == nil {
		t.Fatal("accepted oversized bundle")
	}
}
