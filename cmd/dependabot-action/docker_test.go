package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDockerAction(t *testing.T) {
	if os.Getenv("DEPENDABOT_ACTION_INTEGRATION") != "1" {
		t.Skip("set DEPENDABOT_ACTION_INTEGRATION=1 to build and run the Docker Action")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actionImage := "dependabot-action-test:" + suffix
	updaterImage := "dependabot-action-updater-test:" + suffix
	proxyImage := "dependabot-action-proxy-test:" + suffix
	docker := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v\n%s", args[0], err, out)
		}
		return out
	}
	t.Cleanup(func() {
		if out, err := exec.Command("docker", "image", "rm", actionImage, updaterImage, proxyImage).CombinedOutput(); err != nil {
			t.Logf("fixture image cleanup: %v\n%s", err, out)
		}
	})
	docker("build", "-q", "-t", actionImage, root)
	fixtures := filepath.Join(root, "cmd/dependabot-action/testdata")
	docker("build", "-q", "--target", "updater", "-t", updaterImage, fixtures)
	docker("build", "-q", "--target", "proxy", "-t", proxyImage, fixtures)
	for _, image := range []string{actionImage, updaterImage, proxyImage} {
		platform := strings.TrimSpace(string(docker("image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image)))
		if platform != "linux/"+runtime.GOARCH {
			t.Fatalf("fixture %s has platform %s, want native linux/%s", image, platform, runtime.GOARCH)
		}
	}

	cert, certificatePEM := actionTestCertificate(t)
	var mu sync.Mutex
	var requests []string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		endpoint := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		requests = append(requests, endpoint)
		expectedToken := "job-secret"
		if endpoint == "credentials" {
			expectedToken = "cred-secret"
		}
		if r.Header.Get("Authorization") != expectedToken || !strings.HasPrefix(r.URL.Path, "/api/update_jobs/42/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch endpoint {
		case "details":
			fmt.Fprint(w, testJob)
		case "credentials":
			fmt.Fprint(w, testCredentials)
		case "record_update_job_error", "mark_as_processed":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	server.Listener.Close()
	server.Listener, err = net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port

	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("update/failure=%v", failure), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0755); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(dir, "cache")
			if err := os.Mkdir(cache, 0777); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(cache, 0777); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "ca.pem"), certificatePEM, 0644); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			requests = nil
			mu.Unlock()
			name := fmt.Sprintf("dependabot-action-test-%s-update-%v", suffix, failure)
			t.Cleanup(func() {
				if out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput(); err != nil {
					t.Logf("Action container cleanup: %v\n%s", err, out)
				}
			})
			args := []string{
				"run", "--name", name,
				"--add-host", "host.docker.internal:host-gateway",
				"--volume", "/var/run/docker.sock:/var/run/docker.sock",
				"--volume", dir + ":/github/workspace",
				"--env", "SSL_CERT_FILE=/github/workspace/ca.pem",
				"--env", fmt.Sprintf("INPUT_API-URL=https://host.docker.internal:%d/api", port),
				"--env", "INPUT_JOB-ID=42",
				"--env", "GITHUB_DEPENDABOT_JOB_TOKEN=job-secret",
				"--env", "GITHUB_DEPENDABOT_CRED_TOKEN=cred-secret",
				"--env", "INPUT_UPDATER-IMAGE=" + updaterImage,
				"--env", "INPUT_PROXY-IMAGE=" + proxyImage,
				"--env", "INPUT_PULL-IMAGES=false",
				"--env", "INPUT_TIMEOUT=1m",
				"--env", "INPUT_VOLUMES=/github/workspace/cache:/cache",
				"--env", fmt.Sprintf("INPUT_UPDATER-ENV=CACHE_PATH=/cache\nFAIL=%v", failure),
				"--env", "TOKEN=must-not-be-expanded",
				actionImage,
			}
			out, err := exec.CommandContext(t.Context(), "docker", args...).CombinedOutput()
			if (err != nil) != failure {
				t.Fatalf("Action exit = %v\n%s", err, out)
			}
			data, readErr := os.ReadFile(filepath.Join(cache, "result"))
			if readErr != nil || string(data) != "job=42\n" {
				t.Fatalf("cache bind mount not shared: %q, %v\n%s", data, readErr, out)
			}
			if !bytes.Contains(out, []byte("proxy-credentials-ok")) {
				t.Fatalf("proxy did not receive literal credentials\n%s", out)
			}
			for _, secret := range []string{"job-secret", "cred-secret", "target$TOKEN", "dependency-secret", "registry-secret"} {
				if !bytes.Contains(out, []byte("::add-mask::"+secret)) {
					t.Errorf("missing mask for %q", secret)
				}
			}
			mu.Lock()
			got := strings.Join(requests, ",")
			mu.Unlock()
			want := "details,credentials"
			if failure {
				want += ",record_update_job_error,mark_as_processed"
			}
			if got != want {
				t.Fatalf("API lifecycle = %q, want %q\n%s", got, want, out)
			}
		})
	}
}

func actionTestCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "host.docker.internal"},
		DNSNames: []string{"host.docker.internal"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
