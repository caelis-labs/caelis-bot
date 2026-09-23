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
test -f "$BOT_SIGN_BUNDLE/Contents/MacOS/caelis-bot"
umask 077
BOT_SIGN_TEMP=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/caelis-signing.XXXXXX")
BOT_SIGN_KEYCHAIN="$BOT_SIGN_TEMP/release.keychain-db"
cleanup() {
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
security set-keychain-settings -lut 3600 "$BOT_SIGN_KEYCHAIN"
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
codesign --force --sign "$BOT_SIGN_MATCHES" --keychain "$BOT_SIGN_KEYCHAIN" \
  --identifier dev.caelis.bot --options runtime --timestamp "$BOT_SIGN_BUNDLE"
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_SIGN_BUNDLE" developer-id
echo 'Developer ID identity, team, hardened runtime and timestamp verified.'

# Notary credentials are scoped to this ephemeral keychain, never the login keychain.
: "${BOT_NOTARY_APPLE_ID:?Missing Apple notarization credentials}"
: "${BOT_NOTARY_PASSWORD:?Missing Apple app-specific password}"
xcrun notarytool store-credentials caelis-release --keychain "$BOT_SIGN_KEYCHAIN" \
  --apple-id "$BOT_NOTARY_APPLE_ID" --team-id "$BOT_SIGNING_TEAM_ID" --password "$BOT_NOTARY_PASSWORD" >/dev/null
unset BOT_NOTARY_PASSWORD BOT_NOTARY_APPLE_ID
BOT_NOTARY_REPORTS="$BOT_SIGN_ROOT/dist/notarization"
mkdir -p "$BOT_NOTARY_REPORTS"
notarize() {
  local artifact=$1 label=$2 result status submission
  result="$BOT_NOTARY_REPORTS/$label.json"
  # A timeout leaves a receipt in the log; it never proceeds to publishing.
  xcrun notarytool submit "$artifact" --keychain-profile caelis-release --keychain "$BOT_SIGN_KEYCHAIN" \
    --wait --timeout 20m --output-format json > "$result" || { cat "$result"; return 1; }
  cat "$result"
  submission=$(node -pe 'JSON.parse(require("node:fs").readFileSync(process.argv[1],"utf8")).id' "$result")
  status=$(node -pe 'JSON.parse(require("node:fs").readFileSync(process.argv[1],"utf8")).status' "$result")
  xcrun notarytool log "$submission" --keychain-profile caelis-release --keychain "$BOT_SIGN_KEYCHAIN" \
    "$BOT_NOTARY_REPORTS/$label-log.json"
  cat "$BOT_NOTARY_REPORTS/$label-log.json"
  [[ "$status" == Accepted ]] || { echo "Apple rejected $label: $status" >&2; return 1; }
}
# Staple the app before creating the DMG, so a copied app also works offline.
ditto -c -k --keepParent "$BOT_SIGN_BUNDLE" "$BOT_SIGN_TEMP/app.zip"
notarize "$BOT_SIGN_TEMP/app.zip" app
xcrun stapler staple "$BOT_SIGN_BUNDLE"
xcrun stapler validate "$BOT_SIGN_BUNDLE"
BOT_SIGNING_MODE=developer-id BOT_REQUIRE_NOTARIZATION=1 bash "$BOT_SIGN_ROOT/script/package.sh" --skip-build
BOT_SIGN_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CaelisReleaseVersion' "$BOT_SIGN_BUNDLE/Contents/Info.plist")
BOT_SIGN_ARCH=$(lipo -archs "$BOT_SIGN_BUNDLE/Contents/MacOS/caelis-bot")
BOT_SIGN_DMG="$BOT_SIGN_ROOT/dist/releases/Caelis-Bot-$BOT_SIGN_VERSION-macos-$BOT_SIGN_ARCH.dmg"
codesign --force --sign "$BOT_SIGN_MATCHES" --keychain "$BOT_SIGN_KEYCHAIN" \
  --identifier dev.caelis.bot.dmg --timestamp "$BOT_SIGN_DMG"
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_SIGN_DMG" developer-id dmg
notarize "$BOT_SIGN_DMG" dmg
xcrun stapler staple "$BOT_SIGN_DMG"
xcrun stapler validate "$BOT_SIGN_DMG"
hdiutil verify -quiet "$BOT_SIGN_DMG"
bash "$BOT_SIGN_ROOT/script/verify-signature.sh" "$BOT_SIGN_DMG" developer-id dmg
spctl --assess --type open --context context:primary-signature --verbose=2 "$BOT_SIGN_DMG"
# Signing and stapling change the DMG bytes. Only this final checksum is published.
cd "$(dirname "$BOT_SIGN_DMG")"
shasum -a 256 "$(basename "$BOT_SIGN_DMG")" > "$BOT_SIGN_DMG.sha256"
echo 'App and DMG are Developer ID signed, notarized, stapled and Gatekeeper accepted.'
