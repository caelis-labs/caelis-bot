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
BOT_VERSION_JSON=$(node script/release-version.mjs)
BOT_BASE_VERSION=$(node -e 'console.log(JSON.parse(process.argv[1]).bundleVersion)' "$BOT_VERSION_JSON")
BOT_RELEASE_VERSION=$(node -e 'console.log(JSON.parse(process.argv[1]).version)' "$BOT_VERSION_JSON")
BOT_SOURCE_COMMIT=$(git rev-parse HEAD)
# Wails beta.23 otherwise leaves WKWebView opaque above transparent native windows.
# This opts into Wails' guarded drawsBackground bridge for the pet/prop/materials.
CGO_ENABLED=1 go build -tags production,private_mac_apis -ldflags "-X github.com/caelis-labs/caelis-bot/internal/updates.Version=$BOT_RELEASE_VERSION" -trimpath -o "$BOT_BUNDLE/Contents/MacOS/caelis-bot" .
cp resources/macos/Info.plist "$BOT_BUNDLE/Contents/Info.plist"
cp resources/macos/CaelisBot.icns "$BOT_BUNDLE/Contents/Resources/CaelisBot.icns"
/usr/libexec/PlistBuddy -c "Set CFBundleShortVersionString $BOT_BASE_VERSION" "$BOT_BUNDLE/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add CaelisReleaseVersion string $BOT_RELEASE_VERSION" "$BOT_BUNDLE/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add CaelisSourceCommit string $BOT_SOURCE_COMMIT" "$BOT_BUNDLE/Contents/Info.plist"
cp protocol/caelis/LICENSE "$BOT_BUNDLE/Contents/Resources/Caelis-Protocol-LICENSE"
cp LICENSE ASSET-LICENSE.md "$BOT_BUNDLE/Contents/Resources/"
cp resources/character-pack.json "$BOT_BUNDLE/Contents/Resources/character-pack.json"
codesign --force --sign - --identifier dev.caelis.bot "$BOT_BUNDLE"
echo "Built $BOT_BUNDLE"
