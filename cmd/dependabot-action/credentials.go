package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/dependabot/cli/internal/model"
)

func registryCredentials(value string, output io.Writer) ([]model.Credential, error) {
	if value == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(value)
	var creds []model.Credential
	if err == nil {
		err = json.Unmarshal(data, &creds)
	}
	if err == nil {
		for _, cred := range creds {
			if typ, ok := cred["type"].(string); !ok || typ == "" {
				err = fmt.Errorf("credential type is required")
				break
			}
		}
	}
	if err != nil {
		if _, writeErr := fmt.Fprintln(output, "Failed to parse GITHUB_REGISTRIES_PROXY environment variable"); writeErr != nil {
			return nil, writeErr
		}
		return nil, nil
	}
	return creds, nil
}

func credentialsMetadata(creds []model.Credential) ([]model.Credential, error) {
	result := make([]model.Credential, 0, len(creds))
	seen := make(map[string]bool)
	for _, cred := range creds {
		if cred["type"] == "jit_access" || cred["proxy-only"] == true {
			continue
		}
		metadata := model.Credential{}
		for _, key := range []string{
			"type", "host", "registry", "url", "index-url", "env-key", "organization",
			"replaces-base", "scope", "public-key-fingerprint", "repo",
		} {
			if value, ok := cred[key]; ok {
				metadata[key] = value
			}
		}
		rawURL, _ := cred["url"].(string)
		registry, _ := cred["registry"].(string)
		if registry == "" && rawURL != "" {
			switch cred["type"] {
			case "npm_registry", "composer_repository", "docker_registry":
				if parsed, err := url.Parse(rawURL); err == nil && parsed.IsAbs() && parsed.Hostname() != "" {
					registry = strings.ToLower(parsed.Hostname())
					if cred["type"] == "npm_registry" {
						path := parsed.EscapedPath()
						if path == "" {
							path = "/"
						}
						registry += path
					}
					metadata["registry"] = registry
				}
			}
		}
		if indexURL, _ := cred["index-url"].(string); cred["type"] == "python_index" && indexURL == "" && rawURL != "" {
			metadata["index-url"] = rawURL
		}
		key, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("cannot encode credential metadata")
		}
		if !seen[string(key)] {
			seen[string(key)] = true
			result = append(result, metadata)
		}
	}
	return result, nil
}
