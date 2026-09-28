#!/usr/bin/env bash
# Verify the actual bundled payload, including resources that codesign --deep
# does not treat as nested code. Importing the SDK does not create a driver/TCC prompt.
set -euo pipefail
BOT_CUA_VERIFY_BUNDLE=${1:?bundle required}
BOT_CUA_VERIFY_MODE=${2:?signing mode required}
BOT_CUA_VERIFY_ROOT="$BOT_CUA_VERIFY_BUNDLE/Contents/Resources/ComputerUse"
BOT_CUA_VERIFY_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CaelisComputerUseVersion' "$BOT_CUA_VERIFY_BUNDLE/Contents/Info.plist" 2>/dev/null || true)
if [[ -z "$BOT_CUA_VERIFY_VERSION" && ! -e "$BOT_CUA_VERIFY_ROOT" && "${3:-}" == --allow-legacy ]]; then
  echo 'Legacy app has no Computer Use payload; preserving immutable release recovery.'
  exit 0
fi
[[ -n "$BOT_CUA_VERIFY_VERSION" && -d "$BOT_CUA_VERIFY_ROOT" ]] || { echo 'Missing Computer Use payload or bundle version marker.' >&2; exit 1; }
case "$BOT_CUA_VERIFY_MODE" in adhoc|development|developer-id) ;; *) exit 1 ;; esac
for BOT_CUA_VERIFY_FILE in node host.mjs desktop.mjs package.json package-lock.json Node-LICENSE MPL-2.0.txt THIRD-PARTY-NOTICES.md; do
  test -s "$BOT_CUA_VERIFY_ROOT/$BOT_CUA_VERIFY_FILE"
done
BOT_CUA_VERIFY_ARCH=$(lipo -archs "$BOT_CUA_VERIFY_BUNDLE/Contents/MacOS/caelis-bot")
case "$BOT_CUA_VERIFY_ARCH" in arm64|x86_64) ;; *) echo 'Unsupported host architecture.' >&2; exit 1 ;; esac
BOT_CUA_VERIFY_TEAM=$(codesign -dvv "$BOT_CUA_VERIFY_BUNDLE" 2>&1 | sed -n 's/^TeamIdentifier=//p')
BOT_CUA_VERIFY_COUNT=0
while IFS= read -r -d '' BOT_CUA_VERIFY_NATIVE; do
  codesign --verify --strict "$BOT_CUA_VERIFY_NATIVE"
  lipo -verify_arch "$BOT_CUA_VERIFY_ARCH" "$BOT_CUA_VERIFY_NATIVE"
  BOT_CUA_VERIFY_DETAILS=$(codesign -dvv "$BOT_CUA_VERIFY_NATIVE" 2>&1)
  if [[ "$BOT_CUA_VERIFY_MODE" == adhoc ]]; then
    grep -q '^Signature=adhoc$' <<< "$BOT_CUA_VERIFY_DETAILS"
  else
    [[ "$BOT_CUA_VERIFY_TEAM" =~ ^[A-Z0-9]{10}$ ]]
    grep -Fxq "TeamIdentifier=$BOT_CUA_VERIFY_TEAM" <<< "$BOT_CUA_VERIFY_DETAILS"
    grep -q 'flags=.*runtime' <<< "$BOT_CUA_VERIFY_DETAILS"
    if [[ "$BOT_CUA_VERIFY_MODE" == developer-id ]]; then
      codesign --verify --strict --test-requirement "=anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = \"$BOT_CUA_VERIFY_TEAM\"" "$BOT_CUA_VERIFY_NATIVE"
      grep -q '^Timestamp=' <<< "$BOT_CUA_VERIFY_DETAILS"
    else
      codesign --verify --strict --test-requirement '=anchor apple generic' "$BOT_CUA_VERIFY_NATIVE"
      grep -q '^Authority=Apple Development:' <<< "$BOT_CUA_VERIFY_DETAILS"
    fi
  fi
  # Absolute Homebrew/local dependencies would break on a clean user's Mac.
  bash "$(dirname "$0")/verify-native-dependencies.sh" "$BOT_CUA_VERIFY_ARCH" "$BOT_CUA_VERIFY_NATIVE"
  BOT_CUA_VERIFY_COUNT=$((BOT_CUA_VERIFY_COUNT+1))
done < <(find "$BOT_CUA_VERIFY_ROOT" -type f \( -name '*.dylib' -o -name '*.node' -o -name node \) -print0)
[[ "$BOT_CUA_VERIFY_COUNT" -ge 4 ]] || { echo 'Incomplete Computer Use native dependencies.' >&2; exit 1; }
"$BOT_CUA_VERIFY_ROOT/node" --jitless --input-type=module - "$BOT_CUA_VERIFY_ROOT" "$BOT_CUA_VERIFY_VERSION" <<'NODE'
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
const root=resolve(process.argv[2]), version=process.argv[3];
const read=p=>JSON.parse(readFileSync(resolve(root,p),'utf8'));
const lock=read('package-lock.json');
assert.equal(read('package.json').dependencies['@trycua/cua-driver'],version);
assert.equal(read('node_modules/@trycua/cua-driver/package.json').version,version);
assert.equal(lock.packages['node_modules/@trycua/cua-driver'].version,version);
const sdk=await import(pathToFileURL(resolve(root,'node_modules/@trycua/cua-driver/dist/index.js')));
assert.equal(typeof sdk.CuaDriver.create,'function');
console.log(`Computer Use verified: Cua ${version}, Node ${process.version}, ${process.arch}; native binding loaded without a desktop request.`);
NODE
