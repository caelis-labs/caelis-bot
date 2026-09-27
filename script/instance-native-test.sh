#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory="$(mktemp -d "${TMPDIR:-/tmp}/bot-instance.XXXXXX")"
trap 'rm -rf "$directory"' EXIT
bundle="$directory/Fixture.app"
mkdir -p "$bundle/Contents/MacOS"
cat > "$bundle/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>dev.caelis.instance.$(basename "$directory")</string>
<key>CFBundleExecutable</key><string>fixture</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleName</key><string>Caelis Bot Instance Fixture</string>
<key>CFBundleDocumentTypes</key><array><dict><key>CFBundleTypeExtensions</key><array><string>command</string></array><key>CFBundleTypeRole</key><string>Viewer</string></dict></array>
</dict></plist>
PLIST
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/taskterminal" -framework Cocoa -framework ApplicationServices \
 "$BOT_ROOT/script/instance-native-test.m" -o "$bundle/Contents/MacOS/fixture"
touch "$directory/fixture.command"
/usr/bin/open -n "$bundle" --stdout "$directory/output" --stderr "$directory/output" --args --controller
for ((attempt=0; attempt<160; attempt++)); do
  if [[ -f "$directory/fixture.command.result" ]]; then cat "$directory/output"; exit 0; fi
  sleep 0.2
done
cat "$directory/output"
exit 1
