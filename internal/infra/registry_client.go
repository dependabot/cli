package infra

import (
	"github.com/distribution/reference"
	"github.com/heroku/docker-registry-client/registry"
	"strings"
)

const defaultDockerRegistry = "https://registry-1.docker.io"

func getLatestDigest(imageName string) (string, error) {
	// Parse the image name using docker reference library
	ref, err := reference.ParseAnyReference(imageName)
	if err != nil {
		return "", err
	}
	switch ref.(type) {
	case reference.Digested:
	case reference.Tagged:
		return "", nil
	}

	reg, err := getRegistryUrl(ref)
	if reg == "" || err != nil {
		return "", err
	}

	client, err := registry.New(reg, "", "")
	if err != nil {
		return "", err
	}

	name, err := GetNamedRef(ref)
	if err != nil {
		return "", err
	}
	tag, err := getTag(ref)
	if err != nil {
		return "", err
	}

	res, err := client.ManifestDigest(name, tag)
	if err != nil {
		return "", err
	}

	return res.String(), nil
}

func getRegistryUrl(ref reference.Reference) (string, error) {
	switch t := ref.(type) {
	case reference.Named:
		fullName := t.Name()
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) > 1 && strings.Contains(parts[0], ".") {
			if parts[0] == "docker.io" {
				return defaultDockerRegistry, nil
			}
			return parts[0], nil
		}
	}
	// If no registry is provided in the reference, return the default Docker registry URL
	return defaultDockerRegistry, nil
}

func getTag(ref reference.Reference) (string, error) {
	switch t := ref.(type) {
	case reference.NamedTagged:
		return t.Tag(), nil
	}
	return "latest", nil
}

func GetNamedRef(ref reference.Reference) (string, error) {
	var name string
	if nn, ok := ref.(reference.Named); ok {
		name = nn.Name()
	}
	if nn, ok := ref.(reference.NamedTagged); ok {
		name = nn.Name()
	}
	if nn, ok := ref.(reference.Canonical); ok {
		name = nn.Name()
	}
	if name != "" {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) > 1 {
			return parts[1], nil
		}
	}
	return "", nil
}
