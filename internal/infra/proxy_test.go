package infra

import (
	"os"
	"strings"
	"testing"
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

		env := proxyEnv("", "")

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

		env := proxyEnv("", "")

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

		env := proxyEnv("", "")

		value, ok := envValue(env, "PROXY_CACHE")
		if !ok {
			t.Fatal("expected PROXY_CACHE to be present in proxy env")
		}
		if value != "true" {
			t.Errorf("expected PROXY_CACHE to fall back to %q, got %q", "true", value)
		}
	})
}

func Test_proxyEnv_PackageManager(t *testing.T) {
	t.Run("passes PACKAGE_MANAGER from the parameter", func(t *testing.T) {
		env := proxyEnv("", "go_modules")

		value, ok := envValue(env, "PACKAGE_MANAGER")
		if !ok {
			t.Fatal("expected PACKAGE_MANAGER to be present in proxy env")
		}
		if value != "go_modules" {
			t.Errorf("expected PACKAGE_MANAGER to be %q, got %q", "go_modules", value)
		}
	})

	t.Run("sets an empty PACKAGE_MANAGER when not provided", func(t *testing.T) {
		env := proxyEnv("", "")

		value, ok := envValue(env, "PACKAGE_MANAGER")
		if !ok {
			t.Fatal("expected PACKAGE_MANAGER to be present in proxy env")
		}
		if value != "" {
			t.Errorf("expected PACKAGE_MANAGER to be empty, got %q", value)
		}
	})
}

func Test_proxyEnv_OpenSSLForceFIPSMode(t *testing.T) {
	t.Run("passes OPENSSL_FORCE_FIPS_MODE from environment", func(t *testing.T) {
		t.Setenv("OPENSSL_FORCE_FIPS_MODE", "1")

		env := proxyEnv("", "")

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

		env := proxyEnv("", "")

		if _, ok := envValue(env, "OPENSSL_FORCE_FIPS_MODE"); ok {
			t.Error("expected OPENSSL_FORCE_FIPS_MODE to be absent from proxy env when host has it unset")
		}
	})
}

func Test_proxyEnv_JobToken(t *testing.T) {
	t.Run("passes JOB_TOKEN from environment", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "super-secret-token")

		env := proxyEnv("", "")

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

		env := proxyEnv("", "")

		if _, ok := envValue(env, "JOB_TOKEN"); ok {
			t.Error("expected JOB_TOKEN to be absent from proxy env when host has it unset")
		}
	})

	t.Run("omits JOB_TOKEN when set to empty", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "")

		env := proxyEnv("", "")

		if _, ok := envValue(env, "JOB_TOKEN"); ok {
			t.Error("expected JOB_TOKEN to be absent from proxy env when host has it empty")
		}
	})
}

func Test_proxyEnv_DependabotAPIURL(t *testing.T) {
	t.Run("sets DEPENDABOT_API_URL from the apiURL parameter when JOB_TOKEN is set", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "super-secret-token")

		env := proxyEnv("https://api.example.com", "")

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

		env := proxyEnv("https://api.example.com", "")

		if _, ok := envValue(env, "DEPENDABOT_API_URL"); ok {
			t.Error("expected DEPENDABOT_API_URL to be absent when JOB_TOKEN is unset")
		}
	})

	t.Run("omits DEPENDABOT_API_URL when JOB_TOKEN is empty", func(t *testing.T) {
		t.Setenv("JOB_TOKEN", "")

		env := proxyEnv("https://api.example.com", "")

		if _, ok := envValue(env, "DEPENDABOT_API_URL"); ok {
			t.Error("expected DEPENDABOT_API_URL to be absent when JOB_TOKEN is empty")
		}
	})
}
