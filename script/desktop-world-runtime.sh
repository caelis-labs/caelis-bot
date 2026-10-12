#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
BOT_DW_BUNDLE=${1:?bundle required}
[[ "$(uname -m)" == arm64 ]] || { echo 'Desktop World is qualified for macOS arm64 only.' >&2; exit 1; }
BOT_DW_VERSION=$(node -p 'require("./resources/desktop-world/release.json").version')
BOT_DW_CACHE="$BOT_ROOT/.cache/desktop-world-runtime/$BOT_DW_VERSION"
BOT_DW_ARTIFACT=$(node -p 'require("./resources/desktop-world/release.json").targets["darwin-arm64"].artifact')
mkdir -p "$BOT_DW_CACHE"
for BOT_DW_NAME in \
  "$BOT_DW_ARTIFACT" BUILD-darwin-arm64.json; do
  if [[ ! -f "$BOT_DW_CACHE/$BOT_DW_NAME" ]]; then
    curl --fail --location --retry 3 \
      "https://github.com/caelis-labs/desktop-world/releases/download/$BOT_DW_VERSION/$BOT_DW_NAME" \
      -o "$BOT_DW_CACHE/$BOT_DW_NAME.partial"
    mv "$BOT_DW_CACHE/$BOT_DW_NAME.partial" "$BOT_DW_CACHE/$BOT_DW_NAME"
  fi
done
python3 "$BOT_ROOT/script/stage-desktop-world-plugin.py" stage darwin-arm64 \
  "$BOT_DW_BUNDLE/Contents/Resources/Plugins"
# Staging verifies upstream Developer ID signatures before exporting or changing
# binaries. The enclosing Bot must independently pass its own release gates.
/usr/libexec/PlistBuddy -c "Add CaelisDesktopWorldVersion string $BOT_DW_VERSION" "$BOT_DW_BUNDLE/Contents/Info.plist"
