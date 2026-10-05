#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/../../script/env.sh"
bundle="$BOT_ROOT/.cache/Caelis Desktop Control Fixture.app"
if [[ "${BOT_DESKTOP_FIXTURE_ALPHA6:-}" == 1 ]]; then
  bundle="$BOT_ROOT/.cache/Caelis Alpha6 Fixture.app"
elif [[ "${BOT_DESKTOP_FIXTURE_SCROLL_RC2:-}" == 1 ]]; then
  bundle="$BOT_ROOT/.cache/Caelis RC2 Scroll Fixture.app"
elif [[ "${BOT_DESKTOP_FIXTURE_MENU_RC2:-}" == 1 ]]; then
  bundle="$BOT_ROOT/.cache/Caelis RC2 Menu Fixture.app"
fi
mkdir -p "$bundle/Contents/MacOS"
if [[ "${BOT_DESKTOP_FIXTURE_ALPHA6:-}" == 1 ]]; then
  swiftc experiments/desktop-control/alpha6.swift -o "$bundle/Contents/MacOS/desktop-control-fixture"
elif [[ "${BOT_DESKTOP_FIXTURE_SCROLL_RC2:-}" == 1 ]]; then
  swiftc -framework WebKit experiments/desktop-control/scroll-rc2.swift -o "$bundle/Contents/MacOS/desktop-control-fixture"
elif [[ "${BOT_DESKTOP_FIXTURE_MENU_RC2:-}" == 1 ]]; then
  swiftc experiments/desktop-control/menu-rc2.swift -o "$bundle/Contents/MacOS/desktop-control-fixture"
else
  clang -fobjc-arc -fblocks -Werror -Wno-deprecated-declarations -framework Cocoa \
    experiments/desktop-control/fixture.m -o "$bundle/Contents/MacOS/desktop-control-fixture"
fi
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>desktop-control-fixture</string><key>CFBundleIdentifier</key><string>dev.caelis.desktop-control-fixture</string><key>CFBundleName</key><string>Caelis Desktop Control Fixture</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
if [[ "${BOT_DESKTOP_FIXTURE_ALPHA6:-}" == 1 ]]; then
  /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier dev.caelis.bot.alpha6-fixture" "$bundle/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleName Caelis Alpha6 Fixture" "$bundle/Contents/Info.plist"
elif [[ "${BOT_DESKTOP_FIXTURE_SCROLL_RC2:-}" == 1 ]]; then
  /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier dev.caelis.bot.rc2-scroll-fixture" "$bundle/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleName Caelis RC2 Scroll Fixture" "$bundle/Contents/Info.plist"
elif [[ "${BOT_DESKTOP_FIXTURE_MENU_RC2:-}" == 1 ]]; then
  /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier dev.caelis.bot.rc2-menu-fixture" "$bundle/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleName Caelis RC2 Menu Fixture" "$bundle/Contents/Info.plist"
fi
codesign --force --sign - "$bundle"
/usr/bin/open -n "$bundle" --args --title "${BOT_DESKTOP_FIXTURE_TITLE:-Caelis Desktop Control Fixture}" --log "${BOT_DESKTOP_FIXTURE_LOG:-$BOT_ROOT/.cache/desktop-fixture-result.json}"
