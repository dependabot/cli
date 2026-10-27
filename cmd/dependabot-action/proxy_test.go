package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dependabot/cli/internal/model"
)

func unsetEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

func TestActionProxyOptions(t *testing.T) {
	customPath := filepath.Join(t.TempDir(), "custom.pem")
	nodePath := filepath.Join(t.TempDir(), "node.pem")
	for path, contents := range map[string]string{customPath: "custom CA", nodePath: "node CA"} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name       string
		customPath string
		customSet  bool
		nodePath   string
		wantCert   string
		wantError  bool
	}{
		{name: "no custom CA"},
		{name: "custom CA takes precedence", customSet: true, customPath: customPath, nodePath: nodePath, wantCert: "custom CA"},
		{name: "Node CA fallback", nodePath: nodePath, wantCert: "node CA"},
		{name: "empty custom CA disables fallback", customSet: true, nodePath: nodePath},
		{name: "missing custom CA does not fall back", customSet: true, customPath: filepath.Join(t.TempDir(), "missing.pem"), nodePath: nodePath, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unsetEnv(t, "CUSTOM_CA_PATH")
			if tc.customSet {
				t.Setenv("CUSTOM_CA_PATH", tc.customPath)
			}
			t.Setenv("NODE_EXTRA_CA_CERTS", tc.nodePath)
			experiments := model.Experiment{"proxy-feature": true}
			options, err := actionProxyOptions("https://api.example/prefix", experiments)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "cannot read custom proxy CA certificate") {
					t.Fatalf("options error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if options.APIURL != "https://api.example/prefix" || !options.WaitForReady ||
				!reflect.DeepEqual(options.Experiments, experiments) || string(options.CACert) != tc.wantCert {
				t.Fatalf("incorrect proxy options: %+v", options)
			}
		})
	}
}

func TestActionProxyEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows environment variable names are case-insensitive")
	}
	for _, tc := range []struct {
		name, lower, upper, want string
	}{
		{name: "lowercase takes precedence", lower: "http://lower.example", upper: "http://upper.example", want: "http://lower.example"},
		{name: "uppercase fallback", upper: "http://upper.example", want: "http://upper.example"},
		{name: "empty values"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"http_proxy", "https_proxy", "no_proxy"} {
				t.Setenv(name, tc.lower)
				t.Setenv(strings.ToUpper(name), tc.upper)
			}
			unsetEnv(t, "CUSTOM_CA_PATH")
			t.Setenv("NODE_EXTRA_CA_CERTS", "")
			options, err := actionProxyOptions("", nil)
			if err != nil || options.HTTPProxy != tc.want || options.HTTPSProxy != tc.want || options.NoProxy != tc.want {
				t.Fatalf("proxy environment = %+v, %v; want %q", options, err, tc.want)
			}
		})
	}
}
