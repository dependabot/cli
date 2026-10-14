package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAPIRetriesTransportFailure(t *testing.T) {
	api := newJobClient(config{apiURL: "https://dependabot.example", jobID: "42"})
	api.retryDelay = 0
	attempts := 0
	api.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("connection refused")
	})
	if _, err := api.details(context.Background()); err == nil || attempts != 4 || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("transport failure: attempts %d, error %v", attempts, err)
	}
}

func TestAPIRetries(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusUnauthorized} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			attempts := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts == 1 {
					w.WriteHeader(status)
					fmt.Fprint(w, "never-log-this-response")
					return
				}
				fmt.Fprint(w, testJob)
			}))
			defer server.Close()
			api := newJobClient(config{apiURL: server.URL, jobID: "42", jobToken: "secret"})
			api.http = server.Client()
			api.retryDelay = 0
			_, err := api.details(context.Background())
			if status == http.StatusUnauthorized {
				if err == nil || attempts != 1 || strings.Contains(err.Error(), "never-log-this-response") {
					t.Fatalf("authentication failure: attempts %d, error %v", attempts, err)
				}
			} else if err != nil || attempts != 2 {
				t.Fatalf("transient failure: attempts %d, error %v", attempts, err)
			}
		})
	}
}

func TestAPIRetryLimitAndCancellation(t *testing.T) {
	attempts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	api := newJobClient(config{apiURL: server.URL, jobID: "42", jobToken: "secret"})
	api.http = server.Client()
	api.retryDelay = 0
	if _, err := api.details(context.Background()); err == nil || attempts != 4 {
		t.Fatalf("retry limit: attempts %d, error %v", attempts, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := api.details(ctx); err == nil || attempts != 4 {
		t.Fatalf("canceled request: attempts %d, error %v", attempts, err)
	}
}

func TestAPIRejectsRedirects(t *testing.T) {
	var requests int
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	api := newJobClient(config{apiURL: server.URL, jobID: "42", jobToken: "secret"})
	api.http.Transport = server.Client().Transport
	api.http.Timeout = time.Second
	if _, err := api.details(context.Background()); err == nil || requests != 0 {
		t.Fatalf("redirect followed: requests %d, error %v", requests, err)
	}
}

func TestAPIRejectsMalformedPayloads(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"data":{"attributes":null}}`, `{"data":{"attributes":{"source":{}}}}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			api := newJobClient(config{apiURL: server.URL, jobID: "42"})
			api.http = server.Client()
			if _, err := api.details(context.Background()); err == nil {
				t.Error("invalid details accepted")
			}
			if _, err := api.credentials(context.Background()); err == nil {
				t.Error("invalid credentials accepted")
			}
		})
	}
}
