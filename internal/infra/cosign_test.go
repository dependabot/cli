package infra

import (
	"context"
	"testing"
)

func Test_verifySignatures(t *testing.T) {
	var tests = []struct {
		name                  string
		image                 string
		identitySubjectRegExp string
		expected              string
	}{
		{
			name:                  "valid signature",
			image:                 "cgr.dev/chainguard/busybox:latest",
			identitySubjectRegExp: "^https://github\\.com/chainguard-images/images/\\.github/workflows/release\\.yaml@refs/heads/main$",
			expected:              "",
		},
		{
			name:                  "wrong certificate identity",
			image:                 "cgr.dev/chainguard/busybox:latest",
			identitySubjectRegExp: "^https://github\\.com/i-do-not-exist/\\.github/workflows/release\\.yaml@refs/heads/main$",
			expected:              "no matching signatures: none of the expected identities matched what was in the certificate, got subjects [https://github.com/chainguard-images/images/.github/workflows/release.yaml@refs/heads/main] with issuer https://token.actions.githubusercontent.com",
		},
		{
			name:                  "no signature",
			image:                 "docker.io/library/busybox:latest",
			identitySubjectRegExp: "",
			expected:              "no signatures found",
		},
		{
			name:     "invalid image reference",
			image:    ":-:",
			expected: "could not parse reference: :-:",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := verifySignatures(context.Background(), test.image, test.identitySubjectRegExp)
			if test.expected != "" && actual.Error() != test.expected {
				t.Errorf("expected %v, got %v", test.expected, actual)
			}
		})
	}
}
