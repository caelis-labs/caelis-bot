#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
BOT_DW_BUNDLE=${1:?bundle required}
[[ "$(uname -m)" == arm64 ]] || { echo 'Desktop World rc.4 is qualified for macOS arm64 only.' >&2; exit 1; }
BOT_DW_CACHE="$BOT_ROOT/.cache/desktop-world-runtime"
BOT_DW_VERSION=$(node -p 'require("./resources/desktop-world/release.json").version')
mkdir -p "$BOT_DW_CACHE"
for BOT_DW_NAME in \
  "desktop-world-$BOT_DW_VERSION-darwin-arm64.tar.gz" \
  "desktop-world-$BOT_DW_VERSION-source.tar.gz" \
  BUILD-darwin-arm64.json ACCEPTANCE.json; do
  if [[ ! -f "$BOT_DW_CACHE/$BOT_DW_NAME" ]]; then
    curl --fail --location --retry 3 \
      "https://github.com/caelis-labs/desktop-world/releases/download/$BOT_DW_VERSION/$BOT_DW_NAME" \
      -o "$BOT_DW_CACHE/$BOT_DW_NAME.partial"
    mv "$BOT_DW_CACHE/$BOT_DW_NAME.partial" "$BOT_DW_CACHE/$BOT_DW_NAME"
  fi
done
python3 "$BOT_ROOT/script/stage-desktop-world-plugin.py" stage darwin-arm64 \
  "$BOT_DW_BUNDLE/Contents/Resources/Plugins"
# Check upstream's pinned ad-hoc binaries before adjusting worker rpaths or
# replacing their signatures with Bot's identity.
for BOT_DW_NATIVE in \
  "$BOT_DW_BUNDLE/Contents/Resources/Plugins/desktop-world/bin/dtw" \
  "$BOT_DW_BUNDLE/Contents/Resources/Plugins/desktop-world/libexec/cua-driver"; do
  codesign --verify --strict "$BOT_DW_NATIVE"
  BOT_DW_SIGN_DETAILS=$(codesign -dvv "$BOT_DW_NATIVE" 2>&1)
  grep -q '^Signature=adhoc$' <<<"$BOT_DW_SIGN_DETAILS"
done
/usr/libexec/PlistBuddy -c "Add CaelisDesktopWorldVersion string $BOT_DW_VERSION" "$BOT_DW_BUNDLE/Contents/Info.plist"
