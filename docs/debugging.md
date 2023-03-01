# CLI Debugging Guide

This guide will help you debug issues with your Dependabot update using the Dependabot CLI.

## Getting started

First, test to make sure you have a working Dependabot CLI by performing a simple update, like `dependabot update --dry-run go_modules rsc/quote -o out.yml`. This should complete without error, and you can examine the out.yml file which should contain two calls to `create_pull_request`.

Next, clone https://github.com/dependabot/dependabot-core. This project contains all the source for the updater images, and a helpful script `script/dependabot` which will mount the ecosystems in the container that the CLI starts.

Try opening a terminal and run `script/dependabot update --dry-run go_modules rsc/quote --debug` in the `dependabot-core` project directory. This will drop you in an interactive session with the update ready to proceed.

To perform the update, you need to run two commands:

- `bin/run fetch_files`
- `bin/run update_files`

If the problem you are debugging is during the fetch step, the fetch_files command will be all you need to run.

If the problem is after the fetch step, you can repeatedly run update_files while you're debugging. 

In the example `script/dependabot` command above, try running an update in the container. 

Next, let's try adding a `debugger` statement. Open the `dependabot-core` project in your favorite editor. Open the file `go_modules/lib/dependabot/go_modules/update_checker.rb`. In `latest_resolvable_version` add a `debugger` statement like this:

```ruby
      def latest_resolvable_version
        debugger
        latest_version_finder.latest_version
      end
```

> **Note** You don't have to restart your CLI session, the changes are automatically synced to the container!

In the interactive debugging session, run `bin/run fetch_files` and `bin/run update_files`. During the update_files command, the Ruby debugger will open. It should look something like this:

```ruby
[11, 20] in ~/go_modules/lib/dependabot/go_modules/update_checker.rb
    11|   module GoModules
    12|     class UpdateChecker < Dependabot::UpdateCheckers::Base
    13|       require_relative "update_checker/latest_version_finder"
    14| 
    15|       def latest_resolvable_version
=>  16|         debugger
    17|         latest_version_finder.latest_version
    18|       end
    19| 
    20|       # This is currently used to short-circuit latest_resolvable_version,
=>#0    Dependabot::GoModules::UpdateChecker#latest_resolvable_version at ~/go_modules/lib/dependabot/go_modules/update_checker.rb:16
  #1    Dependabot::GoModules::UpdateChecker#latest_version at ~/go_modules/lib/dependabot/go_modules/update_checker.rb:24
  # and 9 frames (use `bt' command for all frames)
(rdbg) 
```

At this prompt, you can run [debugger commands](https://github.com/ruby/debug) to navigate around, or enter methods and variables to see what they contain. Try entering `dependency` to see what dependency Dependabot is currently working on.

>**Note** While in the debugger, changes made to the source code will not be picked up. You will have to end your debugging session and restart it.

## Debugging a hang

If your Dependabot job is hanging and would like to figure out why, the CLI is the perfect tool for the job. 

Start by running the update that recreates the hang with `dependabot update --dry-run <ecosystem> <org/repo>`. Once the hang is reproducible, run with the `--debug` flag and the run the `fetch_files` and `update_files` commands and wait until the job hangs.

Once it does hang, hit CTL-C, and you'll get a stack trace leading you to the problematic code.

>**Note** Under debug mode, the Proxy output won't be shown in the terminal. Use Docker Desktop or another method to view the Proxy logs to tell when it starts to hang.
