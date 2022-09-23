package infra

import (
	"encoding/json"
	"fmt"
	"os"
)

// ConfigFilePath is the path to proxy config file.
const ConfigFilePath = "/config.json"

// Config is the structure of the proxy's config file
type Config struct {
	Credentials []map[string]string  `json:"all_credentials"`
	CA          CertificateAuthority `json:"ca"`
	ProxyAuth   BasicAuthCredentials `json:"proxy_auth"`
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

// StoreProxyConfig saves the config to a temporary file, returning the path
func StoreProxyConfig(tmpPath string, config *Config) (string, error) {
	tmp, err := os.CreateTemp(TempDir(tmpPath), "config.json")
	if err != nil {
		return "", fmt.Errorf("creating proxy config: %w", err)
	}
	defer tmp.Close()

	if err := json.NewEncoder(tmp).Encode(config); err != nil {
		_ = os.RemoveAll(tmp.Name())
		return "", fmt.Errorf("encoding proxy config: %w", err)
	}
	return tmp.Name(), nil
}
