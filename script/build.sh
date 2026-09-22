#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'The initial desktop build is qualified on macOS only.' >&2
  exit 1
fi
node script/asset-pack.mjs verify
npm run build
BOT_BUNDLE="$BOT_ROOT/dist/Caelis Bot.app"
mkdir -p "$BOT_BUNDLE/Contents/MacOS" "$BOT_BUNDLE/Contents/Resources"
BOT_BASE_VERSION=$(node -p 'JSON.parse(require("node:fs").readFileSync("package.json","utf8")).version')
if [[ ! "$BOT_BASE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Expected a numeric package version.' >&2
  exit 1
fi
BOT_RELEASE_VERSION="$BOT_BASE_VERSION-preview"
CGO_ENABLED=1 go build -tags production -ldflags "-X github.com/caelis-labs/caelis-bot/internal/updates.Version=$BOT_RELEASE_VERSION" -trimpath -o "$BOT_BUNDLE/Contents/MacOS/caelis-bot" .
cp resources/macos/Info.plist "$BOT_BUNDLE/Contents/Info.plist"
cp resources/macos/CaelisBot.icns "$BOT_BUNDLE/Contents/Resources/CaelisBot.icns"
/usr/libexec/PlistBuddy -c "Set CFBundleShortVersionString $BOT_BASE_VERSION" "$BOT_BUNDLE/Contents/Info.plist"
cp LICENSE ASSET-LICENSE.md "$BOT_BUNDLE/Contents/Resources/"
cp resources/character-pack.json "$BOT_BUNDLE/Contents/Resources/character-pack.json"
codesign --force --sign - --identifier dev.caelis.bot "$BOT_BUNDLE"
echo "Built $BOT_BUNDLE"
