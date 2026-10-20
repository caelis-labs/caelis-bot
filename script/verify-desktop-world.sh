#!/usr/bin/env bash
set -euo pipefail
BOT_DW_VERIFY_BUNDLE=${1:?bundle required}
BOT_DW_VERIFY_MODE=${2:?signing mode required}
BOT_DW_VERIFY_PLUGINS="$BOT_DW_VERIFY_BUNDLE/Contents/Resources/Plugins"
BOT_DW_VERIFY_ROOT="$BOT_DW_VERIFY_PLUGINS/desktop-world"
BOT_DW_VERIFY_PIN="$(cd "$(dirname "$0")/.." && pwd)/resources/desktop-world/release.json"
BOT_DW_VERIFY_EXPECTED=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$BOT_DW_VERIFY_PIN")
BOT_DW_VERIFY_VERSION=$(/usr/libexec/PlistBuddy -c 'Print CaelisDesktopWorldVersion' "$BOT_DW_VERIFY_BUNDLE/Contents/Info.plist" 2>/dev/null || true)
[[ "$BOT_DW_VERIFY_VERSION" == "$BOT_DW_VERIFY_EXPECTED" && -f "$BOT_DW_VERIFY_PLUGINS/desktop-world-inventory.json" ]] || { echo "Missing Desktop World $BOT_DW_VERIFY_EXPECTED plugin." >&2; exit 1; }
case "$BOT_DW_VERIFY_MODE" in adhoc|development|developer-id) ;; *) exit 1 ;; esac
BOT_DW_VERIFY_ARCH=$(lipo -archs "$BOT_DW_VERIFY_BUNDLE/Contents/MacOS/caelis-bot")
[[ "$BOT_DW_VERIFY_ARCH" == arm64 ]] || { echo 'Unsupported host architecture.' >&2; exit 1; }
BOT_DW_VERIFY_TEAM=$(codesign -dvv "$BOT_DW_VERIFY_BUNDLE" 2>&1 | sed -n 's/^TeamIdentifier=//p')
for BOT_DW_VERIFY_NATIVE in "$BOT_DW_VERIFY_ROOT/bin/dtw" "$BOT_DW_VERIFY_ROOT/libexec/cua-driver"; do
  [[ -x "$BOT_DW_VERIFY_NATIVE" ]] || { echo 'Desktop World native executable mode missing.' >&2; exit 1; }
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
      grep -q '^Timestamp=' <<< "$BOT_DW_VERIFY_DETAILS"
    fi
  fi
  bash "$(dirname "$0")/verify-native-dependencies.sh" arm64 "$BOT_DW_VERIFY_NATIVE"
done
python3 - "$BOT_DW_VERIFY_PLUGINS" "$BOT_DW_VERIFY_EXPECTED" <<'PY'
import hashlib,json,sys
from pathlib import Path
root=Path(sys.argv[1]); package=root/'desktop-world'
entry=json.loads((root/'desktop-world-inventory.json').read_text())
actual={p.relative_to(package).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in package.rglob('*') if p.is_file()}
assert entry['ID']=='desktop-world' and entry['Version']==sys.argv[2].removeprefix('v')
assert entry['Source']=='bundled:desktop-world/'+sys.argv[2]
assert actual==entry['Files'], 'Desktop World signed package inventory differs'
print(f'Verified complete signed Desktop World plugin: {len(actual)} files')
PY
node "$(dirname "$0")/verify-desktop-world-version.mjs" "$BOT_DW_VERIFY_ROOT/bin/dtw"
