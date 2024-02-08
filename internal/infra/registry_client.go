package infra

import (
	"errors"
	"os"
	"strings"

	"github.com/distribution/reference"
	"github.com/heroku/docker-registry-client/registry"
)

const defaultDockerRegistry = "registry-1.docker.io"

func getLatestDigest(imageName string) (string, error) {
	// Parse the image name using docker reference library
	ref, err := reference.ParseAnyReference(imageName)
	if err != nil {
		return "", err
	}

	named, ok := ref.(reference.Named)
	if !ok {
		return "", errors.New("image name must be a named reference")
	}

	domain := reference.Domain(named)
	if domain == "docker.io" {
		domain = defaultDockerRegistry
	}

	user, pass, err := getRegistryAuthHeader(domain)
	if err != nil {
		return "", err
	}

	client, err := registry.New("https://"+domain, user, pass)
	if err != nil {
		return "", err
	}

	path := reference.Path(ref.(reference.Named))
	var tagName string
	if tagRef, isTagged := ref.(reference.Tagged); isTagged {
		tagName = tagRef.Tag()
	} else {
		tagName = "latest"
	}

	res, err := client.ManifestDigest(path, tagName)
	if err != nil {
		return "", err
	}

	return res.String(), nil
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
