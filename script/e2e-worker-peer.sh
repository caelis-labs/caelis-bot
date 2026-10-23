#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# Opt-in application acceptance. No package/browser/runtime installation.
: "${WORKER_PEER_PLAYWRIGHT_MODULE:?Set an existing Playwright module path (index.mjs), or use a locally installed module}"
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
mkdir -p .cache/worker-peer-e2e/bin
npm run build
CGO_ENABLED=0 go build -trimpath -ldflags '-X github.com/caelis-labs/caelis-bot/internal/updates.Version=FIXTURE-worker-peer-e2e' -o .cache/worker-peer-e2e/bin/bot .
go build -trimpath -tags workerpeere2e -o .cache/worker-peer-e2e/bin/native ./cmd/worker-peer-native-fixture
exec node script/e2e-worker-peer.mjs "$@"
