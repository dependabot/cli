package credentials

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetch(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/jobs/1/credentials?graph_directory=%2Fapi" {
			t.Errorf("unexpected credential request: %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "opaque-token" {
			t.Error("missing credentials authorization")
		}
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"data":{"attributes":{"credentials":[{"type":"npm_registry","registry":"registry.example","token":"$literal-secret"}]}}}`)
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), retryDelay: time.Millisecond}
	creds, err := client.Fetch(context.Background(), server.URL+"/jobs/1/credentials?graph_directory=%2Fapi", "opaque-token")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(creds) != 1 || creds[0]["token"] != "$literal-secret" {
		t.Fatalf("calls=%d, credential response was not preserved", calls)
	}
}

func TestFetchRejectsUnsafeOrInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "do not print this secret"},
		{"missing credentials", 200, `{"data":{"attributes":{}}}`},
		{"null credentials", 200, `{"data":{"attributes":{"credentials":null}}}`},
		{"missing type", 200, `{"data":{"attributes":{"credentials":[{"token":"do not print this secret"}]}}}`},
		{"invalid JSON", 200, "do not print this secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := (Client{HTTP: server.Client()}).Fetch(context.Background(), server.URL, "opaque-token")
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "do not print this secret") || strings.Contains(err.Error(), "opaque-token") {
				t.Fatal("credential request error exposed a secret")
			}
		})
	}
}

func TestFetchDoesNotFollowRedirects(t *testing.T) {
	var redirected bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := (Client{HTTP: server.Client()}).Fetch(context.Background(), server.URL, "opaque-token")
	if err == nil || redirected {
		t.Fatal("credential redirect must fail without forwarding authorization")
	}
}

func TestFetchRequiresHTTPSAndToken(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "https://user:password@example.com", "https://example.com/#fragment"} {
		if _, err := (Client{}).Fetch(context.Background(), endpoint, "opaque-token"); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	if _, err := (Client{}).Fetch(context.Background(), "https://example.com", ""); err == nil {
		t.Fatal("accepted empty token")
	}
}

func TestFetchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Client{}).Fetch(ctx, "https://example.com", "opaque-token")
	if err == nil {
		t.Fatal("accepted cancelled request")
	}
}
