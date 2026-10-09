#!/usr/bin/env bash
set -euo pipefail
directory=${1:?Expected candidate artifact directory}
: "${RELEASE_TAG:?Missing tag}"
: "${ACCEPTED_DMG_SHA256:?Missing accepted DMG digest}"
: "${ACCEPTANCE_URL:?Missing acceptance record URL}"
[[ "$ACCEPTED_DMG_SHA256" =~ ^[a-f0-9]{64}$ ]]
[[ "$ACCEPTANCE_URL" =~ ^https://github\.com/caelis-labs/caelis-bot/(issues|pull)/[1-9][0-9]*(\#.*)?$ ]]
export BOT_RELEASE_TAG=$RELEASE_TAG
export BOT_ROOT=$PWD
source script/app-identity.sh
[[ "$BOT_BUILD_CHANNEL" == release ]]
if [[ "$RELEASE_TAG" == *-dev.* ]]; then channel=dev; else channel=stable; fi
node script/update-manifest.mjs verify "$directory"
source_sha=$(node -p 'require(process.argv[1]).source' "./$directory/latest.json")
digest=$(node -p 'require(process.argv[1]).sha256' "./$directory/latest.json")
test "$digest" = "$ACCEPTED_DMG_SHA256"
tag_sha=$(git rev-parse "refs/tags/$RELEASE_TAG^{commit}")
test "$source_sha" = "$tag_sha"
git merge-base --is-ancestor "$source_sha" origin/main
version=${RELEASE_TAG#v}
name="Caelis-Bot-$version-macos-arm64.dmg"
test "$(shasum -a 256 "$directory/$name" | cut -d ' ' -f 1)" = "$ACCEPTED_DMG_SHA256"
hdiutil verify -quiet "$directory/$name"
bash script/verify-signature.sh "$directory/$name" developer-id dmg "$BOT_APP_ID.dmg"
xcrun stapler validate "$directory/$name"
spctl --assess --type open --context context:primary-signature "$directory/$name"
mount=$(mktemp -d "${TMPDIR:-/tmp}/caelis-candidate.XXXXXX")
mounted=false
cleanup() { if [[ "$mounted" == true ]]; then hdiutil detach "$mount" -quiet || true; fi; rmdir "$mount" 2>/dev/null || true; }
trap cleanup EXIT
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$mount" "$directory/$name" -quiet
mounted=true
app="$mount/$BOT_APP_NAME.app"
bash script/verify-signature.sh "$app" developer-id app "$BOT_APP_ID"
xcrun stapler validate "$app"
spctl --assess --type execute "$app"
test "$(/usr/libexec/PlistBuddy -c 'Print CaelisSourceCommit' "$app/Contents/Info.plist")" = "$source_sha"
test "$(/usr/libexec/PlistBuddy -c 'Print CaelisReleaseVersion' "$app/Contents/Info.plist")" = "$version"
test "$(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$app/Contents/Info.plist")" = "$BOT_APP_ID"
if [[ "$channel" == stable || "$channel" == dev ]]; then
  test "$(/usr/libexec/PlistBuddy -c 'Print CaelisAutoUpdatesEnabled' "$app/Contents/Info.plist")" = true
  test "$(/usr/libexec/PlistBuddy -c 'Print SUFeedURL' "$app/Contents/Info.plist")" = 'https://releases.caelis.dev/caelis-bot/appcast.xml'
  test "$(/usr/libexec/PlistBuddy -c 'Print CaelisDevFeedURL' "$app/Contents/Info.plist")" = 'https://releases.caelis.dev/caelis-bot/feeds/macos/arm64/dev/appcast.xml'
  test "$(/usr/libexec/PlistBuddy -c 'Print CaelisDefaultUpdateChannel' "$app/Contents/Info.plist")" = "$channel"
fi
expected_bundle=$(node --input-type=module -e 'import {releaseVersion} from "./script/release-version.mjs";const v=process.env.BOT_RELEASE_TAG.slice(1);console.log(releaseVersion(v.split("-")[0],process.env.BOT_RELEASE_TAG).bundleVersion)')
test "$(/usr/libexec/PlistBuddy -c 'Print CFBundleVersion' "$app/Contents/Info.plist")" = "$expected_bundle"
printf 'source=%s\nchannel=%s\n' "$source_sha" "$channel" >> "$GITHUB_OUTPUT"
printf 'Accepted candidate: %s\nSource: %s\nDMG SHA-256: %s\nEvidence: %s\n' "$RELEASE_TAG" "$source_sha" "$digest" "$ACCEPTANCE_URL" >> "$GITHUB_STEP_SUMMARY"
