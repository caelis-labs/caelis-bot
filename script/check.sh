#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
go run ./cmd/contract-gen --check
node script/check-public-tree.mjs
python3 script/test_caelis_runtime.py
python3 script/test_linux_installer.py
node script/check-caelis-protocol.mjs
node script/asset-pack.mjs verify
npm run check:i18n
npm run build
node --test \
  script/ci-scope.test.mjs script/release-version.test.mjs \
  script/release-network.test.mjs script/windows-version.test.mjs script/linux-update-manifest.test.mjs script/linux-release.test.mjs script/windows-update-manifest.test.mjs script/windows-publication.test.mjs \
  script/update.test.mjs script/publication.test.mjs script/release-channel.test.mjs \
  script/telegram-keychain.test.mjs script/signing.test.mjs script/notarization.test.mjs \
  script/setup-stage.test.mjs script/permission-actions.test.mjs script/native-call.test.mjs script/web-transport.test.mjs script/web-csrf.test.mjs script/web-operations.test.mjs script/managed-update-client.test.mjs script/read-recovery.test.mjs script/login-at-login.test.mjs script/task-preferences.test.mjs \
  script/runtime-settings.test.mjs script/connections-loader.test.mjs script/context-navigation.test.mjs script/composer-document.test.mjs script/codex-runtime-panel.test.mjs script/desktop-world.test.mjs \
  script/content-pack.test.mjs script/asset-pack.test.mjs \
  script/question-presentation.test.mjs script/chat-presentation.test.mjs script/chat-observation.test.mjs script/chat-time.test.mjs \
  script/reduced-motion.test.mjs script/streaming-text.test.mjs script/markdown-chunks.test.mjs
go test -tags private_mac_apis ./...
git diff --check
