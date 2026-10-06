#!/usr/bin/env bash
# Verify the pinned helper and its actual nested signature without desktop input.
set -euo pipefail
BOT_DW_VERIFY_BUNDLE=${1:?bundle required}
BOT_DW_VERIFY_MODE=${2:?signing mode required}
BOT_DW_VERIFY_ROOT="$BOT_DW_VERIFY_BUNDLE/Contents/Resources/DesktopWorld"
BOT_DW_VERIFY_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CaelisDesktopWorldVersion' "$BOT_DW_VERIFY_BUNDLE/Contents/Info.plist" 2>/dev/null || true)
[[ -n "$BOT_DW_VERIFY_VERSION" && -d "$BOT_DW_VERIFY_ROOT" ]] || { echo 'Missing Desktop World payload or bundle version marker.' >&2; exit 1; }
case "$BOT_DW_VERIFY_MODE" in adhoc|development|developer-id) ;; *) exit 1 ;; esac
for BOT_DW_VERIFY_FILE in bin/dtw manifest.json LICENSE NOTICE THIRD_PARTY_NOTICES.md source/go.mod source/host/client.go; do
 test -s "$BOT_DW_VERIFY_ROOT/$BOT_DW_VERIFY_FILE"
done
node "$(dirname "$0")/verify-desktop-world-manifest.mjs" "$BOT_DW_VERIFY_ROOT" --target darwin-arm64 --bundled
[[ "$BOT_DW_VERIFY_VERSION" == "$(node -p 'require(process.argv[1]).version' "$BOT_DW_VERIFY_ROOT/manifest.json")" ]]
BOT_DW_VERIFY_ARCH=$(lipo -archs "$BOT_DW_VERIFY_BUNDLE/Contents/MacOS/caelis-bot")
case "$BOT_DW_VERIFY_ARCH" in arm64|x86_64) ;; *) echo 'Unsupported host architecture.' >&2; exit 1 ;; esac
BOT_DW_VERIFY_TEAM=$(codesign -dvv "$BOT_DW_VERIFY_BUNDLE" 2>&1 | sed -n 's/^TeamIdentifier=//p')
BOT_DW_VERIFY_COUNT=0
while IFS= read -r -d '' BOT_DW_VERIFY_NATIVE; do
  codesign --verify --strict "$BOT_DW_VERIFY_NATIVE"
  lipo "$BOT_DW_VERIFY_NATIVE" -verify_arch "$BOT_DW_VERIFY_ARCH"
  BOT_DW_VERIFY_DETAILS=$(codesign -dvv "$BOT_DW_VERIFY_NATIVE" 2>&1)
  if [[ "$BOT_DW_VERIFY_MODE" == adhoc ]]; then
    grep -q '^Signature=adhoc$' <<< "$BOT_DW_VERIFY_DETAILS"
  else
    [[ "$BOT_DW_VERIFY_TEAM" =~ ^[A-Z0-9]{10}$ ]]
    grep -Fxq "TeamIdentifier=$BOT_DW_VERIFY_TEAM" <<< "$BOT_DW_VERIFY_DETAILS"
    grep -q 'flags=.*runtime' <<< "$BOT_DW_VERIFY_DETAILS"
    if [[ "$BOT_DW_VERIFY_MODE" == developer-id ]]; then
      codesign --verify --strict --test-requirement "=anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = \"$BOT_DW_VERIFY_TEAM\"" "$BOT_DW_VERIFY_NATIVE"
      grep -q '^Timestamp=' <<< "$BOT_DW_VERIFY_DETAILS"
    else
      codesign --verify --strict --test-requirement '=anchor apple generic' "$BOT_DW_VERIFY_NATIVE"
      grep -q '^Authority=Apple Development:' <<< "$BOT_DW_VERIFY_DETAILS"
    fi
  fi
  # Absolute Homebrew/local dependencies would break on a clean user's Mac.
  bash "$(dirname "$0")/verify-native-dependencies.sh" "$BOT_DW_VERIFY_ARCH" "$BOT_DW_VERIFY_NATIVE"
  BOT_DW_VERIFY_COUNT=$((BOT_DW_VERIFY_COUNT+1))
done < <(find "$BOT_DW_VERIFY_ROOT/bin" -type f -name dtw -print0)
[[ "$BOT_DW_VERIFY_COUNT" -eq 1 ]]
# Executing version only: no desktop observation, permission request or input.
node "$(dirname "$0")/verify-desktop-world-manifest.mjs" "$BOT_DW_VERIFY_ROOT" "$("$BOT_DW_VERIFY_ROOT/bin/dtw" version)" --target darwin-arm64 --bundled
