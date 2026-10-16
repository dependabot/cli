package infra

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

const (
	actionsIssuer               = "https://token.actions.githubusercontent.com"
	provenancePredicate         = "https://slsa.dev/provenance/v1"
	imageSignaturePredicate     = "https://sigstore.dev/cosign/sign/v1"
	stableReleaseVersionPattern = `(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)`
	stableReleaseRefPattern     = `refs/tags/v` + stableReleaseVersionPattern
	sigstoreBundleType          = "application/vnd.dev.sigstore.bundle"
	maxBundleBytes              = 16 << 20
)

type imageRole struct {
	name            string
	repository      string
	workflow        string
	issuer          string
	sourceRef       string
	predicate       string
	releaseWorkflow bool
}

var (
	updaterImageRole = imageRole{
		name:       "updater",
		repository: "https://github.com/dependabot/dependabot-core",
		workflow:   "https://github.com/dependabot/dependabot-core/.github/workflows/images-latest.yml@refs/heads/main",
		issuer:     actionsIssuer,
		sourceRef:  "refs/heads/main",
		predicate:  provenancePredicate,
	}
	proxyImageRole = imageRole{
		name:       "proxy",
		repository: "https://github.com/dependabot/proxy",
		workflow:   "https://github.com/dependabot/proxy/.github/workflows/ghcr.yml@refs/heads/main",
		issuer:     actionsIssuer,
		sourceRef:  "refs/heads/main",
		predicate:  provenancePredicate,
	}
	collectorImageRole = imageRole{
		name:            "collector",
		repository:      "https://github.com/open-telemetry/opentelemetry-collector-releases",
		workflow:        "https://github.com/open-telemetry/opentelemetry-collector-releases/.github/workflows/base-release.yaml",
		issuer:          actionsIssuer,
		predicate:       imageSignaturePredicate,
		releaseWorkflow: true,
	}
	stableReleaseRefRegex   = regexp.MustCompile("^" + stableReleaseRefPattern + "$")
	stableImageVersionRegex = regexp.MustCompile("^v?" + stableReleaseVersionPattern + "$")
	versionedImageTagRegex  = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+($|-)`)
)

func collectorRoleForImage(imageName string) (imageRole, error) {
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return imageRole{}, err
	}
	if _, ok := ref.(name.Digest); ok {
		// Digest parsing drops an optional tag that still constrains the signing release.
		tagName, _, _ := strings.Cut(imageName, "@")
		ref, err = name.NewTag(tagName)
		if err != nil {
			return imageRole{}, err
		}
	}
	role := collectorImageRole
	if tag, ok := ref.(name.Tag); ok {
		version := tag.TagStr()
		if stableImageVersionRegex.MatchString(version) {
			role.sourceRef = "refs/tags/v" + strings.TrimPrefix(version, "v")
		} else if versionedImageTagRegex.MatchString(version) {
			return imageRole{}, fmt.Errorf("image tag %q must be a stable release version", version)
		}
	}
	return role, nil
}

type verifiedImage struct {
	reference      name.Digest
	producerCommit string
}

type imageVerifier struct {
	trustedMaterial sigroot.TrustedMaterial
}

func newImageVerifier(ctx context.Context) (*imageVerifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := fetcher.NewDefaultFetcher()
	f.SetHTTPClient(contextHTTPClient{ctx: ctx, client: http.DefaultClient})
	f.SetHTTPUserAgent("dependabot-cli")
	trusted, err := sigroot.FetchTrustedRootWithOptions(tuf.DefaultOptions().WithContext(ctx).WithFetcher(f))
	if err != nil {
		return nil, fmt.Errorf("failed to load Sigstore trusted material: %w", err)
	}
	return &imageVerifier{trustedMaterial: trusted}, nil
}

type contextHTTPClient struct {
	ctx    context.Context
	client *http.Client
}

func (c contextHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req.WithContext(c.ctx))
}

func (v *imageVerifier) Verify(ctx context.Context, ref name.Digest, role imageRole) (verified verifiedImage, err error) {
	workflow := role.workflow
	if role.releaseWorkflow && role.sourceRef != "" {
		workflow += "@" + role.sourceRef
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s image %s: attestation verification failed (expected %s): %w", role.name, ref, workflow, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return verifiedImage{}, err
	}
	sanValue, sanPattern := workflow, ""
	if role.releaseWorkflow && role.sourceRef == "" {
		sanValue = ""
		sanPattern = "^" + regexp.QuoteMeta(workflow) + "@" + stableReleaseRefPattern + "$"
	}
	san, err := verify.NewSANMatcher(sanValue, sanPattern)
	if err != nil {
		return verifiedImage{}, err
	}
	issuer, err := verify.NewIssuerMatcher(role.issuer, "")
	if err != nil {
		return verifiedImage{}, err
	}
	identity, err := verify.NewCertificateIdentity(san, issuer, certificate.Extensions{
		SourceRepositoryURI: role.repository,
		SourceRepositoryRef: role.sourceRef,
	})
	if err != nil {
		return verifiedImage{}, err
	}
	verifier, err := verify.NewVerifier(v.trustedMaterial,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return verifiedImage{}, err
	}
	hash, err := v1.NewHash(ref.DigestStr())
	if err != nil {
		return verifiedImage{}, fmt.Errorf("invalid image digest: %w", err)
	}
	digest, err := hex.DecodeString(hash.Hex)
	if err != nil {
		return verifiedImage{}, fmt.Errorf("invalid image digest: %w", err)
	}
	policy := verify.NewPolicy(verify.WithArtifactDigest(hash.Algorithm, digest), verify.WithCertificateIdentity(identity))
	options, err := imageRemoteOptions(ctx, ref)
	if err != nil {
		return verifiedImage{}, err
	}
	referrers, err := remote.Referrers(ref, options...)
	if err != nil {
		return verifiedImage{}, fmt.Errorf("failed to fetch OCI referrers: %w", err)
	}
	index, err := referrers.IndexManifest()
	if err != nil {
		return verifiedImage{}, fmt.Errorf("failed to read OCI referrers: %w", err)
	}
	var failures []error
	for _, descriptor := range index.Manifests {
		if !strings.HasPrefix(descriptor.ArtifactType, sigstoreBundleType) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return verifiedImage{}, err
		}
		bundleRef := ref.Context().Digest(descriptor.Digest.String())
		image, err := remote.Image(bundleRef, options...)
		if err != nil {
			failures = append(failures, fmt.Errorf("failed to fetch bundle %s: %w", bundleRef, err))
			continue
		}
		layers, err := image.Layers()
		if err != nil {
			failures = append(failures, fmt.Errorf("failed to read bundle %s: %w", bundleRef, err))
			continue
		}
		if len(layers) == 0 {
			failures = append(failures, fmt.Errorf("bundle %s has no layers", bundleRef))
		}
		for _, layer := range layers {
			reader, err := layer.Uncompressed()
			if err != nil {
				failures = append(failures, fmt.Errorf("failed to open bundle %s: %w", bundleRef, err))
				continue
			}
			b, err := readImageBundle(reader)
			if err != nil {
				failures = append(failures, fmt.Errorf("failed to parse bundle %s: %w", bundleRef, err))
				continue
			}
			result, err := verifier.Verify(b, policy)
			if err != nil {
				failures = append(failures, fmt.Errorf("bundle %s: %w", bundleRef, err))
				continue
			}
			commit, err := verifiedProducerCommit(result, role)
			if err != nil {
				failures = append(failures, fmt.Errorf("bundle %s: %w", bundleRef, err))
				continue
			}
			if err := ctx.Err(); err != nil {
				return verifiedImage{}, err
			}
			return verifiedImage{reference: ref, producerCommit: commit}, nil
		}
	}
	if len(failures) == 0 {
		return verifiedImage{}, errors.New("no Sigstore bundles found")
	}
	return verifiedImage{}, errors.Join(failures...)
}

func readImageBundle(reader io.ReadCloser) (*bundle.Bundle, error) {
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxBundleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBundleBytes {
		return nil, fmt.Errorf("bundle exceeds %d bytes", maxBundleBytes)
	}
	b := &bundle.Bundle{}
	if err := b.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return b, nil
}

func verifiedProducerCommit(result *verify.VerificationResult, role imageRole) (string, error) {
	if result.Statement == nil || result.Statement.PredicateType != role.predicate {
		return "", fmt.Errorf("attestation must contain a %s statement", role.predicate)
	}
	if result.Signature == nil || result.Signature.Certificate == nil || result.Signature.Certificate.SourceRepositoryDigest == "" {
		return "", errors.New("attestation certificate has no producer commit")
	}
	cert := result.Signature.Certificate
	if role.releaseWorkflow {
		if result.Statement.Type != "https://in-toto.io/Statement/v1" {
			return "", errors.New("image signature must contain an in-toto v1 statement")
		}
		if !stableReleaseRefRegex.MatchString(cert.SourceRepositoryRef) {
			return "", fmt.Errorf("image signature certificate has no stable release ref: %q", cert.SourceRepositoryRef)
		}
		if cert.SubjectAlternativeName != role.workflow+"@"+cert.SourceRepositoryRef {
			return "", errors.New("image signature workflow identity does not match its source ref")
		}
	}
	return cert.SourceRepositoryDigest, nil
}
