package infra

import (
	"errors"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const defaultDockerRegistry = "registry-1.docker.io"

type RegistryClient struct {
	registry      string
	remoteOptions []remote.Option
}

func NewRegistryClient(image string) *RegistryClient {
	ref, err := name.ParseReference(image)
	if err != nil {
		return nil
	}

	domain := ref.Context().RegistryStr()

	var remoteOptions []remote.Option
	user, pass, err := getRegistryAuthHeader(domain)
	if err != nil {
		remoteOptions = []remote.Option{
			remote.WithAuth(&authn.Basic{Username: user, Password: pass}),
		}
	} else {
		remoteOptions = []remote.Option{}
	}

	return &RegistryClient{
		registry:      domain,
		remoteOptions: remoteOptions,
	}
}

func (r *RegistryClient) GetLatestDigest(image string) (string, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return "", err
	}

	descriptor, err := remote.Get(ref, r.remoteOptions...)
	if err != nil {
		return "", err
	}

	return descriptor.Digest.String(), nil
}

func (r *RegistryClient) DigestExists(repoDigests []string) (bool, error) {
	repoDigest := ""
	for _, digest := range repoDigests {
		if strings.HasPrefix(digest, r.registry) {
			repoDigest = digest
			break
		}
	}

	if repoDigest == "" {
		return false, errors.New("no digest found for the registry")
	}

	digestRef, err := name.ParseReference(repoDigest)
	if err != nil {
		return false, err
	}

	_, err = remote.Get(digestRef, r.remoteOptions...)
	if err != nil {
		return false, nil
	}

	return true, nil
}

func getRegistryAuthHeader(image string) (string, string, error) {
	switch {
	case strings.HasPrefix(image, defaultDockerRegistry):
		return "", "", nil
	case strings.HasPrefix(image, "ghcr.io"):
		token := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN")
		if token == "" {
			return "", "", errors.New("LOCAL_GITHUB_ACCESS_TOKEN not set") // More informative error
		}
		return "x-access-token", token, nil
	case strings.Contains(image, ".azurecr.io"):
		username := os.Getenv("AZURE_REGISTRY_USERNAME")
		password := os.Getenv("AZURE_REGISTRY_PASSWORD")
		if username == "" || password == "" {
			return "", "", errors.New("AZURE_REGISTRY_USERNAME or AZURE_REGISTRY_PASSWORD not set") // More informative error
		}
		return username, password, nil
	default:
		return "", "", errors.New("no registry auth found")
	}
}
