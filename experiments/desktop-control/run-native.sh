#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/../../script/env.sh"
bundle="$BOT_ROOT/.cache/Caelis Desktop Control Fixture.app"
mkdir -p "$bundle/Contents/MacOS"
clang -fobjc-arc -fblocks -Werror -Wno-deprecated-declarations -framework Cocoa \
  experiments/desktop-control/fixture.m -o "$bundle/Contents/MacOS/desktop-control-fixture"
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>desktop-control-fixture</string><key>CFBundleIdentifier</key><string>dev.caelis.desktop-control-fixture</string><key>CFBundleName</key><string>Caelis Desktop Control Fixture</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign - "$bundle"
/usr/bin/open -n "$bundle" --args --title "${BOT_DESKTOP_FIXTURE_TITLE:-Caelis Desktop Control Fixture}" --log "${BOT_DESKTOP_FIXTURE_LOG:-$BOT_ROOT/.cache/desktop-fixture-result.json}"
