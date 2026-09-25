#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
go run ./cmd/contract-gen --check
node script/check-public-tree.mjs
node script/check-caelis-protocol.mjs
node script/asset-pack.mjs verify
node --test script/update.test.mjs
npm run check:i18n
npm run build
node --test script/task-preferences.test.mjs script/runtime-settings.test.mjs script/window-lifecycle.test.mjs script/content-pack.test.mjs script/signing.test.mjs script/notarization.test.mjs script/pet-input.test.mjs script/attachment-menu.test.mjs script/release-version.test.mjs script/asset-pack.test.mjs script/avatar.test.mjs script/runtime-assets.test.mjs script/composer-keyboard.test.mjs script/chat-presentation.test.mjs script/chat-scroll.test.mjs script/message-content.test.mjs script/character.test.mjs script/gesture-motion.test.mjs script/drag-run.test.mjs script/hand-rig.test.mjs script/held-plane-contact.test.mjs script/view-correctives.test.mjs
bash script/task-dock-native-test.sh
bash script/care-native-test.sh
go vet -tags private_mac_apis ./...
go test -tags private_mac_apis ./...
node script/check-portability.mjs
git diff --check
