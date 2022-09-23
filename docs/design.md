# Design

The CLI is designed to start minimal Dependabot infrastructure on your machine in order to run an update.

At a high level, the CLI:
- Pulls an Updater image and a Proxy image
- Creates networks so the Updater can make calls only through the Proxy 
- Writes the Proxy input file 
- Starts the Proxy
- Writes the job file, which is the input to the Updater
- Starts the Updater image
- Records calls for creating pull requests from the Updater
- Waits for the Updater image to finish
- Cleans everything up

This setup is identical to how [dependabot-action](https://github.com/github/dependabot-action) works, and pretty similar to how our production setup works. So by running with the CLI, we can test essentially what is in production.

## CLI Goals

- Create one way to run Dependabot
  - Makes it easier to test End-to-End
  - Reduce the amount of code the team supports
- Customers can build custom logic downstream from Dependabot CLI by creating adapters
- External users who integrate with other systems can create adapters too

The CLI also opens a lot of doors around extensibility and maintainability of ecosystems.

## Sequence Diagrams

All Updater calls go through the Proxy, I've elided those for brevity.

### E2E tests

Generating tests with a --dry-run:

```mermaid
sequenceDiagram
    CLI->>Proxy: Starts the Proxy
    CLI->>Updater: Starts the Updater
    Updater->>GitHub: Fetch manifests
    loop
    Updater->>Registry: Get version info, etc
    Updater->>CLI: Create/Update PR, etc
    end
    CLI->>YAML file: CLI was given -o so it outputs the calls made
```

Asserting expected behavior, fully cached in the Proxy:

```mermaid
sequenceDiagram
    CLI->>Proxy: Starts the Proxy
    CLI->>Updater: Starts the Updater
    Updater->>Proxy: Fetch manifests (cached)
    loop
    Updater->>Proxy: Get version info, etc (cached)
    Updater->>CLI: Create/Update PR, etc
    end
    CLI->>stderr: CLI outputs pass/fail info, exit code
```

### On Desktop

Future phase

```mermaid
sequenceDiagram
    CLI->>Proxy: Starts the Proxy
    CLI->>Updater: Starts the Updater
    Updater->>GitHub: Fetch manifests
    loop
    Updater->>Registry: Get version info, etc
    Updater->>CLI: Create/Update PR
    CLI->>GH Adapter: CLI forwards API calls to an adapter (TBD)
    GH Adapter->>GitHub: Adds PAT header to Create/Update PR 
    end
```


### In GHES

Future phase

```mermaid
sequenceDiagram
    Workflow->>CLI: Invoke with Job
    CLI->>Proxy: Starts the Proxy
    CLI->>Updater: Starts the Updater
    Updater->>GitHub: Fetch manifests
    loop
    Updater->>Registry: Get version info, etc
    Updater->>CLI: Create/Update PR
    CLI->>GHES Adapter: CLI forwards API calls to an adapter (TBD)
    GHES Adapter->>Dependabot API: Adds auth headers
    end
```
