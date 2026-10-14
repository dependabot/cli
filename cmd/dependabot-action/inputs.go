package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type config struct {
	apiURL           string
	jobID            string
	jobToken         string
	credentialsToken string
	updaterImage     string
	proxyImage       string
	pullImages       bool
	timeout          time.Duration
	volumes          []string
	updaterEnv       []string
}

func readConfig(getenv func(string) string) (config, error) {
	input := func(name string) string { return strings.TrimSpace(getenv("INPUT_" + strings.ToUpper(name))) }
	cfg := config{
		apiURL: strings.TrimRight(input("api-url"), "/"), jobID: input("job-id"),
		jobToken: getenv("GITHUB_DEPENDABOT_JOB_TOKEN"), credentialsToken: getenv("GITHUB_DEPENDABOT_CRED_TOKEN"),
		updaterImage: input("updater-image"), proxyImage: input("proxy-image"),
		pullImages: true, timeout: time.Hour,
		volumes: inputLines(input("volumes")), updaterEnv: inputLines(input("updater-env")),
	}
	if cfg.jobToken == "" {
		cfg.jobToken = input("job-token")
	}
	if cfg.credentialsToken == "" {
		cfg.credentialsToken = input("credentials-token")
	}
	if cfg.jobToken == "" || cfg.credentialsToken == "" {
		return config{}, fmt.Errorf("job-token and credentials-token are required (or their GITHUB_DEPENDABOT_* environment variables)")
	}
	apiURL, err := url.Parse(cfg.apiURL)
	if err != nil || apiURL.Scheme != "https" || apiURL.Host == "" || apiURL.User != nil || apiURL.RawQuery != "" || apiURL.Fragment != "" {
		return config{}, fmt.Errorf("api-url must be an HTTPS URL without credentials, query, or fragment")
	}
	if id, err := strconv.ParseUint(cfg.jobID, 10, 64); err != nil || id == 0 {
		return config{}, fmt.Errorf("job-id must be a positive integer")
	}
	if value := input("pull-images"); value != "" {
		if value != "true" && value != "false" {
			return config{}, fmt.Errorf("pull-images must be true or false")
		}
		cfg.pullImages = value == "true"
	}
	if value := input("timeout"); value != "" {
		cfg.timeout, err = time.ParseDuration(value)
		if err != nil || cfg.timeout <= 0 {
			return config{}, fmt.Errorf("timeout must be a positive Go duration, for example 60m")
		}
	}
	for _, entry := range cfg.updaterEnv {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" || strings.ContainsAny(name, " \t\r") {
			return config{}, fmt.Errorf("updater-env must contain one NAME=value per line")
		}
	}
	return cfg, nil
}

func inputLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
