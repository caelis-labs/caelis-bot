#!/usr/bin/env bash
# Local prerelease packaging only. Never changes Gatekeeper or uploads artifacts.
set -euo pipefail
source "$(dirname "$0")/env.sh"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'Packaging currently requires macOS.' >&2
  exit 1
fi
if [[ "${1:-}" != --skip-build ]]; then
  "$BOT_ROOT/script/build.sh"
fi
BOT_PACKAGE_BUNDLE="$BOT_ROOT/dist/Caelis Bot.app"
plutil -lint "$BOT_PACKAGE_BUNDLE/Contents/Info.plist"
codesign --verify --deep --strict "$BOT_PACKAGE_BUNDLE"
BOT_PACKAGE_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$BOT_PACKAGE_BUNDLE/Contents/Info.plist")
BOT_PACKAGE_ARCH=$(lipo -archs "$BOT_PACKAGE_BUNDLE/Contents/MacOS/caelis-bot")
if [[ ! "$BOT_PACKAGE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || [[ "$BOT_PACKAGE_ARCH" != arm64 && "$BOT_PACKAGE_ARCH" != x86_64 ]]; then
  echo 'Unexpected bundle version or architecture.' >&2
  exit 1
fi
BOT_PACKAGE_NAME="Caelis-Bot-$BOT_PACKAGE_VERSION-preview-macos-$BOT_PACKAGE_ARCH.zip"
mkdir -p "$BOT_ROOT/dist/releases"
ditto -c -k --sequesterRsrc --keepParent "$BOT_PACKAGE_BUNDLE" "$BOT_ROOT/dist/releases/$BOT_PACKAGE_NAME"
cd "$BOT_ROOT/dist/releases"
shasum -a 256 "$BOT_PACKAGE_NAME" > "$BOT_PACKAGE_NAME.sha256"
echo "Created $BOT_PACKAGE_NAME and SHA-256 (ad-hoc signature; not notarized; not uploaded)."
