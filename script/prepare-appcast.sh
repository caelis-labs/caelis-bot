#!/usr/bin/env bash
# Runs only after the app and DMG passed notarization, stapling and Gatekeeper.
set -euo pipefail
source "$(dirname "$0")/env.sh"
source script/sparkle.sh
trap 'rm -rf "$BOT_SPARKLE_DIR"' EXIT
: "${BOT_SPARKLE_PRIVATE_KEY:?Missing Sparkle signing key}"
: "${BOT_SPARKLE_PUBLIC_KEY:?Missing Sparkle public key}"
version=$(node --input-type=module -e 'import {validateTag} from "./script/release-version.mjs";console.log(validateTag(process.env.BOT_RELEASE_TAG))')
source script/app-identity.sh
bundle="${1:-$BOT_BUNDLE}"
test "$(/usr/libexec/PlistBuddy -c 'Print SUPublicEDKey' "$bundle/Contents/Info.plist")" = "$BOT_SPARKLE_PUBLIC_KEY"
directory="${2:-$BOT_ROOT/dist/releases}"
name="Caelis-Bot-$version-macos-arm64.dmg"
if [[ "$version" == *-* && "$version" != *-dev.* ]]; then
  # Historical preview remains a manual-download pointer.
  test "$(/usr/libexec/PlistBuddy -c 'Print CaelisAutoUpdatesEnabled' "$bundle/Contents/Info.plist")" = false
  test "$(/usr/libexec/PlistBuddy -c 'Print CFBundleVersion' "$bundle/Contents/Info.plist")" = "${version%%-*}"
  node script/update-manifest.mjs create "$directory"
  printf '%s' "$BOT_SPARKLE_PRIVATE_KEY" | "$BOT_SPARKLE_DIR/bin/sign_update" --ed-key-file - -p "$directory/latest.json" > "$directory/latest.json.sig"
  node script/update-manifest.mjs verify "$directory"
  exit 0
fi
test "$(/usr/libexec/PlistBuddy -c 'Print CaelisAutoUpdatesEnabled' "$bundle/Contents/Info.plist")" = true
expected_bundle=$(node --input-type=module -e 'import {releaseVersion} from "./script/release-version.mjs";const v=process.env.BOT_RELEASE_TAG.slice(1);console.log(releaseVersion(v.split("-")[0],process.env.BOT_RELEASE_TAG).bundleVersion)')
test "$(/usr/libexec/PlistBuddy -c 'Print CFBundleVersion' "$bundle/Contents/Info.plist")" = "$expected_bundle"
# Never edit the signed feed after this tool has produced it. No delta/history retention.
download_prefix="https://releases.caelis.dev/caelis-bot/releases/$BOT_RELEASE_TAG/"
if [[ "$version" == *-dev.* ]]; then download_prefix="https://releases.caelis.dev/caelis-bot/dev/macos/arm64/$BOT_RELEASE_TAG/"; fi
printf '%s' "$BOT_SPARKLE_PRIVATE_KEY" | "$BOT_SPARKLE_DIR/bin/generate_appcast" \
  --ed-key-file - --maximum-versions 1 --maximum-deltas 0 \
  --download-url-prefix "$download_prefix" \
  --link 'https://github.com/caelis-labs/caelis-bot/releases' "$directory"
# An independently verifiable manifest binds the exact notarized bytes and feed.
node script/update-manifest.mjs create "$directory"
printf '%s' "$BOT_SPARKLE_PRIVATE_KEY" | "$BOT_SPARKLE_DIR/bin/sign_update" --ed-key-file - -p "$directory/latest.json" > "$directory/latest.json.sig"
node script/update-manifest.mjs verify "$directory"
