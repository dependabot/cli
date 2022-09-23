# Debugging with the CLI

Updater is now merged into core, so we want the CLI to become the preferred way to debug issues in core. This is because the CLI runs the proxy and updater just like how Dependabot runs in production.

To get started, run the dependabot script in core's script directory. This is mostly equivalent to running `bin/dry-run.rb` inside a docker-dev-shell, but without needing to boot a shell in the container first.

If you want to debug like you do with docker-dev-shell, you can pass the `--debug` flag to `script/dependabot`. This will mount core's ecosystems for editing while debugging just like the docker-dev-shell does.

For general debugging of an ecosystem, use the update subcommand, and pass the ecosystem and NWO (name with org) like so:

```console
$ script/dependabot update go_modules rsc/quote --dry-run --debug
```

The CLI will use a Personal Access Token (PAT) in the environment variable `LOCAL_GITHUB_ACCESS_TOKEN` and pass that to the Proxy before starting it. 

It will then generate a `job.json` file with `package_manager` of "go_modules" and a `source` of "rsc/quote".

Unlike the dry-run script and dev-shell, we no longer run the shell while dry-running several repositories. It is recommended you exit the debugging session and start a new one when switching the debugging target. Although you could edit the job.json, the CLI starts very quickly and that's probably not necessary. You may even want to restart after changes to the native helpers.

Once you are in the debugging session, run `bin/run fetch_files` which is the first step the updater takes when it runs. Once complete, run `bin/run update_files` to perform the update.

You might notice the proxy output is not logged in the terminal. To see that you'll have to tail the logs in another terminal or using Docker Desktop. Alternatively you can [fix this issue](https://github.com/dependabot/cli/issues/87), and it will work.

## More complex scenarios

For debugging of more complex situations, you'll want to use a YAML file for input.

View the `testdata` directory for examples of inputs. The input section is all you'll need. So for instance you write a YAML file like this:

```yaml
job:
    package_manager: npm_and_yarn
    allowed_updates:
      - update-type: all
    security_advisories:
      - dependency-name: express
        affected-versions:
          - <5.0.0
        patched-versions: []
        unaffected-versions: []
    security_updates_only: true
    source:
        provider: github
        repo: dependabot/e2e-tests
        directory: /
        commit: 66115359e6f6cc3af6a661c5d5ae803720b98cb8
credentials:
  - type: npm_registry
    registry: https://npm.pkg.github.com
    token: $GPR_TOKEN
```

And then run your debugging session like so:

```console
$ script/dependabot update -f test.yaml --dry-run --debug
```

The CLI will process any `$VARIABLES` in YAML files and replace them with environment variables (for example, `$LOCAL_GITHUB_ACCESS_TOKEN`).

Or if you just want to run a complete update and view the results, run it without the `--debug` flag

```console
$ script/dependabot update -f test.yaml -o output.yaml --dry-run
```

The `output.yaml` file produced shows all the calls the updater will have made during the course of the update. It also happens to be input to an end-to-end test:

```console
$ script/dependabot test -f output.yaml
```
