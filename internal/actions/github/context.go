package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type ActionsContext struct {
	Actor     string
	EventName string
	Payload   WorkflowDispatchEvent
}

type WorkflowDispatchEvent struct {
	Inputs JobParameters `json:"inputs"`
}

type JobParameters struct {
	JobID                  string `json:"jobId"`
	JobToken               string `json:"jobToken"`
	CredentialsToken       string `json:"credentialsToken"`
	DependabotAPIURL       string `json:"dependabotApiUrl"`
	DependabotAPIDockerURL string `json:"dependabotApiDockerUrl"`
	UpdaterImage           string `json:"updaterImage"`
	WorkingDirectory       string `json:"workingDirectory"`
}

func Context() (*ActionsContext, error) {
	context := &ActionsContext{
		Actor:     os.Getenv("GITHUB_ACTOR"),
		EventName: os.Getenv("GITHUB_EVENT_NAME"),
	}
	if eventPath := os.Getenv("GITHUB_EVENT_PATH"); eventPath != "" {
		f, err := os.Open(eventPath)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "GITHUB_EVENT_PATH %s does not exist\n", eventPath)
			return context, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to open event file: %w", err)
		}
		if err = json.NewDecoder(f).Decode(&context.Payload); err != nil {
			return nil, fmt.Errorf("failed to decode event file: %w", err)
		}
	}
	return context, nil
}
