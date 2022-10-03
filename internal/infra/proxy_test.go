package infra

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/namesgenerator"
)

func TestSeed(t *testing.T) {
	// ensure we're still seeding
	a := namesgenerator.GetRandomName(1)
	b := namesgenerator.GetRandomName(1)
	if a == b {
		t.Error("Not seeding math/rand")
	}
}

// This tests the Proxy's ability to use a custom cert for outbound calls.
// It creates a custom proxy image to test with, passes it a cert, and uses it to
// communicate with a test server using the certs.
func TestNewProxy_customCert(t *testing.T) {
	ctx := context.Background()

	CertSubject.CommonName = "host.docker.internal"
	ca, err := GenerateCertificateAuthority()
	if err != nil {
		t.Fatal(err)
	}

	cert, err := os.CreateTemp(os.TempDir(), "cert.pem")
	key, err2 := os.CreateTemp(os.TempDir(), "key.pem")
	if err != nil || err2 != nil {
		t.Fatal(err, err2)
	}
	_, _ = cert.WriteString(ca.Cert)
	_, _ = key.WriteString(ca.Key)
	_ = cert.Close()
	_ = key.Close()

	successChan := make(chan struct{})
	addr := "127.0.0.1:8765"
	if os.Getenv("CI") != "" {
		t.Log("detected running in actions")
		addr = "0.0.0.0:8765"
	}
	testServer := &http.Server{
		ReadHeaderTimeout: time.Second,
		Addr:              addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("SUCCESS"))
			successChan <- struct{}{}
		}),
	}
	defer func() {
		_ = testServer.Shutdown(ctx)
	}()
	go func() {
		t.Log("Starting HTTPS server")
		if err = testServer.ListenAndServeTLS(cert.Name(), key.Name()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}

	// build the test image
	var buildContext bytes.Buffer
	tw := tar.NewWriter(&buildContext)
	addFileToArchive(tw, "/Dockerfile", 0644, proxyTestDockerfile)
	_ = tw.Close()

	tmp := ProxyImageName
	defer func() {
		ProxyImageName = tmp
	}()
	ProxyImageName = "curl-test"
	resp, err := cli.ImageBuild(ctx, &buildContext, types.ImageBuildOptions{Tags: []string{ProxyImageName}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	defer func() {
		_, _ = cli.ImageRemove(ctx, ProxyImageName, types.ImageRemoveOptions{})
	}()

	proxy, err := NewProxy(ctx, cli, &RunParams{
		ProxyCertPath: cert.Name(),
	})
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = proxy.Close()
	}()

	t.Log("Starting proxy")

	go proxy.TailLogs(ctx, cli)

	select {
	case <-successChan:
		t.Log("Success!")
	case <-time.After(5 * time.Second):
		t.Errorf("Not able to contact the test server")
	}
}

const proxyTestDockerfile = `
FROM ghcr.io/github/dependabot-update-job-proxy/dependabot-update-job-proxy:latest
RUN apk add --no-cache curl
RUN echo "#!/bin/sh" > /update-job-proxy
RUN echo "CURLing host.docker.internal" >> /update-job-proxy
RUN echo "curl -s https://host.docker.internal:8765" >> /update-job-proxy
RUN chmod +x /update-job-proxy
`
