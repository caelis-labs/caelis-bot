#!/bin/bash
set -euo pipefail
POC_DIR="$(cd "$(dirname "$0")" && pwd)"
POC_ROOT="$(cd "$POC_DIR/../.." && pwd)"
export GOWORK=off
export GOCACHE="$POC_ROOT/.cache/go-build"
export GOMODCACHE="$POC_ROOT/.cache/go-mod"
export GOPROXY=https://proxy.golang.org,direct
cd "$POC_DIR"
case "${1:-protocol}" in
  test) go test -race -count=1 ./... ;;
  replay) go run . -rules examples/rules.json -events examples/events.jsonl ;;
  protocol|terminal|pty)
    go build -o "$POC_ROOT/.cache/eventbot-poc" .
    if [[ "${1:-protocol}" == pty ]]; then
      exec python3 "$POC_DIR/pty_probe.py"
    fi
    if [[ "${1:-protocol}" == terminal ]]; then
      exec "$POC_ROOT/.cache/eventbot-poc" -terminal
    fi
    exec "$POC_ROOT/.cache/eventbot-poc"
    ;;
  native)
    swiftc -module-cache-path "$POC_ROOT/.cache/swift-module-cache" macos-events.swift -o "$POC_ROOT/.cache/eventbot-macos-events"
    exec "$POC_ROOT/.cache/eventbot-macos-events" "${2:-5}"
    ;;
  *) echo "usage: bash poc/eventbot/run.sh test|replay|protocol|terminal|pty|native [seconds]" >&2; exit 2 ;;
esac
