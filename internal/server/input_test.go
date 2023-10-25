package server

import (
	"bytes"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/model"
)

func TestInput(t *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	var input *model.Input
	go func() {
		input, _ = Input(8080)
		wg.Done()
	}()
	// give the server time to start
	time.Sleep(10 * time.Millisecond)

	data := `{"job":{"package-manager":"test"},"credentials":[{"credential":"value"}]}`
	resp, err := http.Post("http://localhost:8080", "application/json", bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatal(err.Error())
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", resp.StatusCode)
	}
	wg.Wait()

	if input.Job.PackageManager != "test" {
		t.Errorf("expected package manager to be 'test', got '%s'", input.Job.PackageManager)
	}
	if input.Credentials[0]["credential"] != "value" {
		t.Errorf("expected credential to be 'value', got '%v'", input.Credentials[0])
	}
}
