package infra

import (
	"strings"
	"testing"
)

func TestGenerateCaDetails(t *testing.T) {
	ca, err := GenerateCertificateAuthority()
	if err != nil {
		t.Fatal(err.Error())
	}
	if !strings.Contains(ca.Cert, "BEGIN CERTIFICATE") {
		t.Errorf("Expected certificate to contain BEGIN CERTIFICATE, got %s", ca.Cert)
	}
	if !strings.Contains(ca.Key, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("Expected certificate to contain BEGIN RSA PRIVATE KEY, got %s", ca.Key)
	}
}
