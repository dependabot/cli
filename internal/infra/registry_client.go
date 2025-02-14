package infra

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const defaultDockerRegistry = "registry-1.docker.io"

func getLatestTimestamp(imageName string) (time.Time, error) {
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return time.Time{}, err
	}

	// Get the domain of the image
	domain := ref.Context().RegistryStr()

	user, pass, err := getRegistryAuthHeader(domain)
	if err != nil {
		return time.Time{}, err
	}

	// Create a new registry client using the go-containerregistry library
	remoteOptions := remote.WithAuth(&authn.Basic{Username: user, Password: pass})
	descriptor, err := remote.Get(ref, remoteOptions)
	if err != nil {
		return time.Time{}, err
	}

	image, err := descriptor.Image()
	if err != nil {
		return time.Time{}, err
	}

	// Get the config of the image
	configFile, err := image.ConfigFile()
	if err != nil {
		return time.Time{}, err
	}

	timestamp := configFile.Created.Time
	return timestamp, nil
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
