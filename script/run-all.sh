#!/bin/bash

# This script is useful for regenerating all of the smoke tests running locally.

declare -a arr=("actions" "bundler" "cargo" "composer" "docker" "elm" "go" "gradle" "hex" "maven" "npm" "nuget" "pip" "pip-compile" "pipenv" "poetry" "pub" "submodules" "terraform")
for eco in "${arr[@]}"
do
  go run cmd/dependabot/dependabot.go test -f "testdata/smoke-$eco.yaml" -o "testdata/smoke-$eco.yaml"
done
