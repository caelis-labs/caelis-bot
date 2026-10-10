#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
go run ./cmd/contract-gen --check
node script/check-public-tree.mjs
node script/check-caelis-protocol.mjs
node script/asset-pack.mjs verify
npm run check:i18n
npm run build
node --test \
  script/ci-scope.test.mjs script/release-version.test.mjs \
  script/update.test.mjs script/publication.test.mjs script/release-channel.test.mjs \
  script/telegram-keychain.test.mjs script/signing.test.mjs script/notarization.test.mjs \
  script/permission-actions.test.mjs script/task-preferences.test.mjs \
  script/runtime-settings.test.mjs script/desktop-world.test.mjs \
  script/content-pack.test.mjs script/asset-pack.test.mjs \
  script/chat-presentation.test.mjs script/chat-observation.test.mjs \
  script/streaming-text.test.mjs script/markdown-chunks.test.mjs
go test -tags private_mac_apis ./...
git diff --check
