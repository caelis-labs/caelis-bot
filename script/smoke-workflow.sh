#!/usr/bin/env bash
# Explicit real-model/tool acceptance, isolated synthetic inputs only.
set -euo pipefail
source "$(dirname "$0")/env.sh"
for BOT_SCENARIO in files approval interrupt background; do
  go run ./cmd/codex-workflow-smoke -scenario "$BOT_SCENARIO"
done
