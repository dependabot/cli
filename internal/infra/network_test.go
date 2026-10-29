package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func newDockerClientForNetworkTests(t *testing.T, handler http.Handler) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.44"),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func TestNewNetworks_CleansUpNoInternetNetworkWhenSecondCreateFails(t *testing.T) {
	var mu sync.Mutex
	createCalls := 0
	removeCalls := 0

	cli := newDockerClientForNetworkTests(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.44/networks/create":
			createCalls++
			if createCalls == 1 {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"ID": "no-internet-id"})
				return
			}
			http.Error(w, "internet create failed", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.44/networks/no-internet-id":
			removeCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	_, err := NewNetworks(context.Background(), cli)
	if err == nil {
		t.Fatalf("expected NewNetworks to return an error when second network create fails")
	}

	if removeCalls != 1 {
		t.Fatalf("expected rollback to remove first-created network once, got %d", removeCalls)
	}
}

func TestNetworksClose_AttemptsRemovingBothNetworksWhenFirstRemoveFails(t *testing.T) {
	var mu sync.Mutex
	removed := map[string]int{}

	cli := newDockerClientForNetworkTests(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}

		switch r.URL.Path {
		case "/v1.44/networks/no-internet-id":
			removed["no-internet-id"]++
			http.Error(w, "cannot remove first network", http.StatusInternalServerError)
		case "/v1.44/networks/internet-id":
			removed["internet-id"]++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	nets := &Networks{
		cli:        cli,
		NoInternet: network.CreateResponse{ID: "no-internet-id"},
		Internet:   network.CreateResponse{ID: "internet-id"},
	}

	err := nets.Close()
	if err == nil {
		t.Fatalf("expected Close to return an error when one of the removals fails")
	}

	if removed["no-internet-id"] != 1 {
		t.Fatalf("expected no-internet network removal attempt once, got %d", removed["no-internet-id"])
	}
	if removed["internet-id"] != 1 {
		t.Fatalf("expected internet network removal to still be attempted once, got %d", removed["internet-id"])
	}
}
