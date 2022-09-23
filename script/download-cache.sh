#!/bin/bash
if [ $# -eq 0 ]
  then
    echo "First argument is the ecosystem cache name, e.g. bundler"
    exit 1
fi
retry=0
until [ "$retry" -ge 5 ]
do
  gh run download --repo dependabot/smoke-tests --name cache-"$1" --dir cache && exit 0
  retry=$((retry+1))
  sleep 1
done

# Failed to download cache after all retries
exit 1
