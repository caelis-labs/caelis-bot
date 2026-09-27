#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
bundle="$BOT_ROOT/.cache/Caelis Capture Preview.app"
mkdir -p "$bundle/Contents/MacOS"
clang -fobjc-arc -fblocks -Werror -Wno-deprecated-declarations -I internal/desktop \
  -framework Cocoa -framework ScreenCaptureKit -framework UniformTypeIdentifiers \
  script/capture-native-test.m internal/desktop/capture_image_darwin.m internal/desktop/permission_capture_darwin.m \
  -o "$bundle/Contents/MacOS/capture-preview"
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>capture-preview</string><key>CFBundleIdentifier</key><string>dev.caelis.capture-preview</string><key>CFBundleName</key><string>Caelis Capture Preview</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$bundle"
if [[ "${BOT_CAPTURE_LIVE:-}" == 1 ]]; then
  /usr/bin/open -n "$bundle" --args --live
else
  "$bundle/Contents/MacOS/capture-preview" "$BOT_ROOT/.cache/capture-preview.png"
fi
