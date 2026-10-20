#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# No Wails/GTK/display server is linked into the Linux build.
npm run build
BOT_HEADLESS_ARCH="${BOT_HEADLESS_ARCH:-$(go env GOARCH)}"
case "$BOT_HEADLESS_ARCH" in amd64|arm64) ;; *) echo "Unsupported headless architecture" >&2; exit 1 ;; esac
BOT_HEADLESS_DIST="dist/headless/linux-$BOT_HEADLESS_ARCH"
mkdir -p "$BOT_HEADLESS_DIST"
python3 script/caelis-runtime.py stage-headless "$BOT_HEADLESS_DIST" "linux-$BOT_HEADLESS_ARCH"
BOT_VERSION="$(node -p 'JSON.parse(require("fs").readFileSync("package.json","utf8")).version')"
CGO_ENABLED=0 GOOS=linux GOARCH="$BOT_HEADLESS_ARCH" go build -trimpath \
  -ldflags "-X github.com/caelis-labs/caelis-bot/internal/updates.Version=$BOT_VERSION" \
  -o "$BOT_HEADLESS_DIST/caelisbot" .
cp LICENSE "$BOT_HEADLESS_DIST/LICENSE"
cp docs/headless-web.md "$BOT_HEADLESS_DIST/README.md"
tar -czf "dist/headless/caelisbot-${BOT_VERSION}-linux-${BOT_HEADLESS_ARCH}.tar.gz" -C "$BOT_HEADLESS_DIST" .
