#!/usr/bin/env bash
# Sign and notarize the app and DMG after the credential-free build job.
set -euo pipefail
set +x
BOT_SIGN_ROOT=$(cd "$(dirname "$0")/.." && pwd)
[[ "$(uname -s)" == Darwin ]] || { echo 'Developer ID signing requires macOS.' >&2; exit 1; }
: "${BOT_SIGNING_CERTIFICATE_BASE64:?Missing Developer ID PKCS12 secret}"
: "${BOT_SIGNING_CERTIFICATE_PASSWORD:?Missing PKCS12 password secret}"
[[ "${BOT_SIGNING_TEAM_ID:-}" =~ ^[A-Z0-9]{10}$ ]] || { echo 'Missing or invalid Apple team ID.' >&2; exit 1; }
BOT_SIGN_BUNDLE="$BOT_SIGN_ROOT/dist/Caelis Bot.app"
export BOT_NOTARY_REPORTS="$BOT_SIGN_ROOT/dist/notarization"
[[ "${BOT_RELEASE_SOURCE_SHA:-}" =~ ^[a-f0-9]{40}$ ]]
: "${BOT_RELEASE_TAG:?Missing immutable release tag}"
mkdir -p "$BOT_NOTARY_REPORTS"
if [[ "${BOT_NOTARY_RESUME:-0}" == 1 ]]; then
  node -e 'const fs=require("node:fs"),assert=require("node:assert/strict");
    const state=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));
    assert.deepEqual(state,{version:1,tag:process.env.BOT_RELEASE_TAG,
      source:process.env.BOT_RELEASE_SOURCE_SHA,team:process.env.BOT_SIGNING_TEAM_ID});' \
    "$BOT_NOTARY_REPORTS/context.json"
  test -f "$BOT_NOTARY_REPORTS/app.zip"
  rm -rf "$BOT_SIGN_BUNDLE"
  ditto -x -k "$BOT_NOTARY_REPORTS/app.zip" "$BOT_SIGN_ROOT/dist"
else
  [[ ! -e "$BOT_NOTARY_REPORTS/context.json" ]] || { echo 'Existing notarization state requires explicit resume.' >&2; exit 1; }
fi
test -f "$BOT_SIGN_BUNDLE/Contents/MacOS/caelis-bot"
test "$(/usr/libexec/PlistBuddy -c 'Print CaelisSourceCommit' "$BOT_SIGN_BUNDLE/Contents/Info.plist")" = "$BOT_RELEASE_SOURCE_SHA"
test "$(/usr/libexec/PlistBuddy -c 'Print CaelisReleaseVersion' "$BOT_SIGN_BUNDLE/Contents/Info.plist")" = "${BOT_RELEASE_TAG#v}"
test "$(lipo -archs "$BOT_SIGN_BUNDLE/Contents/MacOS/caelis-bot")" = arm64
umask 077
BOT_SIGN_TEMP=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/caelis-signing.XXXXXX")
export BOT_SIGN_KEYCHAIN="$BOT_SIGN_TEMP/release.keychain-db"
BOT_VERIFY_MOUNT=
BOT_SIGN_ORIGINAL_KEYCHAINS=()
while IFS= read -r keychain; do
  BOT_SIGN_ORIGINAL_KEYCHAINS+=("$keychain")
