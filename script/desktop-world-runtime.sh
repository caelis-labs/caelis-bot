#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
BOT_DW_BUNDLE=${1:?bundle required}
[[ "$(uname -m)" == arm64 ]] || { echo 'Desktop World helper is qualified for macOS arm64 only.' >&2; exit 1; }
BOT_DW_CACHE="$BOT_ROOT/.cache/desktop-world-runtime"
BOT_DW_VERSION=$(node -p 'require("./resources/desktop-world/release.json").version')
BOT_DW_ARCHIVE=$(node -p 'require("./resources/desktop-world/release.json").archive')
BOT_DW_HASH=$(node -p 'require("./resources/desktop-world/release.json").sha256')
mkdir -p "$BOT_DW_CACHE"
if [[ ! -f "$BOT_DW_CACHE/$BOT_DW_ARCHIVE" ]]; then
  curl --fail --location --retry 3 "https://github.com/caelis-labs/desktop-world/releases/download/$BOT_DW_VERSION/$BOT_DW_ARCHIVE" -o "$BOT_DW_CACHE/$BOT_DW_ARCHIVE.partial"
  mv "$BOT_DW_CACHE/$BOT_DW_ARCHIVE.partial" "$BOT_DW_CACHE/$BOT_DW_ARCHIVE"
fi
[[ "$(shasum -a 256 "$BOT_DW_CACHE/$BOT_DW_ARCHIVE" | awk '{print $1}')" == "$BOT_DW_HASH" ]] || { echo 'Desktop World archive checksum mismatch.' >&2; exit 1; }
BOT_DW_STAGE=$(mktemp -d "$BOT_DW_CACHE/stage.XXXXXX")
trap 'rm -rf "$BOT_DW_STAGE"' EXIT
# The exact verified archive is trusted; no model-selected paths or downloads.
tar -xzf "$BOT_DW_CACHE/$BOT_DW_ARCHIVE" -C "$BOT_DW_STAGE"
BOT_DW_SOURCE="$BOT_DW_STAGE/${BOT_DW_ARCHIVE%.tar.gz}"
node "$BOT_ROOT/script/verify-desktop-world-manifest.mjs" "$BOT_DW_SOURCE"
BOT_DW_DEST="$BOT_DW_BUNDLE/Contents/Resources/DesktopWorld"
rm -rf "$BOT_DW_DEST"
mkdir -p "$BOT_DW_DEST/bin"
cp "$BOT_DW_SOURCE/bin/dtw" "$BOT_DW_DEST/bin/dtw"
cp "$BOT_DW_SOURCE/manifest.json" "$BOT_DW_SOURCE/LICENSE" "$BOT_DW_SOURCE/NOTICE" "$BOT_DW_SOURCE/THIRD_PARTY_NOTICES.md" "$BOT_DW_DEST/"
# The pinned helper is MPL-2.0. Preserve its corresponding source in the app.
cp -R "$BOT_DW_SOURCE/source" "$BOT_DW_DEST/source"
/usr/libexec/PlistBuddy -c "Add CaelisDesktopWorldVersion string $BOT_DW_VERSION" "$BOT_DW_BUNDLE/Contents/Info.plist"
