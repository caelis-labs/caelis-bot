#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
: "${WORKER_PEER_PLAYWRIGHT_MODULE:?Set an existing Playwright module; no install/download}"
: "${CAELIS_BOT_TEST_CODEX:?Set the real installed Codex binary}"
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
mkdir -p .cache/worker-peer-e2e/bin
npm run build
CGO_ENABLED=0 go build -trimpath -ldflags '-X github.com/caelis-labs/caelis-bot/internal/updates.Version=ISOLATED-worker-peer-real-url' -o .cache/worker-peer-e2e/bin/real-url-bot .
exec node script/e2e-worker-peer-real-url.mjs "$@"