done < <(security list-keychains -d user | sed -n 's/^[[:space:]]*"\(.*\)"$/\1/p')
cleanup() {
  if [[ -n "$BOT_VERIFY_MOUNT" ]]; then
    hdiutil detach "$BOT_VERIFY_MOUNT" -quiet >/dev/null 2>&1 || true
    rmdir "$BOT_VERIFY_MOUNT" 2>/dev/null || true
  fi
  security delete-keychain "$BOT_SIGN_KEYCHAIN" >/dev/null 2>&1 || true
  rm -rf "$BOT_SIGN_TEMP"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
printf '%s' "$BOT_SIGNING_CERTIFICATE_BASE64" | base64 --decode > "$BOT_SIGN_TEMP/identity.p12"
unset BOT_SIGNING_CERTIFICATE_BASE64
BOT_SIGN_KEYCHAIN_PASSWORD=$(openssl rand -hex 32)
security create-keychain -p "$BOT_SIGN_KEYCHAIN_PASSWORD" "$BOT_SIGN_KEYCHAIN"
if [[ ${#BOT_SIGN_ORIGINAL_KEYCHAINS[@]} -gt 0 ]]; then
  security list-keychains -d user -s "$BOT_SIGN_KEYCHAIN" "${BOT_SIGN_ORIGINAL_KEYCHAINS[@]}"
else
  security list-keychains -d user -s "$BOT_SIGN_KEYCHAIN"
fi
# Deleting this keychain on exit also removes its temporary search-list entry.
security set-keychain-settings -lut 9000 "$BOT_SIGN_KEYCHAIN"
security unlock-keychain -p "$BOT_SIGN_KEYCHAIN_PASSWORD" "$BOT_SIGN_KEYCHAIN"
security import "$BOT_SIGN_TEMP/identity.p12" -k "$BOT_SIGN_KEYCHAIN" \
  -P "$BOT_SIGNING_CERTIFICATE_PASSWORD" -T /usr/bin/codesign >/dev/null
unset BOT_SIGNING_CERTIFICATE_PASSWORD
rm "$BOT_SIGN_TEMP/identity.p12"
security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
  -k "$BOT_SIGN_KEYCHAIN_PASSWORD" "$BOT_SIGN_KEYCHAIN" >/dev/null
unset BOT_SIGN_KEYCHAIN_PASSWORD
# Include the public Apple G2 intermediate even on runners without it installed.
curl --fail --silent --show-error --location \
  https://www.apple.com/certificateauthority/DeveloperIDG2CA.cer -o "$BOT_SIGN_TEMP/AppleG2.cer"
printf '%s  %s\n' f16cd3c54c7f83cea4bf1a3e6a0819c8aaa8e4a1528fd144715f350643d2df3a \
  "$BOT_SIGN_TEMP/AppleG2.cer" | shasum -a 256 --check
security import "$BOT_SIGN_TEMP/AppleG2.cer" -k "$BOT_SIGN_KEYCHAIN" >/dev/null
BOT_SIGN_IDENTITIES=$(security find-identity -v -p codesigning "$BOT_SIGN_KEYCHAIN")
BOT_SIGN_MATCHES=$(printf '%s\n' "$BOT_SIGN_IDENTITIES" | sed -n 's/^[[:space:]]*[0-9][0-9]*) \([A-Fa-f0-9]\{40\}\) "Developer ID Application: .*"$/\1/p')
[[ -n "$BOT_SIGN_MATCHES" && "$BOT_SIGN_MATCHES" != *$'\n'* ]] || {
  echo 'The PKCS12 must contain exactly one valid Developer ID Application identity.' >&2; exit 1;
}
# This bundle currently contains one native executable and no embedded helpers.
# WebKit runs out of process; no JIT, debugger, or library-validation exception is needed.
if [[ "${BOT_NOTARY_RESUME:-0}" != 1 ]]; then
  echo 'Signing the app with the imported Developer ID identity.'
  codesign --force --sign "$BOT_SIGN_MATCHES" --keychain "$BOT_SIGN_KEYCHAIN" \
    --identifier dev.caelis.bot --options runtime --timestamp "$BOT_SIGN_BUNDLE"
fi
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_SIGN_BUNDLE" developer-id
echo 'Developer ID identity, team, hardened runtime and timestamp verified.'
if [[ "${BOT_NOTARY_RESUME:-0}" != 1 ]]; then
  ditto -c -k --keepParent "$BOT_SIGN_BUNDLE" "$BOT_NOTARY_REPORTS/app.zip"
  node -e 'require("node:fs").writeFileSync(process.argv[1],JSON.stringify({version:1,
    tag:process.env.BOT_RELEASE_TAG,source:process.env.BOT_RELEASE_SOURCE_SHA,
    team:process.env.BOT_SIGNING_TEAM_ID})+"\n")' "$BOT_NOTARY_REPORTS/context.json"
fi

# Notary credentials are scoped to this ephemeral keychain, never the login keychain.
: "${BOT_NOTARY_APPLE_ID:?Missing Apple notarization credentials}"
: "${BOT_NOTARY_PASSWORD:?Missing Apple app-specific password}"
xcrun notarytool store-credentials caelis-release --keychain "$BOT_SIGN_KEYCHAIN" \
  --apple-id "$BOT_NOTARY_APPLE_ID" --team-id "$BOT_SIGNING_TEAM_ID" --password "$BOT_NOTARY_PASSWORD" >/dev/null
unset BOT_NOTARY_PASSWORD BOT_NOTARY_APPLE_ID
notarize() {
  local code=0
  bash "$BOT_SIGN_ROOT/script/notarize.sh" "$@" || code=$?
  if [[ "$code" == 75 ]]; then
    [[ -z "${GITHUB_OUTPUT:-}" ]] || printf 'verified=false\nnotarization_status=pending\n' >> "$GITHUB_OUTPUT"
    if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
      printf '## Awaiting Apple notarization\n\nThe %s submission is still In Progress. No release was published. Resume the retained checkpoint with `resume_run_id=%s` and the same tag.\n' \
        "$2" "${GITHUB_RUN_ID:-unknown}" >> "$GITHUB_STEP_SUMMARY"
    fi
    # Pending is a successful checkpoint, not artifact rejection or permission to publish.
    exit 0
  fi
  [[ "$code" == 0 ]] || exit "$code"
}
# Staple the app before creating the DMG, so a copied app also works offline.
notarize "$BOT_NOTARY_REPORTS/app.zip" app
xcrun stapler staple "$BOT_SIGN_BUNDLE"
xcrun stapler validate "$BOT_SIGN_BUNDLE"
BOT_SIGN_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CaelisReleaseVersion' "$BOT_SIGN_BUNDLE/Contents/Info.plist")
BOT_SIGN_ARCH=$(lipo -archs "$BOT_SIGN_BUNDLE/Contents/MacOS/caelis-bot")
BOT_SIGN_DMG="$BOT_SIGN_ROOT/dist/releases/Caelis-Bot-$BOT_SIGN_VERSION-macos-$BOT_SIGN_ARCH.dmg"
if [[ ! -f "$BOT_NOTARY_REPORTS/release.dmg" ]]; then
  [[ ! -e "$BOT_NOTARY_REPORTS/dmg-submission.json" ]] || { echo 'Missing submitted DMG; cannot recreate its bytes.' >&2; exit 1; }
  BOT_SIGNING_MODE=developer-id BOT_REQUIRE_NOTARIZATION=1 bash "$BOT_SIGN_ROOT/script/package.sh" --skip-build
  codesign --force --sign "$BOT_SIGN_MATCHES" --keychain "$BOT_SIGN_KEYCHAIN" \
    --identifier dev.caelis.bot.dmg --timestamp "$BOT_SIGN_DMG"
  cp "$BOT_SIGN_DMG" "$BOT_NOTARY_REPORTS/release.dmg"
fi
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_NOTARY_REPORTS/release.dmg" developer-id dmg
notarize "$BOT_NOTARY_REPORTS/release.dmg" dmg
# Keep the uploaded bytes unchanged for future resumes; staple only the final copy.
mkdir -p "$(dirname "$BOT_SIGN_DMG")"
cp "$BOT_NOTARY_REPORTS/release.dmg" "$BOT_SIGN_DMG"
xcrun stapler staple "$BOT_SIGN_DMG"
xcrun stapler validate "$BOT_SIGN_DMG"
hdiutil verify -quiet "$BOT_SIGN_DMG"
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_SIGN_DMG" developer-id dmg
spctl --assess --type open --context context:primary-signature --verbose=2 "$BOT_SIGN_DMG"
# Also verify the enclosed app when resuming an already-created DMG.
BOT_VERIFY_MOUNT=$(mktemp -d "${TMPDIR:-/tmp}/caelis-signed-dmg.XXXXXX")
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$BOT_VERIFY_MOUNT" "$BOT_SIGN_DMG" -quiet
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_VERIFY_MOUNT/Caelis Bot.app" developer-id
xcrun stapler validate "$BOT_VERIFY_MOUNT/Caelis Bot.app"
spctl --assess --type execute --verbose=2 "$BOT_VERIFY_MOUNT/Caelis Bot.app"
test "$(/usr/libexec/PlistBuddy -c 'Print CaelisSourceCommit' "$BOT_VERIFY_MOUNT/Caelis Bot.app/Contents/Info.plist")" = "$BOT_RELEASE_SOURCE_SHA"
cmp "$BOT_SIGN_BUNDLE/Contents/MacOS/caelis-bot" "$BOT_VERIFY_MOUNT/Caelis Bot.app/Contents/MacOS/caelis-bot"
hdiutil detach "$BOT_VERIFY_MOUNT" -quiet
rmdir "$BOT_VERIFY_MOUNT"
BOT_VERIFY_MOUNT=
# Signing and stapling change the DMG bytes. Only this final checksum is published.
cd "$(dirname "$BOT_SIGN_DMG")"
shasum -a 256 "$(basename "$BOT_SIGN_DMG")" > "$BOT_SIGN_DMG.sha256"
echo 'App and DMG are Developer ID signed, notarized, stapled and Gatekeeper accepted.'
[[ -z "${GITHUB_OUTPUT:-}" ]] || printf 'verified=true\nnotarization_status=accepted\n' >> "$GITHUB_OUTPUT"
