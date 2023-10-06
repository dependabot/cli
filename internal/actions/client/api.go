package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dependabot/cli/internal/actions/core"
	"github.com/dependabot/cli/internal/actions/github"
	"github.com/dependabot/cli/internal/model"
	"io"
	"net/http"
)

type Client struct {
	baseUrl string
	params  *github.JobParameters
}

func New(baseUrl string, params *github.JobParameters) *Client {
	return &Client{
		baseUrl: baseUrl,
		params:  params,
	}
}

func request[T any](ctx context.Context, method, url, auth string, body io.Reader) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}
	if res.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, res.Body)
		return nil, nil
	}

	var result T
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) JobDetails(ctx context.Context) (*model.Job, error) {
	detailsUrl := fmt.Sprintf("%s/update_jobs/%s/details", c.baseUrl, c.params.JobID)
	return request[model.Job](ctx, http.MethodGet, detailsUrl, c.params.JobToken, nil)
}

func (c *Client) Credentials(ctx context.Context) ([]model.Credential, error) {
	credentialsUrl := fmt.Sprintf("%s/update_jobs/%s/credentials", c.baseUrl, c.params.JobID)
	credentials, err := request[[]model.Credential](ctx, http.MethodGet, credentialsUrl, c.params.CredentialsToken, nil)
	if err != nil {
		return nil, err
	}

	// mask secrets
	for _, credential := range *credentials {
		if credential["password"] != nil {
			core.SetSecret(credential["password"].(string))
		}
		if credential["token"] != nil {
			core.SetSecret(credential["token"].(string))
		}
	}

	return *credentials, nil
}

// TODO
type JobError struct{}

func (c *Client) ReportJobError(ctx context.Context, error JobError) error {
	recordErrorURL := fmt.Sprintf("%s/update_jobs/%s/record_update_job_error", c.baseUrl, c.params.JobID)
	errorReader, err := json.Marshal(error)
	if err != nil {
		return err
	}
	_, err = request[any](ctx, http.MethodPost, recordErrorURL, c.params.JobToken, bytes.NewReader(errorReader))
	return err
}

func (c *Client) MarkJobAsProcessed(ctx context.Context) error {
	markAsProcessedUrl := fmt.Sprintf("%s/update_jobs/%s/mark_as_processed", c.baseUrl, c.params.JobID)
	body := map[string]any{
		"base-commit-sha": "unknown",
	}
	bodyReader, _ := json.Marshal(body)
	_, err := request[any](ctx, http.MethodPatch, markAsProcessedUrl, c.params.JobToken, bytes.NewReader(bodyReader))
	return err
}
