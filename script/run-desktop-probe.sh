#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
BOT_PROBE="$BOT_ROOT/.cache/Native Probe.app"
mkdir -p "$BOT_PROBE/Contents/MacOS"
xcrun swiftc -module-cache-path "$BOT_ROOT/.cache/swift-modules" script/desktop-probe.swift -o "$BOT_PROBE/Contents/MacOS/native-probe"
cat > "$BOT_PROBE/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>native-probe</string>
<key>CFBundleIdentifier</key><string>dev.caelis.bot.probe</string>
<key>CFBundleName</key><string>Native Probe</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>NSPrincipalClass</key><string>NSApplication</string>
</dict></plist>
PLIST
open -n "$BOT_PROBE"
