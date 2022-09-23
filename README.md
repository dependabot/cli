# Dependabot CLI

A CLI that pulls in the [updater] and [proxy] containers and runs them.

## Installation

You can download a pre-built binary for your system from the [Releases] page.

If you have the [`gh` CLI][gh] installed,
you can install the latest release of `dependabot` using the following command
([gist source](https://gist.github.com/mattt/e09e1ecd76d5573e0517a7622009f06f)): 

```console
$ gh gist view --raw e09e1ecd76d5573e0517a7622009f06f | bash
```

## Requirements

* Docker

For development:

* Go 1.18+

## Usage

```console
$ dependabot update go_modules rsc/quote --dry-run
```

<details>
<summary>Output</summary>

```
time="2022-07-25T11:35:05Z" level=info msg="proxy starting" commit=8bc7edd876c7b566c70dcf22daa1c039912767f9
time="2022-07-25T11:35:05Z" level=warning msg="initializing metrics client" error="No address passed and autodetection from environment failed"
2022/07/25 11:35:05 Listening (:1080)
I, [2022-07-25T11:35:06.684817 #9]  INFO -- sentry: ** [Raven] Raven 3.1.2 configured not to capture errors: DSN not set
INFO <job_cli> Starting job processing
2022/07/25 11:35:28 [002] GET https://api.github.com:443/repos/rsc/quote
2022/07/25 11:35:28 [002] * authenticating github api request
2022/07/25 11:35:28 [002] 200 https://api.github.com:443/repos/rsc/quote
2022/07/25 11:35:28 [004] GET https://api.github.com:443/repos/rsc/quote/git/refs/heads/master
2022/07/25 11:35:28 [004] * authenticating github api request
2022/07/25 11:35:28 [004] 200 https://api.github.com:443/repos/rsc/quote/git/refs/heads/master
2022/07/25 11:35:29 [006] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:29 [006] * authenticating git server request (host: github.com)
2022/07/25 11:35:29 [006] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:29 [008] POST https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:29 [008] * authenticating git server request (host: github.com)
2022/07/25 11:35:29 [008] 200 https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:29 [010] POST https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:29 [010] * authenticating git server request (host: github.com)
2022/07/25 11:35:29 [010] 200 https://github.com:443/rsc/quote/git-upload-pack
INFO <job_cli> Finished job processing
I, [2022-07-25T11:35:30.005259 #30]  INFO -- sentry: ** [Raven] Raven 3.1.2 configured not to capture errors: DSN not set
INFO <job_cli> Starting job processing
INFO <job_cli> Starting update job for rsc/quote
2022/07/25 11:35:51 [011] POST http://host.docker.internal:8080/update_jobs/cli/update_dependency_list
2022/07/25 11:35:51 [011] 200 http://host.docker.internal:8080/update_jobs/cli/update_dependency_list
INFO <job_cli> Checking if rsc.io/quote/v3 3.0.0 needs updating
2022/07/25 11:35:51 [013] GET https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:35:51 [013] 200 https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:35:51 [015] GET https://rsc.io:443/quote?go-get=1
2022/07/25 11:35:51 [015] 200 https://rsc.io:443/quote?go-get=1
2022/07/25 11:35:51 [017] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:51 [017] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [017] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [019] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [019] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [019] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [021] POST https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:52 [021] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [021] 200 https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:52 [023] POST https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:52 [023] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [023] 200 https://github.com:443/rsc/quote/git-upload-pack
INFO <job_cli> Latest version is 3.1.0
INFO <job_cli> Requirements to unlock own
INFO <job_cli> Requirements update strategy 
INFO <job_cli> Updating rsc.io/quote/v3 from 3.0.0 to 3.1.0
2022/07/25 11:35:52 [026] GET https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:35:52 [027] GET https://rsc.io:443/sampler?go-get=1
2022/07/25 11:35:52 [026] 200 https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:35:52 [029] GET https://rsc.io:443/quote?go-get=1
2022/07/25 11:35:52 [027] 200 https://rsc.io:443/sampler?go-get=1
2022/07/25 11:35:52 [029] 200 https://rsc.io:443/quote?go-get=1
2022/07/25 11:35:52 [032] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [032] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [033] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [033] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [032] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [033] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [035] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [035] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [037] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [037] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [035] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [037] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:52 [039] POST https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:52 [039] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [041] POST https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:52 [041] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [039] 200 https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:52 [043] POST https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:52 [043] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [041] 200 https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:52 [045] POST https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:52 [045] * authenticating git server request (host: github.com)
2022/07/25 11:35:52 [043] 200 https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:52 [045] 200 https://github.com:443/rsc/quote/git-upload-pack
2022/07/25 11:35:53 [047] GET https://golang.org:443/x/text?go-get=1
2022/07/25 11:35:53 [047] 200 https://golang.org:443/x/text?go-get=1
2022/07/25 11:35:53 [049] GET https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:35:53 [049] 200 https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:35:53 [051] GET https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:35:53 [051] 200 https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:35:53 [053] POST https://go.googlesource.com:443/text/git-upload-pack
2022/07/25 11:35:53 [053] 200 https://go.googlesource.com:443/text/git-upload-pack
2022/07/25 11:35:57 [055] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:57 [055] * authenticating git server request (host: github.com)
2022/07/25 11:35:57 [055] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:57 [057] POST https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:57 [057] * authenticating git server request (host: github.com)
2022/07/25 11:35:57 [057] 200 https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:57 [059] POST https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:57 [059] * authenticating git server request (host: github.com)
2022/07/25 11:35:57 [059] 200 https://github.com:443/rsc/sampler/git-upload-pack
2022/07/25 11:35:57 [062] GET https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:35:57 [063] GET https://golang.org:443/x/text?go-get=1
2022/07/25 11:35:57 [062] 200 https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:35:57 [065] GET https://rsc.io:443/quote?go-get=1
2022/07/25 11:35:57 [065] 200 https://rsc.io:443/quote?go-get=1
2022/07/25 11:35:57 [063] 200 https://golang.org:443/x/text?go-get=1
2022/07/25 11:35:58 [068] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:58 [068] * authenticating git server request (host: github.com)
2022/07/25 11:35:58 [069] GET https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:35:58 [069] 200 https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:35:58 [068] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:35:58 [071] GET https://rsc.io:443/sampler?go-get=1
2022/07/25 11:35:58 [071] 200 https://rsc.io:443/sampler?go-get=1
2022/07/25 11:35:58 [073] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:35:58 [073] * authenticating git server request (host: github.com)
2022/07/25 11:35:58 [073] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
INFO <job_cli> Submitting rsc.io/quote/v3 pull request for creation
2022/07/25 11:36:18 [074] POST http://host.docker.internal:8080/update_jobs/cli/create_pull_request
2022/07/25 11:36:18 [074] 200 http://host.docker.internal:8080/update_jobs/cli/create_pull_request
INFO <job_cli> Checking if rsc.io/sampler 1.3.0 needs updating
2022/07/25 11:36:18 [076] GET https://rsc.io:443/sampler?go-get=1
2022/07/25 11:36:18 [076] 200 https://rsc.io:443/sampler?go-get=1
2022/07/25 11:36:18 [078] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [078] * authenticating git server request (host: github.com)
2022/07/25 11:36:18 [078] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
INFO <job_cli> Latest version is 1.99.99
INFO <job_cli> Requirements to unlock own
INFO <job_cli> Requirements update strategy 
INFO <job_cli> Updating rsc.io/sampler from 1.3.0 to 1.99.99
2022/07/25 11:36:18 [080] GET https://rsc.io:443/sampler?go-get=1
2022/07/25 11:36:18 [080] 200 https://rsc.io:443/sampler?go-get=1
2022/07/25 11:36:18 [082] GET https://golang.org:443/x/text?go-get=1
2022/07/25 11:36:18 [084] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [084] * authenticating git server request (host: github.com)
2022/07/25 11:36:18 [082] 200 https://golang.org:443/x/text?go-get=1
2022/07/25 11:36:18 [086] GET https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [084] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [086] 200 https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [088] GET https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:36:18 [088] 200 https://rsc.io:443/quote/v3?go-get=1
2022/07/25 11:36:18 [090] GET https://rsc.io:443/quote?go-get=1
2022/07/25 11:36:18 [090] 200 https://rsc.io:443/quote?go-get=1
2022/07/25 11:36:18 [092] GET https://golang.org:443/x/text?go-get=1
2022/07/25 11:36:18 [094] GET https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [094] * authenticating git server request (host: github.com)
2022/07/25 11:36:18 [092] 200 https://golang.org:443/x/text?go-get=1
2022/07/25 11:36:18 [096] GET https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:36:18 [094] 200 https://github.com:443/rsc/quote/info/refs?service=git-upload-pack
2022/07/25 11:36:19 [098] GET https://rsc.io:443/sampler?go-get=1
2022/07/25 11:36:19 [096] 200 https://go.googlesource.com:443/text/info/refs?service=git-upload-pack
2022/07/25 11:36:19 [098] 200 https://rsc.io:443/sampler?go-get=1
2022/07/25 11:36:19 [100] GET https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
2022/07/25 11:36:19 [100] * authenticating git server request (host: github.com)
2022/07/25 11:36:19 [100] 200 https://github.com:443/rsc/sampler/info/refs?service=git-upload-pack
INFO <job_cli> Submitting rsc.io/sampler pull request for creation
2022/07/25 11:36:39 [101] POST http://host.docker.internal:8080/update_jobs/cli/create_pull_request
2022/07/25 11:36:39 [101] 200 http://host.docker.internal:8080/update_jobs/cli/create_pull_request
2022/07/25 11:36:59 [102] PATCH http://host.docker.internal:8080/update_jobs/cli/mark_as_processed
2022/07/25 11:36:59 [102] 200 http://host.docker.internal:8080/update_jobs/cli/mark_as_processed
INFO <job_cli> Finished job processing
INFO Results:
+----------------------------------------------------+
|        Changes to Dependabot Pull Requests         |
+---------+------------------------------------------+
| created | rsc.io/quote/v3 ( from 3.0.0 to 3.1.0 )  |
| created | rsc.io/sampler ( from 1.3.0 to 1.99.99 ) |
+---------+------------------------------------------+
```

</details>

## Troubleshooting

### Docker daemon not running

```
failed to pull ghcr.io/github/dependabot-update-job-proxy/dependabot-update-job-proxy:latest: 
Error response from daemon: dial unix docker.raw.sock: connect: no such file or directory
```

The CLI requires Docker to be running on your machine.
Follow the instructions on [Docker's website](https://docs.docker.com/get-started/)
to get the latest version of Docker installed and running.

You can verify that Docker is running locally with the following command:

```console
$ docker --version
```

### Network internet is ambiguous

```
failed to start container: Error response from daemon: network internet is ambiguous (2 matches found on name)
```

This error can occur when the CLI exits before getting to clean up
(e.g. terminating with <kbd>^</kbd><kbd>C</kbd>).
Run the following command to remove all unused networks:

```console
$ docker network prune
```

[updater]: https://github.com/dependabot/dependabot-core/pkgs/container/dependabot-updater
[proxy]: https://github.com/github/dependabot-update-job-proxy/pkgs/container/dependabot-update-job-proxy%2Fdependabot-update-job-proxy
[gh]: https://github.com/cli/cli
[Releases]: https://github.com/dependabot/cli/releases
