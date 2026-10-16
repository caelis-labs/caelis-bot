#!/usr/bin/env bash
set -euo pipefail
bundle=${1:?app bundle required}
mode=${2:?signing mode required}
binary="$bundle/Contents/Helpers/caelis"
[[ -f "$binary" && ! -L "$binary" && -x "$binary" ]]
codesign --verify --strict "$binary"
details=$(codesign -dvv "$binary" 2>&1)
grep -Fxq 'Identifier=dev.caelis.bot.runtime' <<< "$details"
grep -q 'flags=.*runtime' <<< "$details"
case "$mode" in
  adhoc) grep -q '^Signature=adhoc$' <<< "$details" ;;
  development|developer-id)
    team=$(codesign -dvv "$bundle" 2>&1 | sed -n 's/^TeamIdentifier=//p')
    [[ "$team" =~ ^[A-Z0-9]{10}$ ]]
    codesign --verify --strict --test-requirement "=anchor apple generic and certificate leaf[subject.OU] = \"$team\"" "$binary"
    if [[ "$mode" == developer-id ]]; then
      grep -q '^Authority=Developer ID Application:' <<< "$details"
      grep -q '^Timestamp=' <<< "$details"
    fi ;;
  *) exit 1 ;;
esac
bash "$(dirname "$0")/verify-native-dependencies.sh" arm64 "$binary"
python3 "$(dirname "$0")/caelis-runtime.py" verify "$bundle"
