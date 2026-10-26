package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	apiURL                string
	apiDockerURL          string
	jobID                 string
	jobToken              string
	credentialsToken      string
	updaterImage          string
	proxyImage            string
	pullImages            bool
	skipImageVerification bool
	timeout               time.Duration
	volumes               []string
	updaterEnv            []string
}

type eventInputs struct {
	APIURL           string `json:"dependabotApiUrl"`
	APIDockerURL     string `json:"dependabotApiDockerUrl"`
	JobID            string `json:"jobId"`
	JobToken         string `json:"jobToken"`
	CredentialsToken string `json:"credentialsToken"`
	UpdaterImage     string `json:"updaterImage"`
}

func readEventInputs(getenv func(string) string) (eventInputs, error) {
	if getenv("GITHUB_EVENT_NAME") != "dynamic" {
		return eventInputs{}, nil
	}
	path := getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return eventInputs{}, fmt.Errorf("GITHUB_EVENT_PATH is required for Dependabot dynamic events")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return eventInputs{}, fmt.Errorf("cannot read GITHUB_EVENT_PATH")
	}
	var event struct {
		Inputs *eventInputs `json:"inputs"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return eventInputs{}, fmt.Errorf("invalid Dependabot event JSON")
	}
	if event.Inputs == nil {
		return eventInputs{}, fmt.Errorf("Dependabot event must contain inputs")
	}
	return *event.Inputs, nil
}

func readConfig(getenv func(string) string) (config, error) {
	event, err := readEventInputs(getenv)
	if err != nil {
		return config{}, err
	}
	input := func(name string) string { return strings.TrimSpace(getenv("INPUT_" + strings.ToUpper(name))) }
	inputOrEvent := func(name, fallback string) string {
		if value := input(name); value != "" {
			return value
		}
		return strings.TrimSpace(fallback)
	}
	cfg := config{
		apiURL:       strings.TrimRight(inputOrEvent("api-url", event.APIURL), "/"),
		apiDockerURL: strings.TrimRight(inputOrEvent("api-docker-url", event.APIDockerURL), "/"),
		jobID:        inputOrEvent("job-id", event.JobID),
		jobToken:     getenv("GITHUB_DEPENDABOT_JOB_TOKEN"), credentialsToken: getenv("GITHUB_DEPENDABOT_CRED_TOKEN"),
		updaterImage: inputOrEvent("updater-image", event.UpdaterImage), proxyImage: input("proxy-image"),
		pullImages: true, timeout: time.Hour,
		volumes: inputLines(input("volumes")), updaterEnv: inputLines(input("updater-env")),
	}
	if cfg.apiDockerURL == "" {
		cfg.apiDockerURL = cfg.apiURL
	}
	if cfg.jobToken == "" {
		cfg.jobToken = inputOrEvent("job-token", event.JobToken)
	}
	if cfg.credentialsToken == "" {
		cfg.credentialsToken = inputOrEvent("credentials-token", event.CredentialsToken)
	}
	if cfg.jobToken == "" || cfg.credentialsToken == "" {
		return config{}, fmt.Errorf("job-token and credentials-token are required (or their GITHUB_DEPENDABOT_* environment variables or Dependabot dynamic event inputs)")
	}
	if cfg.apiURL == "" {
		return config{}, fmt.Errorf("api-url is required: set the action input or Dependabot event inputs.dependabotApiUrl")
	}
	for _, endpoint := range []struct{ name, value string }{
		{"api-url", cfg.apiURL}, {"api-docker-url", cfg.apiDockerURL},
	} {
		apiURL, err := url.Parse(endpoint.value)
		if err != nil || apiURL.Scheme != "https" || apiURL.Host == "" || apiURL.User != nil || apiURL.RawQuery != "" || apiURL.Fragment != "" {
			return config{}, fmt.Errorf("%s must be an HTTPS URL without credentials, query, or fragment", endpoint.name)
		}
	}
	if cfg.jobID == "" {
		return config{}, fmt.Errorf("job-id is required: set the action input or Dependabot event inputs.jobId")
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
	if value := input("skip-image-verification"); value != "" {
		if value != "true" && value != "false" {
			return config{}, fmt.Errorf("skip-image-verification must be true or false")
		}
		cfg.skipImageVerification = value == "true"
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
