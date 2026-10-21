#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
preview_dir=$(mktemp -d "${TMPDIR:-/tmp}/bot-settings-preview.XXXXXX")
node script/desktop-settings-preview.mjs "$preview_dir/url" &
preview_server=$!
trap 'kill "$preview_server" 2>/dev/null || true; rm -rf "$preview_dir"' EXIT
preview_bundle="$BOT_ROOT/dist/Caelis Settings Preview.app"
mkdir -p "$preview_bundle/Contents/MacOS" "$BOT_ROOT/.cache/settings-acceptance"
clang -fobjc-arc -fblocks -framework Cocoa -framework WebKit script/desktop-settings-preview.m internal/desktop/context_navigation_darwin.m internal/desktop/capture_menu_darwin.m -o "$preview_bundle/Contents/MacOS/settings-preview"
cat > "$preview_bundle/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>settings-preview</string><key>CFBundleIdentifier</key><string>dev.caelis.settings-preview.$(printf %s "$BOT_ROOT" | shasum | cut -c1-8)</string><key>CFBundleName</key><string>Caelis Settings Preview</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$preview_bundle"
for ((attempt=0;attempt<50;attempt++)); do [[ -s "$preview_dir/url" ]] && break; sleep .1; done
/usr/bin/open -W -n "$preview_bundle" --args "$(cat "$preview_dir/url")" "$BOT_ROOT/.cache/settings-acceptance"
