package infra

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

const (
	keySize        = 2048
	keyExpiryYears = 2
)

var CertSubject = pkix.Name{
	CommonName:         "Dependabot Internal CA",
	OrganizationalUnit: []string{"Dependabot"},
	Organization:       []string{"GitHub Inc."},
	Locality:           []string{"San Francisco"},
	Province:           []string{"California"},
	Country:            []string{"US"},
}

// GenerateCertificateAuthority generates a new proxy keypair CA
func GenerateCertificateAuthority() (CertificateAuthority, error) {
	key, pemKey, err := generateKey()
	if err != nil {
		return CertificateAuthority{}, err
	}

	pemCert, err := generateCert(key)
	if err != nil {
		return CertificateAuthority{}, err
	}

	return CertificateAuthority{
		Cert: pemCert,
		Key:  pemKey,
	}, nil
}

func generateKey() (*rsa.PrivateKey, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, "", err
	}
	kb := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return key, string(pem.EncodeToMemory(kb)), nil
}

func generateCert(key *rsa.PrivateKey) (string, error) {
	notBefore := time.Now()
	notAfter := notBefore.AddDate(keyExpiryYears, 0, 0)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               CertSubject,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		SignatureAlgorithm:    x509.SHA256WithRSA,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		return "", err
	}
	cb := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	}
	return string(pem.EncodeToMemory(cb)), nil
}
