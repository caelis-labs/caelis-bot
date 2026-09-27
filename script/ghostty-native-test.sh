#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory="$(mktemp -d "${TMPDIR:-/tmp}/bot-ghostty-contract.XXXXXX")"
trap 'rm -rf "$directory"' EXIT
bundle="$directory/Fixture.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>dev.caelis.ghostty-contract</string>
<key>CFBundleExecutable</key><string>fixture</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleName</key><string>Ghostty Contract Fixture</string>
<key>NSAppleScriptEnabled</key><true/>
<key>OSAScriptingDefinition</key><string>Fixture.sdef</string>
</dict></plist>
PLIST
# Cocoa's OSA loader caches bundles by identifier. Each disposable fixture must
# have its own identity so a later run cannot resolve a removed prior bundle.
/usr/bin/plutil -replace CFBundleIdentifier -string "dev.caelis.ghostty-contract.$(basename "$directory")" "$bundle/Contents/Info.plist"
cat > "$bundle/Contents/Resources/Fixture.sdef" <<'SDEF'
<?xml version="1.0" encoding="UTF-8"?>
<dictionary title="Local contract fixture"><suite name="Ghostty Suite" code="Ghst">
<record-type name="surface configuration" code="GScf">
<property name="command" code="GScC" type="text"><cocoa key="command"/></property>
<property name="wait after command" code="GScW" type="boolean"><cocoa key="waitAfterCommand"/></property>
</record-type>
<command name="new window" code="GhstNWin"><cocoa class="FixtureNewWindow"/>
<parameter name="with configuration" code="GNwS" type="surface configuration"><cocoa key="configuration"/></parameter>
</command>
</suite></dictionary>
SDEF
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/taskterminal" -framework Cocoa -framework ApplicationServices \
  "$BOT_ROOT/script/ghostty-native-test.m" \
  "$BOT_ROOT/internal/taskterminal/instance_darwin.m" \
  -o "$bundle/Contents/MacOS/fixture"
"$bundle/Contents/MacOS/fixture"
