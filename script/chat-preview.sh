#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# Independent native WebKit fixture; never opens the daily Bot profile.
npm run build
preview_dir=$(mktemp -d "${TMPDIR:-/tmp}/bot-chat-preview.XXXXXX")
BOT_PREVIEW_SURFACE="${BOT_PREVIEW_SURFACE:-history}" node script/bubble-preview.mjs "$preview_dir/url" &
preview_server=$!
trap 'kill "$preview_server" 2>/dev/null || true; rm -rf "$preview_dir"' EXIT
preview_bundle="$BOT_ROOT/dist/Caelis Chat Preview.app"
mkdir -p "$preview_bundle/Contents/MacOS" "$BOT_ROOT/.cache"
clang -fobjc-arc -fblocks -framework Cocoa -framework WebKit script/chat-preview.m -o "$preview_bundle/Contents/MacOS/chat-preview"
cat > "$preview_bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>chat-preview</string><key>CFBundleIdentifier</key><string>dev.caelis.chat-preview</string><key>CFBundleName</key><string>Caelis Chat Preview</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$preview_bundle"
for ((attempt=0;attempt<50;attempt++)); do [[ -s "$preview_dir/url" ]] && break; sleep .1; done
[[ -s "$preview_dir/url" ]] || { echo 'Chat preview server did not start.' >&2; exit 1; }
capture_path="${BOT_PREVIEW_CAPTURE:-$BOT_ROOT/.cache/chat-preview.png}"
args=("$(cat "$preview_dir/url")" "$capture_path")
if [[ "${BOT_PREVIEW_AUTO_CAPTURE:-}" == 1 ]]; then args+=(auto "${BOT_PREVIEW_APPEARANCE:-light}"); fi
/usr/bin/open -W -n "$preview_bundle" --args "${args[@]}"
