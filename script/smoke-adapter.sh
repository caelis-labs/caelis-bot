#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
go run ./cmd/codex-smoke
