#!/usr/bin/env bash
# Sourced by project commands; do not change the user's shell configuration.
BOT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for BOT_NODE_DIR in /opt/homebrew/opt/node@24/bin /usr/local/opt/node@24/bin; do
  if [[ -x "$BOT_NODE_DIR/node" ]]; then
    export PATH="$BOT_NODE_DIR:$PATH"
    break
  fi
done
export GOWORK=off
export GOCACHE="$BOT_ROOT/.cache/go-build"
if [[ "$(uname -s)" == Darwin ]]; then
  # Keep cgo objects, the linker and Info.plist on the same deployment target.
  export MACOSX_DEPLOYMENT_TARGET=12.0
  export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=12.0"
  export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=12.0"
fi
cd "$BOT_ROOT"
