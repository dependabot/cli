package infra

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, e := range env {
		if after, ok := strings.CutPrefix(e, prefix); ok {
			return after, true
		}
	}
	return "", false
}

func Test_proxyEnv_ProxyCache(t *testing.T) {
	t.Run("uses host PROXY_CACHE when set", func(t *testing.T) {
		t.Setenv("PROXY_CACHE", "false")

		env := proxyEnv("")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "false" {
			t.Errorf("expected PROXY_CACHE to be %q, got %q", "false", value)
		}
	})

	t.Run("falls back to true when host PROXY_CACHE is unset", func(t *testing.T) {
		t.Setenv("PROXY_CACHE", "placeholder")
		os.Unsetenv("PROXY_CACHE")

		env := proxyEnv("")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "true" {
			t.Errorf("expected PROXY_CACHE to fall back to %q, got %q", "true", value)
		}
	})

	t.Run("falls back to true when host PROXY_CACHE is empty", func(t *testing.T) {
		t.Setenv("PROXY_CACHE", "")

		env := proxyEnv("")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "true" {
			t.Errorf("expected PROXY_CACHE to fall back to %q, got %q", "true", value)
		}
	})
}

func Test_proxyEnv_OpenSSLForceFIPSMode(t *testing.T) {
	t.Run("passes OPENSSL_FORCE_FIPS_MODE from environment", func(t *testing.T) {
		t.Setenv("OPENSSL_FORCE_FIPS_MODE", "1")

		env := proxyEnv("")

		value, ok := envValue(env, "OPENSSL_FORCE_FIPS_MODE")
		if !ok {
			t.Fatal("expected OPENSSL_FORCE_FIPS_MODE to be present in proxy env")
		}
		if value != "1" {
			t.Errorf("expected OPENSSL_FORCE_FIPS_MODE to be %q, got %q", "1", value)
		}
	})

	t.Run("omits OPENSSL_FORCE_FIPS_MODE when unset", func(t *testing.T) {
		t.Setenv("OPENSSL_FORCE_FIPS_MODE", "placeholder")
		os.Unsetenv("OPENSSL_FORCE_FIPS_MODE")

		env := proxyEnv("")

		if _, ok := envValue(env, "OPENSSL_FORCE_FIPS_MODE"); ok {
			t.Error("expected OPENSSL_FORCE_FIPS_MODE to be absent from proxy env when host has it unset")
		}
	})
}

func Test_proxyEnv_JobToken(t *testing.T) {
	t.Run("passes JOB_TOKEN from environment", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "super-secret-token")

		env := proxyEnv("")

		value, ok := envValue(env, "JOB_TOKEN")
		if !ok {
			t.Fatal("expected JOB_TOKEN to be present in proxy env")
		}
		if value != "super-secret-token" {
			t.Errorf("expected JOB_TOKEN to be %q, got %q", "super-secret-token", value)
		}
	})

	t.Run("omits JOB_TOKEN when unset", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "placeholder")
		os.Unsetenv("JOB_TOKEN")

		env := proxyEnv("")

		if _, ok := envValue(env, "JOB_TOKEN"); ok {
			t.Error("expected JOB_TOKEN to be absent from proxy env when host has it unset")
		}
	})

	t.Run("omits JOB_TOKEN when set to empty", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "")

		env := proxyEnv("")

		if _, ok := envValue(env, "JOB_TOKEN"); ok {
			t.Error("expected JOB_TOKEN to be absent from proxy env when host has it empty")
		}
	})
}

func Test_proxyEnv_DependabotAPIURL(t *testing.T) {
	t.Run("sets DEPENDABOT_API_URL from the apiURL parameter when JOB_TOKEN is set", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "super-secret-token")

		env := proxyEnv("https://api.example.com")

		value, ok := envValue(env, "DEPENDABOT_API_URL")
		if !ok {
			t.Fatal("expected DEPENDABOT_API_URL to be present when JOB_TOKEN is set")
		}
		if value != "https://api.example.com" {
			t.Errorf("expected DEPENDABOT_API_URL to be %q, got %q", "https://api.example.com", value)
		}
	})

	t.Run("omits DEPENDABOT_API_URL when JOB_TOKEN is unset", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "placeholder")
		os.Unsetenv("JOB_TOKEN")

		env := proxyEnv("https://api.example.com")

		if _, ok := envValue(env, "DEPENDABOT_API_URL"); ok {
			t.Error("expected DEPENDABOT_API_URL to be absent when JOB_TOKEN is unset")
		}
	})

	t.Run("omits DEPENDABOT_API_URL when JOB_TOKEN is empty", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "")

		env := proxyEnv("https://api.example.com")

		if _, ok := envValue(env, "DEPENDABOT_API_URL"); ok {
			t.Error("expected DEPENDABOT_API_URL to be absent when JOB_TOKEN is empty")
		}
	})
}

func Test_waitForPortUntil(t *testing.T) {
	t.Run("returns when a connection succeeds", func(t *testing.T) {
		attempts := 0
		err := waitForPortUntil(t.Context(), time.Millisecond, func(context.Context) (bool, error) {
			attempts++
			return attempts == 3, nil
		})

		if err != nil {
			t.Fatalf("waitForPortUntil returned unexpected error: %v", err)
		}
		if attempts != 3 {
			t.Fatalf("expected 3 connection attempts, got %d", attempts)
		}
	})

	t.Run("returns probe errors", func(t *testing.T) {
		probeErr := errors.New("docker exec failed")
		err := waitForPortUntil(t.Context(), time.Millisecond, func(context.Context) (bool, error) {
			return false, probeErr
		})

		if !errors.Is(err, probeErr) {
			t.Fatalf("expected probe error, got %v", err)
		}
	})

	t.Run("returns when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		attempts := 0
		err := waitForPortUntil(ctx, time.Hour, func(context.Context) (bool, error) {
			attempts++
			cancel()
			return false, nil
		})

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
		if attempts != 1 {
			t.Fatalf("expected 1 connection attempt, got %d", attempts)
		}
	})
}

func Test_proxyReadinessError(t *testing.T) {
	t.Run("does not expose the internal readiness deadline", func(t *testing.T) {
		err := proxyReadinessError(t.Context(), context.DeadlineExceeded)

		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected internal readiness deadline to be hidden, got %v", err)
		}
		if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("expected readiness error to retain deadline details, got %v", err)
		}
	})

	t.Run("preserves a parent deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 0)
		defer cancel()
		<-ctx.Done()

		err := proxyReadinessError(ctx, ctx.Err())

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected parent deadline to remain identifiable, got %v", err)
		}
	})

	t.Run("preserves probe errors", func(t *testing.T) {
		probeErr := errors.New("docker exec failed")

		err := proxyReadinessError(t.Context(), probeErr)

		if !errors.Is(err, probeErr) {
			t.Fatalf("expected probe error to remain identifiable, got %v", err)
		}
	})
}
