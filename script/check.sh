#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
go run ./cmd/contract-gen --check
node script/check-public-tree.mjs
node script/asset-pack.mjs verify
npm run build
node --test script/release-version.test.mjs script/asset-pack.test.mjs script/runtime-assets.test.mjs script/composer-keyboard.test.mjs script/message-content.test.mjs script/character.test.mjs script/gesture-motion.test.mjs script/drag-run.test.mjs script/hand-rig.test.mjs script/view-correctives.test.mjs
go vet ./...
go test ./...
node script/check-portability.mjs
git diff --check
