package infra

import (
	"context"
	"testing"
)

func Test_verifySignatures(t *testing.T) {
	var tests = []struct {
		name                string
		image               string
		certificateIdentity string
		expected            string
	}{
		{
			name:                "valid signature",
			image:               "cgr.dev/chainguard/busybox:latest",
			certificateIdentity: "https://github.com/chainguard-images/images/.github/workflows/release.yaml@refs/heads/main",
			expected:            "",
		},
		{
			name:                "wrong certificate identity",
			image:               "cgr.dev/chainguard/busybox:latest",
			certificateIdentity: "https://github.com/i-do-not-exist/.github/workflows/release.yaml@refs/heads/main",
			expected:            "no matching signatures:\nexpected identity not found in certificate",
		},
		{
			name:                "no signature",
			image:               "docker.io/library/busybox:latest",
			certificateIdentity: "",
			expected:            "no matching signatures:\n",
		},
		{
			name:     "invalid image reference",
			image:    ":-:",
			expected: "could not parse reference: :-:",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := verifySignatures(context.Background(), test.image, test.certificateIdentity)
			if test.expected != "" && actual.Error() != test.expected {
				t.Errorf("expected %v, got %v", test.expected, actual)
			}
		})
	}
}
