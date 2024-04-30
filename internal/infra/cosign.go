package infra

import (
	"context"
	"fmt"
	"log"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	sigstoreroot "github.com/sigstore/sigstore-go/pkg/root"
)

const (
	GitHubOidcIssuer                            = "https://token.actions.githubusercontent.com"
	DependabotUpdaterIdentitySubjectRegExp      = "^https://github\\.com/dependabot/dependabot-core/\\.github/workflows/images-latest\\.yml@refs/heads/main$"
	OpenTelemetryCollectorIdentitySubjectRegExp = "^https://github\\.com/open-telemetry/opentelemetry-collector-releases/\\.github/workflows/base-release\\.yaml@refs/tags/v\\d+\\.\\d+\\.\\d+$"
)

func verifySignatures(ctx context.Context, s string, identitySubjectRegexp string) error {
	reference, err := name.ParseReference(s)
	if err != nil {
		return err
	}

	trustedRoot, err := sigstoreroot.FetchTrustedRoot()
	if err != nil {
		return err
	}

	co := &cosign.CheckOpts{
		Identities:      []cosign.Identity{{Issuer: GitHubOidcIssuer, SubjectRegExp: identitySubjectRegexp}},
		NewBundleFormat: true,
		TrustedMaterial: trustedRoot,
	}

	_, bundleVerified, err := cosign.VerifyImageAttestations(ctx, reference, co)
	if err != nil {
		return err
	}

	if !bundleVerified {
		return fmt.Errorf("failed to verify signature for %s", reference.Name())
	}

	log.Printf("verified signature for %s", reference.Name())

	return nil
}
