#!/bin/sh
set -eu
export DEPENDABOT_RECORDED_AT=%d
export RUBYOPT="${RUBYOPT:+$RUBYOPT }-r/opt/dependabot-cli-clock/clock.rb"
export NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }--require=/opt/dependabot-cli-clock/clock.cjs"
exec "$@"
