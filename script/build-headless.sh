#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# No Wails/GTK/display server is linked into the Linux build.
npm run build
mkdir -p dist/headless
BOT_VERSION="$(node -p 'JSON.parse(require("fs").readFileSync("package.json","utf8")).version')"
CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags "-X github.com/caelis-labs/caelis-bot/internal/updates.Version=$BOT_VERSION" \
  -o dist/headless/caelisbot .
