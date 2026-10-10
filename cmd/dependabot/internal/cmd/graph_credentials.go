package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/dependabot/cli/internal/model"
	"gopkg.in/yaml.v3"
)

func loadGraphCredentials(ctx context.Context, input *model.Input, endpoint, file, proxyAPIURL, callbackURL string) error {
	if proxyAPIURL != "" {
		u, err := url.Parse(proxyAPIURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("proxy-api-url must be an HTTPS URL without credentials, query, or fragment")
		}
		if os.Getenv("JOB_TOKEN") == "" {
			return fmt.Errorf("JOB_TOKEN is required with proxy-api-url")
		}
	}
	if endpoint == "" && file == "" {
		return nil
	}
	if endpoint != "" && file != "" {
		return fmt.Errorf("credentials-url and credentials-file are mutually exclusive")
	}
	if endpoint != "" {
		if input.Job.Command != "" && input.Job.Command != model.UpdateGraphCommand {
			return fmt.Errorf("credentials-url can only be used with a graph job")
		}
		if callbackURL != "" {
			return fmt.Errorf("credentials-url requires local graph callback capture; omit api-url")
		}
		creds, err := fetchGraphCredentials(ctx, endpoint, os.Getenv("DEPENDABOT_CREDENTIALS_TOKEN"))
		if err != nil {
			return fmt.Errorf("fetch graph credentials: %w", err)
		}
		input.Credentials = creds
	} else {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read graph credentials file: %w", err)
		}
		var supplied struct {
			Credentials *[]model.Credential `yaml:"credentials"`
		}
		if err := yaml.Unmarshal(data, &supplied); err != nil || supplied.Credentials == nil {
			return fmt.Errorf("credentials file must contain a YAML credentials array")
		}
		input.Credentials = *supplied.Credentials
	}
	input.Job.CredentialsMetadata = nil
	for _, cred := range input.Credentials {
		typ, ok := cred["type"].(string)
		if !ok || typ == "" {
			return fmt.Errorf("each graph credential must have a type")
		}
		if typ == "jit_access" && proxyAPIURL == "" {
			return fmt.Errorf("jit_access credentials require proxy-api-url and JOB_TOKEN")
		}
		entry := make(model.Credential)
		for key, value := range cred {
			switch key {
			case "username", "password", "token", "key", "auth-key", "private-key", "client-secret", "secret":
				continue
			default:
				entry[key] = value
			}
		}
		input.Job.CredentialsMetadata = append(input.Job.CredentialsMetadata, entry)
	}
	return nil
}
