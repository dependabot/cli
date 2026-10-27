package main

import (
	"fmt"
	"os"

	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
)

func actionProxyOptions(apiURL string, experiments model.Experiment) (*infra.ProxyOptions, error) {
	proxyEnv := func(lower, upper string) string {
		if value := os.Getenv(lower); value != "" {
			return value
		}
		return os.Getenv(upper)
	}
	options := &infra.ProxyOptions{
		APIURL: apiURL, Experiments: experiments, WaitForReady: true,
		HTTPProxy: proxyEnv("http_proxy", "HTTP_PROXY"), HTTPSProxy: proxyEnv("https_proxy", "HTTPS_PROXY"),
		NoProxy: proxyEnv("no_proxy", "NO_PROXY"),
	}
	certPath, ok := os.LookupEnv("CUSTOM_CA_PATH")
	if !ok {
		certPath = os.Getenv("NODE_EXTRA_CA_CERTS")
	}
	if certPath != "" {
		var err error
		options.CACert, err = os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("cannot read custom proxy CA certificate")
		}
	}
	return options, nil
}
