package infra

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/cmd/cosign/cli/fulcio"
	"github.com/sigstore/cosign/pkg/cosign"
)

const (
	GitHubOidcIssuer                     = "https://token.actions.githubusercontent.com"
	DependabotUpdaterCertificateIdentity = "https://github.com/dependabot/dependabot-core/.github/workflows/images-latest.yml@refs/heads/main"
)

func verifySignatures(ctx context.Context, s string, certificateIdentity string) error {
	reference, err := name.ParseReference(s)
	if err != nil {
		return err
	}

	roots, err := fulcio.GetRoots()
	if err != nil {
		return err
	}

	co := &cosign.CheckOpts{
		CertIdentity:   certificateIdentity,
		CertOidcIssuer: GitHubOidcIssuer,
		RootCerts:      roots,
	}

	_, bundleVerified, err := cosign.VerifyImageSignatures(ctx, reference, co)
	if err != nil {
		return err
	}

	if !bundleVerified {
		return fmt.Errorf("failed to verify signature for %s", reference.Name())
	}

	return nil
}
