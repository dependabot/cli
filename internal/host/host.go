package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/dependabot/cli/internal/infra"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	target, _ := url.Parse("http://127.0.0.1")

	ca, err := infra.GenerateCertificateAuthority()
	if err != nil {
		log.Fatal(err)
	}

	cert, err := os.Create("cert.pem")
	if err != nil {
		log.Fatal(err)
	}
	cert.WriteString(ca.Cert)
	cert.Close()

	key, err := os.Create("key.pem")
	if err != nil {
		log.Fatal(err)
	}
	key.WriteString(ca.Key)
	key.Close()

	reverseProxy := httputil.NewSingleHostReverseProxy(target)

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM([]byte(ca.Cert))

	server := &http.Server{
		Addr:    ":443",
		Handler: reverseProxy,
		TLSConfig: &tls.Config{
			RootCAs:            certPool,
			InsecureSkipVerify: true,
		},
	}

	defer os.Remove(cert.Name())
	defer os.Remove(key.Name())

	if err := server.ListenAndServeTLS(cert.Name(), key.Name()); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

}
