package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := readConfig(os.Getenv)
	if err == nil {
		err = run(ctx, cfg, newJobClient(cfg), os.Stdout, execute)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "::error::"+escapeCommand(err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config, api *jobClient, output io.Writer, runner func(context.Context, infra.RunParams) error) error {
	mask := newMasker(output)
	for _, token := range []string{cfg.jobToken, cfg.credentialsToken} {
		if err := mask.add(token); err != nil {
			return err
		}
	}
	job, err := api.details(ctx)
	if err != nil {
		// Without a successful details fetch, the job may still be enqueued.
		return fmt.Errorf("fetch job details: %s", mask.redact(err.Error()))
	}
	creds, err := api.credentials(ctx)
	if err == nil {
		var registryCreds []model.Credential
		registryCreds, err = registryCredentials(os.Getenv("GITHUB_REGISTRIES_PROXY"), output)
		creds = append(creds, registryCreds...)
	}
	if err == nil {
		err = mask.credentials(creds)
	}
	if err == nil {
		job.CredentialsMetadata, err = credentialsMetadata(creds)
	}
	var proxyOptions *infra.ProxyOptions
	if err == nil {
		proxyOptions, err = actionProxyOptions(cfg.apiURL, job.Experiments)
	}
	if err == nil {
		err = os.Setenv("JOB_TOKEN", cfg.jobToken)
	}
	if err == nil {
		err = os.Setenv("DEPENDABOT_JOB_ID", cfg.jobID)
	}
	if err == nil {
		apiURL := cfg.apiDockerURL
		if apiURL == "" {
			apiURL = cfg.apiURL
		}
		err = runner(ctx, infra.RunParams{
			Job: job, Creds: creds, CredentialsResolved: true,
			ApiUrl: apiURL, UpdaterImage: cfg.updaterImage, ProxyImage: cfg.proxyImage,
			ProxyOptions: proxyOptions,
			StorageImage: infra.StorageImageName, PullImages: cfg.pullImages,
			SkipImageVerification: cfg.skipImageVerification,
			Timeout:               cfg.timeout, Volumes: cfg.volumes, UpdaterEnvironmentVariables: cfg.updaterEnv,
		})
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	failure := errors.New(mask.redact(err.Error()))
	reportCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if reportErr := api.reportFailure(reportCtx, failure.Error()); reportErr != nil {
		return errors.Join(failure, fmt.Errorf("report job failure: %s", mask.redact(reportErr.Error())))
	}
	return failure
}

func execute(ctx context.Context, params infra.RunParams) error {
	if len(params.Volumes) > 0 {
		volumes, err := hostVolumes(ctx, params.Volumes)
		if err != nil {
			return err
		}
		params.Volumes = volumes
	}
	return infra.RunContext(ctx, params)
}

type masker struct {
	output  io.Writer
	secrets []string
}

func newMasker(output io.Writer) *masker {
	return &masker{output: output}
}

func (m *masker) add(secret string) error {
	if secret == "" {
		return nil
	}
	for _, existing := range m.secrets {
		if existing == secret {
			return nil
		}
	}
	m.secrets = append(m.secrets, secret)
	if _, err := fmt.Fprintln(m.output, "::add-mask::"+escapeCommand(secret)); err != nil {
		return fmt.Errorf("register credential mask: %w", err)
	}
	return nil
}

func (m *masker) credentials(creds []model.Credential) error {
	for _, cred := range creds {
		for key, value := range cred {
			switch key {
			case "type", "host", "registry", "url", "index-url", "env-key", "organization",
				"replaces-base", "scope", "public-key-fingerprint", "repo", "proxy-only":
				continue
			default:
				if secret, ok := value.(string); ok {
					if err := m.add(secret); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (m *masker) redact(message string) string {
	sort.SliceStable(m.secrets, func(i, j int) bool { return len(m.secrets[i]) > len(m.secrets[j]) })
	for _, secret := range m.secrets {
		message = strings.ReplaceAll(message, secret, "***")
	}
	return message
}

func escapeCommand(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
}
