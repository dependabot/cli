package server

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/dependabot/cli/internal/model"
)

func TestInput(t *testing.T) {
	inputCh := make(chan *model.Input)
	defer close(inputCh)

	ip := ""
	// prevents security popup
	if os.Getenv("GOOS") == "darwin" {
		ip = "127.0.0.1"
	}
	l, err := net.Listen("tcp", ip+":0")
	if err != nil {
		t.Fatal("Failed to create listener: ", err.Error())
	}

	go func() {
		input, err := Input(l)
		if err != nil {
			t.Errorf("%s", err.Error())
		}
		inputCh <- input
	}()

	url := fmt.Sprintf("http://%s", l.Addr().String())
	data := `{"job":{"package-manager":"test"},"credentials":[{"credential":"value"}]}`
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatal(err.Error())
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", resp.StatusCode)
	}

	// Test will hang here if the server does not shut down
	input := <-inputCh

	if input.Job.PackageManager != "test" {
		t.Errorf("expected package manager to be 'test', got '%s'", input.Job.PackageManager)
	}
	if input.Credentials[0]["credential"] != "value" {
		t.Errorf("expected credential to be 'value', got '%v'", input.Credentials[0])
	}
}
