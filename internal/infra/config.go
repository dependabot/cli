package infra

import "github.com/dependabot/cli/internal/model"

// ConfigFilePath is the path to proxy config file.
const ConfigFilePath = "/config.json"

// Config is the structure of the proxy's config file
type Config struct {
	Credentials []model.Credential   `json:"all_credentials"`
	CA          CertificateAuthority `json:"ca"`
}

// CertificateAuthority includes the MITM CA certificate and private key
type CertificateAuthority struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

// BasicAuthCredentials represents credentials required for HTTP basic auth
type BasicAuthCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
