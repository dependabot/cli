package infra

import (
	"errors"
	"github.com/distribution/reference"
	"github.com/heroku/docker-registry-client/registry"
	"os"
	"strings"
)

const defaultDockerRegistry = "registry-1.docker.io"

func getLatestDigest(imageName string) (string, error) {
	// Parse the image name using docker reference library
	ref, err := reference.ParseAnyReference(imageName)
	if err != nil {
		return "", err
	}

	domain := reference.Domain(ref.(reference.Named))
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
	if strings.HasPrefix(image, defaultDockerRegistry) {
		return "", "", nil
	} else if strings.HasPrefix(image, "ghcr.io") {
		token := os.Getenv("LOCAL_GITHUB_ACCESS_TOKEN")
		return "x-access-token", token, nil
	} else if strings.Contains(image, ".azurecr.io") {
		username := os.Getenv("AZURE_REGISTRY_USERNAME")
		password := os.Getenv("AZURE_REGISTRY_PASSWORD")
		return username, password, nil
	}
	return "", "", errors.New("no registry auth found")
}
