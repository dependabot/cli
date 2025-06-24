package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dependabot/cli/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
)

func Test_decodeWrapper(t *testing.T) {
	t.Run("reject extra data", func(t *testing.T) {
		_, err := decodeWrapper("update_dependency_list", []byte(`data: {"unknown": "value"}`))
		if err == nil {
			t.Error("expected decode would error on extra data")
		}
	})
}

func TestAPI_ServeHTTP(t *testing.T) {
	t.Run("doesn't crash when unknown endpoint is used", func(t *testing.T) {
		request := httptest.NewRequest("POST", "/unexpected-endpoint", nil)
		response := httptest.NewRecorder()

		api := NewAPI(nil, nil)
		api.ServeHTTP(response, request)

		if response.Code != http.StatusNotImplemented {
			t.Errorf("expected status code %d, got %d", http.StatusNotImplemented, response.Code)
		}
	})
}

type Wrapper[T any] struct {
	Data T `json:"data"`
}

func TestAPI_CreatePullRequest_ReplacesBinaryWithHash(t *testing.T) {
	var stdout bytes.Buffer

	api := NewAPI(nil, &stdout)
	defer api.Stop()

	content := base64.StdEncoding.EncodeToString([]byte("Hello, world!"))
	hash := sha256.Sum256([]byte(content))
	expectedHashedContent := hex.EncodeToString(hash[:])

	// Construct the request body for create_pull_request
	createPullRequest := model.CreatePullRequest{
		UpdatedDependencyFiles: []model.DependencyFile{
			{
				Content:         content,
				ContentEncoding: "base64",
			},
		},
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(model.UpdateWrapper{Data: createPullRequest}); err != nil {
		t.Fatalf("failed to encode request body: %v", err)
	}

	url := "http://127.0.0.1:" + // use the API's port
		fmt.Sprintf("%d/create_pull_request", api.Port())
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if len(api.Errors) > 0 {
		t.Fatalf("expected no errors, got %d errors: %v", len(api.Errors), api.Errors)
	}

	// The API should have replaced the content with a SHA hash in a.Actual.Output
	if len(api.Actual.Output) != 1 {
		t.Fatalf("expected 1 output, got %d", len(api.Actual.Output))
	}
	if api.Actual.Output[0].Type != "create_pull_request" {
		t.Fatalf("expected output type 'create_pull_request', got '%s'", api.Actual.Output[0].Type)
	}
	if api.Actual.Output[0].Expect.Data.(model.CreatePullRequest).UpdatedDependencyFiles[0].Content != expectedHashedContent {
		t.Errorf("expected content to be 'hello', got '%s'", api.Actual.Output[0].Expect.Data.(model.CreatePullRequest).UpdatedDependencyFiles[0].Content)
	}

	// stdout should contain the original content so folks can create PRs
	var wrapper Wrapper[model.CreatePullRequest]
	if err := json.NewDecoder(&stdout).Decode(&wrapper); err != nil {
		t.Fatalf("failed to decode stdout: %v", err)
	}
	if wrapper.Data.UpdatedDependencyFiles[0].Content != content {
		t.Errorf("expected stdout to contain the original content, got '%s'", stdout.String())
	}
}
