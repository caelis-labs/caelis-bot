#!/usr/bin/env bash
# Package an already signed app, or build an ad-hoc development app first.
# This does not notarize or upload anything.
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
BOT_PACKAGE_SIGNING_MODE=${BOT_SIGNING_MODE:-adhoc}
plutil -lint "$BOT_PACKAGE_BUNDLE/Contents/Info.plist"
bash "$BOT_ROOT/script/verify-signature.sh" "$BOT_PACKAGE_BUNDLE" "$BOT_PACKAGE_SIGNING_MODE"
if [[ "${BOT_REQUIRE_NOTARIZATION:-0}" == 1 ]]; then
  xcrun stapler validate "$BOT_PACKAGE_BUNDLE"
  spctl --assess --type execute --verbose=2 "$BOT_PACKAGE_BUNDLE"
fi
BOT_PACKAGE_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CaelisReleaseVersion' "$BOT_PACKAGE_BUNDLE/Contents/Info.plist")
if [[ -n "${BOT_RELEASE_TAG:-}" ]]; then
  # Recovery uses current packaging tools with the app built from the immutable tag.
  # The build job already checked that tag against its own package.json and source SHA.
  BOT_EXPECTED_VERSION=$(node --input-type=module -e 'import {validateTag} from "./script/release-version.mjs"; console.log(validateTag(process.env.BOT_RELEASE_TAG))')
else
  BOT_EXPECTED_VERSION=$(node script/release-version.mjs | node -pe 'JSON.parse(require("node:fs").readFileSync(0,"utf8")).version')
fi
BOT_PACKAGE_ARCH=$(lipo -archs "$BOT_PACKAGE_BUNDLE/Contents/MacOS/caelis-bot")
if [[ "$BOT_PACKAGE_VERSION" != "$BOT_EXPECTED_VERSION" ]] || [[ "$BOT_PACKAGE_ARCH" != arm64 && "$BOT_PACKAGE_ARCH" != x86_64 ]]; then
  echo 'Bundle version or architecture does not match this build.' >&2
  exit 1
fi
BOT_PACKAGE_NAME="Caelis-Bot-$BOT_PACKAGE_VERSION-macos-$BOT_PACKAGE_ARCH.dmg"
BOT_PACKAGE_DIR="$BOT_ROOT/dist/releases"
BOT_PACKAGE_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/caelis-dmg.XXXXXX")
BOT_PACKAGE_MOUNT=$(mktemp -d "${TMPDIR:-/tmp}/caelis-mount.XXXXXX")
BOT_PACKAGE_MOUNTED=false
cleanup() {
  if [[ "$BOT_PACKAGE_MOUNTED" == true ]]; then hdiutil detach "$BOT_PACKAGE_MOUNT" -quiet || true; fi
  rm -rf "$BOT_PACKAGE_STAGE"
  rmdir "$BOT_PACKAGE_MOUNT" 2>/dev/null || true
}
trap cleanup EXIT
mkdir -p "$BOT_PACKAGE_DIR"
ditto "$BOT_PACKAGE_BUNDLE" "$BOT_PACKAGE_STAGE/Caelis Bot.app"
ln -s /Applications "$BOT_PACKAGE_STAGE/Applications"
hdiutil create -quiet -ov -format UDZO -fs HFS+ -volname 'Caelis Bot' \
  -srcfolder "$BOT_PACKAGE_STAGE" "$BOT_PACKAGE_DIR/$BOT_PACKAGE_NAME"
hdiutil verify -quiet "$BOT_PACKAGE_DIR/$BOT_PACKAGE_NAME"
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$BOT_PACKAGE_MOUNT" \
  "$BOT_PACKAGE_DIR/$BOT_PACKAGE_NAME" -quiet
BOT_PACKAGE_MOUNTED=true
bash "$BOT_ROOT/script/verify-signature.sh" "$BOT_PACKAGE_MOUNT/Caelis Bot.app" "$BOT_PACKAGE_SIGNING_MODE"
if [[ "${BOT_REQUIRE_NOTARIZATION:-0}" == 1 ]]; then
  xcrun stapler validate "$BOT_PACKAGE_MOUNT/Caelis Bot.app"
  spctl --assess --type execute --verbose=2 "$BOT_PACKAGE_MOUNT/Caelis Bot.app"
fi
test "$(readlink "$BOT_PACKAGE_MOUNT/Applications")" = /Applications
test -f "$BOT_PACKAGE_MOUNT/Caelis Bot.app/Contents/Resources/ASSET-LICENSE.md"
cmp "$BOT_PACKAGE_BUNDLE/Contents/MacOS/caelis-bot" \
  "$BOT_PACKAGE_MOUNT/Caelis Bot.app/Contents/MacOS/caelis-bot"
hdiutil detach "$BOT_PACKAGE_MOUNT" -quiet
BOT_PACKAGE_MOUNTED=false
cd "$BOT_PACKAGE_DIR"
shasum -a 256 "$BOT_PACKAGE_NAME" > "$BOT_PACKAGE_NAME.sha256"
echo "Created and verified $BOT_PACKAGE_NAME and SHA-256 (app signing: $BOT_PACKAGE_SIGNING_MODE)."
