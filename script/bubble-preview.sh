#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# This is intentionally separate from the daily app and its single-instance ID.
npm run build
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-bubble-preview.XXXXXX")
node script/bubble-preview.mjs "$directory/url" &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null || true; rm -rf "$directory"' EXIT
bundle="$BOT_ROOT/dist/Caelis Bubble Preview.app"
mkdir -p "$bundle/Contents/MacOS"
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/desktop" -framework Cocoa -framework WebKit script/bubble-preview.m internal/desktop/material_darwin.m -o "$bundle/Contents/MacOS/bubble-preview"
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>bubble-preview</string><key>CFBundleIdentifier</key><string>dev.caelis.bubble-preview</string><key>CFBundleName</key><string>Caelis Bubble Preview</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$bundle"
for ((attempt=0;attempt<50;attempt++)); do [[ -s "$directory/url" ]] && break; sleep .1; done
/usr/bin/open -W -n "$bundle" --args "$(cat "$directory/url")" "$BOT_ROOT/.cache/bubble-preview.png"
