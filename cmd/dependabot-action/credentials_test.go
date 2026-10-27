package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dependabot/cli/internal/model"
)

func TestRegistryCredentials(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		invalid bool
		want    []model.Credential
	}{
		{
			name: "credentials", payload: `[{"type":"npm_registry","url":"https://registry.example/npm/","token":"literal$TOKEN","custom-secret":"secret-value","replaces-base":false}]`,
			want: []model.Credential{{
				"type": "npm_registry", "url": "https://registry.example/npm/", "token": "literal$TOKEN",
				"custom-secret": "secret-value", "replaces-base": false,
			}},
		},
		{name: "empty array", payload: `[]`, want: []model.Credential{}},
		{name: "null", payload: `null`},
		{name: "malformed JSON", payload: `[{"token":"do-not-log-secret"`, invalid: true},
		{name: "object instead of array", payload: `{"token":"do-not-log-secret"}`, invalid: true},
		{name: "scalar instead of array", payload: `"do-not-log-secret"`, invalid: true},
		{name: "missing type", payload: `[{"token":"do-not-log-secret"}]`, invalid: true},
		{name: "null credential", payload: `[null]`, invalid: true},
		{name: "invalid type", payload: `[{"type":42,"token":"do-not-log-secret"}]`, invalid: true},
		{name: "invalid credential discards batch", payload: `[{"type":"npm_registry","token":"do-not-log-secret"},{}]`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := base64.StdEncoding.EncodeToString([]byte(tc.payload))
			var output bytes.Buffer
			got, err := registryCredentials(value, &output)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("registryCredentials() = %v, %v; want %v", got, err, tc.want)
			}
			if tc.invalid {
				if output.String() != "Failed to parse GITHUB_REGISTRIES_PROXY environment variable\n" {
					t.Fatalf("unexpected parse diagnostic: %q", output.String())
				}
			} else if output.Len() != 0 {
				t.Fatalf("unexpected output: %q", output.String())
			}
		})
	}
	for _, value := range []string{"", "do-not-log-secret%%%"} {
		var output bytes.Buffer
		creds, err := registryCredentials(value, &output)
		if err != nil || len(creds) != 0 || strings.Contains(output.String(), "do-not-log-secret") {
			t.Fatalf("invalid or absent base64 = %v, %v, %q", creds, err, output.String())
		}
		if value != "" && output.Len() == 0 {
			t.Fatal("invalid base64 was not diagnosed")
		}
	}
}

func TestCredentialsMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		creds []model.Credential
		want  []model.Credential
	}{
		{
			name: "allowlist and deduplication",
			creds: []model.Credential{
				{
					"type": "git_source", "host": "github.com", "url": "https://github.com",
					"registry": "github.com", "index-url": "https://github.com/simple",
					"env-key": "GIT_TOKEN", "organization": "org", "replaces-base": false,
					"scope": "scope", "public-key-fingerprint": "fingerprint", "repo": "org/repo",
					"password": "secret", "token": "secret", "custom-secret": "secret",
					"username": "user", "accessible-repos": []string{"org/private"},
				},
				{"type": "git_source", "host": "github.com", "password": "first"},
				{"host": "github.com", "type": "git_source", "password": "second"},
				{"type": "jit_access", "host": "github.com", "endpoint": "/jit"},
				{"type": "npm_registry", "host": "registry.example", "proxy-only": true},
				{"type": "npm_registry", "host": "registry.example", "proxy-only": false},
			},
			want: []model.Credential{
				{
					"type": "git_source", "host": "github.com", "url": "https://github.com",
					"registry": "github.com", "index-url": "https://github.com/simple",
					"env-key": "GIT_TOKEN", "organization": "org", "replaces-base": false,
					"scope": "scope", "public-key-fingerprint": "fingerprint", "repo": "org/repo",
				},
				{"type": "git_source", "host": "github.com"},
				{"type": "npm_registry", "host": "registry.example"},
			},
		},
		{
			name: "registry URL fallbacks",
			creds: []model.Credential{
				{"type": "npm_registry", "url": "https://registry.example:443/npm/"},
				{"type": "npm_registry", "url": "https://registry.example"},
				{"type": "npm_registry", "url": "https://REGISTRY.EXAMPLE/npm%20packages/"},
				{"type": "composer_repository", "url": "https://composer.example/path"},
				{"type": "docker_registry", "url": "https://docker.example:5000/v2/"},
				{"type": "python_index", "url": "https://python.example/simple"},
				{"type": "git_source", "url": "https://github.com/org/repo"},
			},
			want: []model.Credential{
				{"type": "npm_registry", "url": "https://registry.example:443/npm/", "registry": "registry.example/npm/"},
				{"type": "npm_registry", "url": "https://registry.example", "registry": "registry.example/"},
				{"type": "npm_registry", "url": "https://REGISTRY.EXAMPLE/npm%20packages/", "registry": "registry.example/npm%20packages/"},
				{"type": "composer_repository", "url": "https://composer.example/path", "registry": "composer.example"},
				{"type": "docker_registry", "url": "https://docker.example:5000/v2/", "registry": "docker.example"},
				{"type": "python_index", "url": "https://python.example/simple", "index-url": "https://python.example/simple"},
				{"type": "git_source", "url": "https://github.com/org/repo"},
			},
		},
		{
			name: "explicit locations preserved",
			creds: []model.Credential{
				{"type": "npm_registry", "url": "https://registry.example/npm/", "registry": "explicit.example"},
				{"type": "python_index", "url": "https://python.example/simple", "index-url": "https://explicit.example/simple"},
			},
			want: []model.Credential{
				{"type": "npm_registry", "url": "https://registry.example/npm/", "registry": "explicit.example"},
				{"type": "python_index", "url": "https://python.example/simple", "index-url": "https://explicit.example/simple"},
			},
		},
		{
			name: "invalid registry URLs ignored",
			creds: []model.Credential{
				{"type": "npm_registry", "url": "://bad"},
				{"type": "docker_registry", "url": "relative/path"},
				{"type": "composer_repository", "url": "//registry.example/path"},
			},
			want: []model.Credential{
				{"type": "npm_registry", "url": "://bad"},
				{"type": "docker_registry", "url": "relative/path"},
				{"type": "composer_repository", "url": "//registry.example/path"},
			},
		},
		{name: "no credentials", want: []model.Credential{}},
		{
			name: "only proxy credentials",
			creds: []model.Credential{
				{"type": "jit_access"}, {"type": "npm_registry", "proxy-only": true},
			},
			want: []model.Credential{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.creds)
			if err != nil {
				t.Fatal(err)
			}
			got, err := credentialsMetadata(tc.creds)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("credentialsMetadata() = %v, %v; want %v", got, err, tc.want)
			}
			after, err := json.Marshal(tc.creds)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("source credentials changed: %s -> %s, %v", before, after, err)
			}
		})
	}
}
