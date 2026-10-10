package infra

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dependabot/cli/internal/credentials"
	"github.com/dependabot/cli/internal/model"
)

// Run on a Docker-capable host with DEPENDABOT_GRAPH_INTEGRATION=1.
func TestGraphPrivateCredentials(t *testing.T) {
	if os.Getenv("DEPENDABOT_GRAPH_INTEGRATION") != "1" {
		t.Skip("requires Docker and the Dependabot proxy image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dir := t.TempDir()
	cert, certPEM := graphTestCertificate(t)
	certPath := filepath.Join(dir, "backend.crt")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	var port string
	var registryRequests, jitRequests, gitRequests, unexpected atomic.Int32
	var denied atomic.Bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/credentials":
			if r.Header.Get("Authorization") != "opaque-credentials-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"data":{"attributes":{"credentials":[
				{"type":"npm_registry","registry":"https://registry.test:%s","token":"registry-secret"},
				{"type":"jit_access","credential-type":"git_source","host":"private.test","endpoint":"/jit"}
			]}}}`, port)
		case "/package":
			if r.Header.Get("Authorization") != "Bearer registry-secret" || denied.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			registryRequests.Add(1)
			fmt.Fprint(w, `{}`)
		case "/org/private.git/info/refs":
			user, password, ok := r.BasicAuth()
			if !ok || user != "x-access-token" || password != "git-secret" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			gitRequests.Add(1)
			fmt.Fprint(w, "private git response")
		case "/jit":
			var request map[string]string
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "opaque-job-token" ||
				json.NewDecoder(r.Body).Decode(&request) != nil || request["account"] != "org" || request["repository"] != "private" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			jitRequests.Add(1)
			fmt.Fprint(w, `{"type":"git_source","host":"private.test","username":"x-access-token","password":"git-secret"}`)
		case "/update_jobs/1/record_metrics":
			if r.Header.Get("Authorization") != "opaque-job-token" {
				unexpected.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
			}
		default:
			unexpected.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	server.Listener.Close()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	_, port, err = net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	client := credentials.Client{HTTP: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}}
	creds, err := client.Fetch(ctx, "https://127.0.0.1:"+port+"/credentials", "opaque-credentials-token")
	if err != nil {
		t.Fatal(err)
	}
	image := fmt.Sprintf("dependabot-graph-credentials-test:%d", os.Getpid())
	build := exec.CommandContext(ctx, "docker", "build", "-q", "-t", image, "testdata/graph-credentials")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := exec.Command("docker", "image", "rm", image).CombinedOutput(); err != nil {
			t.Errorf("remove fixture image: %v\n%s", err, output)
		}
	})
	t.Setenv("JOB_TOKEN", "opaque-job-token")
	t.Setenv("DEPENDABOT_JOB_ID", "1")
	sha := strings.Repeat("a", 40)
	for _, deny := range []bool{false, true} {
		denied.Store(deny)
		var callbacks bytes.Buffer
		outputPath := filepath.Join(dir, fmt.Sprintf("graph-%t.yml", deny))
		err := Run(RunParams{
			Job: &model.Job{Command: model.UpdateGraphCommand, PackageManager: "npm_and_yarn",
				Source: model.Source{Provider: "github", Repo: "owner/repo", Directory: "/", Commit: sha}},
			Creds: creds, CredentialsResolved: true, OmitCredentials: true,
			ProxyApiUrl:   "https://host.docker.internal:" + port,
			ProxyCertPath: certPath, ProxyImage: ProxyImageName, UpdaterImage: image,
			ExtraHosts:                  []string{"registry.test:host-gateway", "private.test:host-gateway"},
			UpdaterEnvironmentVariables: []string{"TEST_PORT=" + port},
			Timeout:                     2 * time.Minute, Output: outputPath, Writer: &callbacks,
		})
		if deny && err == nil {
			t.Fatal("private registry denial must fail the graph run")
		}
		if !deny && err != nil {
			t.Fatal(err)
		}
		data, readErr := os.ReadFile(outputPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, secret := range []string{"registry-secret", "git-secret", "opaque-job-token", "opaque-credentials-token"} {
			if bytes.Contains(data, []byte(secret)) || strings.Contains(callbacks.String(), secret) {
				t.Fatal("capture or callback output exposed a credential")
			}
		}
		if !deny && !bytes.Contains(data, []byte("create_dependency_submission")) {
			t.Fatal("graph callback was not captured locally")
		}
	}
	if registryRequests.Load() != 1 || jitRequests.Load() != 1 || gitRequests.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("registry=%d jit=%d git=%d unexpected backend requests=%d",
			registryRequests.Load(), jitRequests.Load(), gitRequests.Load(), unexpected.Load())
	}
}

func graphTestCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "graph credentials test"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"host.docker.internal", "registry.test", "private.test"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	return cert, certPEM
}
