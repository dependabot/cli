// Package credentials retrieves already-resolved credentials for a hosted job.
package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/dependabot/cli/internal/model"
)

type Client struct {
	HTTP       *http.Client
	retryDelay time.Duration
}

func (c Client) Fetch(ctx context.Context, endpoint, token string) ([]model.Credential, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("credentials URL must use HTTPS without user information or a fragment")
	}
	if token == "" {
		return nil, fmt.Errorf("DEPENDABOT_CREDENTIALS_TOKEN is required")
	}
	client := http.Client{Timeout: 30 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	delay := c.retryDelay
	if delay == 0 {
		delay = time.Second
	}
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("construct credentials request")
		}
		req.Header.Set("Authorization", token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "dependabot-cli")
		response, requestErr := client.Do(req)
		if requestErr != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var failure error
		retry := false
		if requestErr != nil {
			// Transport errors can contain credential-bearing URLs; do not expose them.
			failure = fmt.Errorf("credentials request failed")
			retry = true
		} else {
			const maxResponseSize = 16 << 20
			body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				if readErr != nil || len(body) > maxResponseSize {
					return nil, fmt.Errorf("could not read complete credentials response")
				}
				return decode(body)
			}
			failure = fmt.Errorf("credentials request returned HTTP %d", response.StatusCode)
			retry = response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		}
		if !retry || attempt == 3 {
			return nil, failure
		}
		timer := time.NewTimer(delay * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func decode(body []byte) ([]model.Credential, error) {
	var response struct {
		Data struct {
			Attributes struct {
				Credentials *[]model.Credential `json:"credentials"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid credentials response JSON")
	}
	if response.Data.Attributes.Credentials == nil {
		return nil, fmt.Errorf("credentials response must contain a credentials array")
	}
	for _, cred := range *response.Data.Attributes.Credentials {
		if typ, ok := cred["type"].(string); !ok || typ == "" {
			return nil, fmt.Errorf("each credential must have a type")
		}
	}
	return *response.Data.Attributes.Credentials, nil
}
