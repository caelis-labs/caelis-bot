#!/usr/bin/env bash
# Sign nested code inside-out; never use --deep to sign an app.
set -euo pipefail
bundle=${1:?app bundle required}
identity=${2:--}
framework="$bundle/Contents/Frameworks/Sparkle.framework"
[[ -d "$framework" ]] || exit 0 # Allows recovery of historical pre-updater releases.
args=(--force --sign "$identity" --options runtime)
if [[ "$identity" != - ]]; then
  args+=(--timestamp --keychain "${BOT_SIGN_KEYCHAIN:?}")
fi
for component in XPCServices/Downloader.xpc XPCServices/Installer.xpc Autoupdate Updater.app; do
  codesign "${args[@]}" "$framework/Versions/B/$component"
done
codesign "${args[@]}" "$framework"
codesign --verify --deep --strict "$framework"
