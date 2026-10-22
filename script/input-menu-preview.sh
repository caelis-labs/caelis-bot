#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
mkdir -p "$BOT_ROOT/.cache" "$BOT_ROOT/output/playwright/input-menu-native"
preview_dir=$(mktemp -d "${TMPDIR:-/tmp}/bot-input-menu.XXXXXX")
node script/input-menu-preview.mjs "$preview_dir/url" &
preview_server=$!
trap 'kill "$preview_server" 2>/dev/null || true; rm -rf "$preview_dir"' EXIT
preview_bundle="$BOT_ROOT/.cache/Caelis Input Menu Preview.app"
mkdir -p "$preview_bundle/Contents/MacOS"
clang -fobjc-arc -fblocks -framework Cocoa -framework WebKit script/input-menu-preview.m -o "$preview_bundle/Contents/MacOS/input-menu-preview"
cat > "$preview_bundle/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>input-menu-preview</string><key>CFBundleIdentifier</key><string>dev.caelis.input-menu-preview.$(printf %s "$BOT_ROOT" | shasum | cut -c1-8)</string><key>CFBundleName</key><string>Caelis Input Menu Preview</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$preview_bundle"
for ((attempt=0;attempt<50;attempt++)); do [[ -s "$preview_dir/url" ]] && break; sleep .1; done
[[ -s "$preview_dir/url" ]] || { echo 'UI fixture server did not start.' >&2; exit 1; }
/usr/bin/open -W -n "$preview_bundle" --args "$(cat "$preview_dir/url")" "$BOT_ROOT/output/playwright/input-menu-native" "${BOT_INPUT_MENU_AUTORUN:-0}"
