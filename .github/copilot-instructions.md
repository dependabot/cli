# Dependabot CLI — Copilot Instructions

## Architecture Overview

This is the **Dependabot CLI** (`dependabot`), a Go tool that orchestrates Dependabot update jobs via Docker containers. It does **not** perform dependency resolution itself — it coordinates three containers:

1. **Proxy** (`ghcr.io/dependabot/proxy`) — intercepts all updater network traffic, injects credentials without exposing them to the updater, and optionally caches requests.
2. **Updater** (`ghcr.io/dependabot/dependabot-updater-<ecosystem>`) — runs the actual dependency update logic (from [dependabot-core](https://github.com/dependabot/dependabot-core)).
3. **Fake API server** (`internal/server/api.go`) — a local HTTP server that captures updater API calls (create PR, close PR, etc.) for output or test assertion.

Data flow: CLI parses input → starts proxy + updater containers on isolated Docker networks → updater calls back to the fake API → CLI collects results as YAML.

## Project Layout

- `cmd/dependabot/` — entrypoint (`main`), delegates to `internal/cmd/`
- `cmd/dependabot/internal/cmd/` — Cobra commands: `update`, `test`, `graph`, `version`, plus `root.go` for shared flags
- `internal/infra/` — Docker container orchestration: `run.go` (main flow), `updater.go`, `proxy.go`, `network.go`, `config.go`
- `internal/model/` — data types for jobs, credentials, smoke tests, API payloads. Shared across all packages.
- `internal/server/` — fake API server (`api.go`) and secure input server (`input.go`)
- `testdata/` — YAML fixtures and `scripts/*.txt` for scripttest-based integration tests

## Key Conventions

### YAML/JSON Model Tags

Models use **kebab-case** YAML tags (`yaml:"package-manager"`) and matching JSON tags. When adding new fields:

- Add `omitempty` initially to maintain backward compatibility with existing smoke tests
- See the comment block at the top of `internal/model/job.go` for the full add/remove lifecycle

### Command Pattern

Each subcommand (`update`, `test`, `graph`) follows the same pattern:

- Define a `NewXCommand() *cobra.Command` constructor
- Call `infra.Run(infra.RunParams{...})` with appropriate parameters
- Register via `init()` with `rootCmd.AddCommand()`
- The `update` and `graph` commands share `extractInput()` and `processInput()` from `update.go`

### Credentials Handling

- `$`-prefixed values in YAML input are expanded from environment variables at runtime
- `LOCAL_GITHUB_ACCESS_TOKEN` and `LOCAL_AZURE_ACCESS_TOKEN` are auto-injected into credentials when set
- Credentials are **never** passed directly to the updater; they go through the proxy which injects them into outbound requests
- `checkCredAccess()` in `run.go` blocks tokens with write access to GitHub API for security

### Container Networking

Two Docker bridge networks are created per run (`network.go`):

- **no-internet** (internal) — updater can only reach the proxy
- **internet** — proxy can reach external services

The updater is connected only to no-internet; the proxy bridges both.

## Build & Test

```bash
# Build
go build ./cmd/dependabot

# Run all unit tests
go test ./...

# Run script-based integration tests (require Docker)
go test ./cmd/dependabot/ -count=1

# Run a specific script test by name pattern
script/e2e <pattern>

# Install from source
go install github.com/dependabot/cli/cmd/dependabot@latest
```

### Script Tests (`testdata/scripts/*.txt`)

These use Go's `rsc.io/script` framework (see `cmd/dependabot/dependabot_test.go`). Each `.txt` file:

- Builds a dummy Docker image inline (via `-- Dockerfile --` sections)
- Runs `dependabot` commands and asserts on stdout/stderr
- Uses `!` prefix for expected-failure commands

### Test Mocking Pattern

The `test` command uses a package-level `var executeTestJob = infra.Run` that tests override to capture `RunParams` without running Docker (see `test_test.go`).

## Smoke Tests

Smoke tests (`model.SmokeTest`) define **input + expected output** for reproducible update jobs:

- `input:` — job definition + credentials
- `output:` — array of expected API calls (`create_pull_request`, `update_dependency_list`, etc.)
- Generate with: `dependabot update <pm> <repo> -o smoke-test.yml`
- Run with: `dependabot test -f smoke-test.yml --cache ./tmp/cache`
- The test runner auto-generates `ignore-conditions` to pin dependency versions for reproducibility

## Package Manager Ecosystem Mapping

The `packageManagerLookup` map in `internal/infra/run.go` maps package manager names (e.g., `go_modules`) to updater image suffixes (e.g., `gomod`). When adding ecosystem support, update this map.
