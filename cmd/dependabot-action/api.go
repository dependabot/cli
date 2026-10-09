package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dependabot/cli/internal/model"
)

type jobClient struct {
	config
	http       *http.Client
	retryDelay time.Duration
}

func newJobClient(cfg config) *jobClient {
	return &jobClient{
		config: cfg,
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryDelay: time.Second,
	}
}

func (c *jobClient) details(ctx context.Context) (*model.Job, error) {
	body, err := c.request(ctx, http.MethodGet, "details", c.jobToken, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Attributes *model.Job `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid job details JSON")
	}
	job := response.Data.Attributes
	if job == nil || job.PackageManager == "" || job.Source.Repo == "" {
		return nil, fmt.Errorf("job details must include package-manager and source.repo")
	}
	return job, nil
}

func (c *jobClient) credentials(ctx context.Context) ([]model.Credential, error) {
	body, err := c.request(ctx, http.MethodGet, "credentials", c.credentialsToken, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Attributes struct {
				Credentials *[]model.Credential `json:"credentials"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid credentials JSON")
	}
	if response.Data.Attributes.Credentials == nil {
		return nil, fmt.Errorf("credentials response must include a credentials array")
	}
	creds := *response.Data.Attributes.Credentials
	for _, cred := range creds {
		if typ, ok := cred["type"].(string); !ok || typ == "" {
			return nil, fmt.Errorf("each credential must include a type")
		}
	}
	return creds, nil
}

func (c *jobClient) reportFailure(ctx context.Context, message string) error {
	body, err := json.Marshal(map[string]any{"data": map[string]any{
		"error-type":    "actions_workflow_updater",
		"error-details": map[string]string{"action-error": message},
	}})
	if err != nil {
		return err
	}
	if _, err := c.request(ctx, http.MethodPost, "record_update_job_error", c.jobToken, body); err != nil {
		return err
	}
	_, err = c.request(ctx, http.MethodPatch, "mark_as_processed", c.jobToken, []byte(`{"data":{"base-commit-sha":"unknown"}}`))
	return err
}

func (c *jobClient) request(ctx context.Context, method, endpoint, token string, body []byte) ([]byte, error) {
	url := strings.TrimRight(c.apiURL, "/") + "/update_jobs/" + c.jobID + "/" + endpoint
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("%s: cannot construct request", endpoint)
		}
		req.Header.Set("Authorization", token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "dependabot-cli-action")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.http.Do(req)
		retry := false
		var failure error
		if err != nil {
			failure = fmt.Errorf("%s: request failed: %w", endpoint, err)
			retry = method == http.MethodGet
		} else {
			const maxResponseSize = 16 << 20
			data, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponseSize+1))
			res.Body.Close()
			if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNoContent {
				if readErr != nil || len(data) > maxResponseSize {
					return nil, fmt.Errorf("%s: failed to read complete response", endpoint)
				}
				return data, nil
			}
			failure = fmt.Errorf("%s: HTTP %d", endpoint, res.StatusCode)
			// Retrying callbacks could record the same failure twice.
			retry = method == http.MethodGet && (res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500)
		}
		if !retry || attempt == 3 {
			return nil, failure
		}
		timer := time.NewTimer(c.retryDelay * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}
