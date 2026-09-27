#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'The initial desktop build is qualified on macOS only.' >&2
  exit 1
fi
source "$BOT_ROOT/script/development-signing.sh"
node script/asset-pack.mjs verify
npm run build
BOT_BUNDLE="$BOT_ROOT/dist/Caelis Bot.app"
mkdir -p "$BOT_BUNDLE/Contents/MacOS" "$BOT_BUNDLE/Contents/Resources"
BOT_VERSION_JSON=$(node script/release-version.mjs)
BOT_BASE_VERSION=$(node -e 'console.log(JSON.parse(process.argv[1]).bundleVersion)' "$BOT_VERSION_JSON")
BOT_RELEASE_VERSION=$(node -e 'console.log(JSON.parse(process.argv[1]).version)' "$BOT_VERSION_JSON")
node -e 'import("./script/configure-updates.mjs").then(m=>m.validateUpdateKey(process.env.BOT_SPARKLE_PUBLIC_KEY,Boolean(process.env.BOT_RELEASE_TAG)))'
source script/sparkle.sh
trap 'rm -rf "$BOT_SPARKLE_DIR"' EXIT
BOT_SOURCE_COMMIT=$(git rev-parse HEAD)
# Wails beta.23 otherwise leaves WKWebView opaque above transparent native windows.
# This opts into Wails' guarded drawsBackground bridge for the pet/prop/materials.
CGO_ENABLED=1 go build -tags production,private_mac_apis -ldflags "-X github.com/caelis-labs/caelis-bot/internal/updates.Version=$BOT_RELEASE_VERSION" -trimpath -o "$BOT_BUNDLE/Contents/MacOS/caelis-bot" .
cp resources/macos/Info.plist "$BOT_BUNDLE/Contents/Info.plist"
cp resources/macos/CaelisBot.icns "$BOT_BUNDLE/Contents/Resources/CaelisBot.icns"
# Fail before signing if the notification/Finder icon cannot be decoded.
sips -g pixelWidth -g pixelHeight "$BOT_BUNDLE/Contents/Resources/CaelisBot.icns" >/dev/null
/usr/libexec/PlistBuddy -c "Set CFBundleShortVersionString $BOT_BASE_VERSION" "$BOT_BUNDLE/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set CFBundleVersion $BOT_BASE_VERSION" "$BOT_BUNDLE/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add CaelisReleaseVersion string $BOT_RELEASE_VERSION" "$BOT_BUNDLE/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add CaelisSourceCommit string $BOT_SOURCE_COMMIT" "$BOT_BUNDLE/Contents/Info.plist"
cp protocol/caelis/LICENSE "$BOT_BUNDLE/Contents/Resources/Caelis-Protocol-LICENSE"
cp LICENSE ASSET-LICENSE.md "$BOT_BUNDLE/Contents/Resources/"
cp resources/character-pack.json "$BOT_BUNDLE/Contents/Resources/character-pack.json"
mkdir -p "$BOT_BUNDLE/Contents/Frameworks"
rm -rf "$BOT_BUNDLE/Contents/Frameworks/Sparkle.framework"
ditto "$BOT_SPARKLE_DIR/Sparkle.framework" "$BOT_BUNDLE/Contents/Frameworks/Sparkle.framework"
cp "$BOT_SPARKLE_DIR/LICENSE" "$BOT_BUNDLE/Contents/Resources/Sparkle-LICENSE"
node script/configure-updates.mjs "$BOT_BUNDLE/Contents/Info.plist"
bash script/sign-sparkle.sh "$BOT_BUNDLE" "$BOT_BUILD_SIGN_IDENTITY" "$BOT_BUILD_SIGN_MODE"
BOT_BUILD_SIGN_ARGS=(--force --sign "$BOT_BUILD_SIGN_IDENTITY" --identifier dev.caelis.bot --entitlements "$BOT_ROOT/resources/macos/entitlements.plist")
if [[ "$BOT_BUILD_SIGN_MODE" == development ]]; then BOT_BUILD_SIGN_ARGS+=(--options runtime --timestamp=none); fi
codesign "${BOT_BUILD_SIGN_ARGS[@]}" "$BOT_BUNDLE"
bash script/verify-signature.sh "$BOT_BUNDLE" "$BOT_BUILD_SIGN_MODE"
echo "Built $BOT_BUNDLE"
