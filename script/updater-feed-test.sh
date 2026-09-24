#!/usr/bin/env bash
# Real Sparkle signing + DMG extraction, using throwaway keys and an ad-hoc fixture.
# This proves feed generation, not Developer ID/notarization or app replacement.
set -euo pipefail
source "$(dirname "$0")/env.sh"
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-feed-test.XXXXXX")
trap 'rm -rf "$directory"' EXIT
umask 077
node --input-type=module - "$directory" <<'JS'
import {generateKeyPairSync} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const {publicKey,privateKey}=generateKeyPairSync('ed25519'), directory=process.argv[2];
writeFileSync(directory+'/public',publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64'));
writeFileSync(directory+'/private',privateKey.export({format:'der',type:'pkcs8'}).subarray(-32).toString('base64'));
JS
export BOT_SPARKLE_PUBLIC_KEY=$(cat "$directory/public")
export BOT_SPARKLE_PRIVATE_KEY=$(cat "$directory/private")
export BOT_RELEASE_TAG=v1.2.3 BOT_RELEASE_SOURCE_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
bundle="$directory/stage/Caelis Bot.app"
mkdir -p "$bundle/Contents/MacOS" "$directory/releases"
cp resources/macos/Info.plist "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set CFBundleVersion 1.2.3' "$bundle/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set CFBundleShortVersionString 1.2.3' "$bundle/Contents/Info.plist"
node script/configure-updates.mjs "$bundle/Contents/Info.plist"
echo 'int main(void) { return 0; }' > "$directory/main.c"
clang "$directory/main.c" -o "$bundle/Contents/MacOS/caelis-bot"
codesign --force --sign - --identifier dev.caelis.bot "$bundle"
name=Caelis-Bot-1.2.3-macos-arm64.dmg
hdiutil create -quiet -format UDZO -fs HFS+ -srcfolder "$directory/stage" "$directory/releases/$name"
(cd "$directory/releases"; shasum -a 256 "$name" > "$name.sha256")
if ! bash script/prepare-appcast.sh "$bundle" "$directory/releases"; then
  cat "$directory/releases/appcast.xml"
  exit 1
fi
printf 'tampered' >> "$directory/releases/$name"
if node script/update-manifest.mjs verify "$directory/releases" 2>/dev/null; then
  echo 'Tampered update was accepted' >&2; exit 1
fi
echo 'Real Sparkle DMG/feed signing and independent tamper rejection passed.'
