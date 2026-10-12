#!/usr/bin/env bash
set -euo pipefail
BOT_DW_SIGN_BUNDLE=${1:?bundle required}
BOT_DW_SIGN_IDENTITY=${2:?identity required}
BOT_DW_SIGN_MODE=${3:-developer-id}
BOT_DW_SIGN_ROOT="$BOT_DW_SIGN_BUNDLE/Contents/Resources/Plugins/desktop-world"
[[ -d "$BOT_DW_SIGN_ROOT" ]] || { echo 'Missing Desktop World plugin.' >&2; exit 1; }
BOT_DW_SIGN_ARGS=(--force --sign "$BOT_DW_SIGN_IDENTITY")
if [[ "$BOT_DW_SIGN_MODE" != adhoc ]]; then
  BOT_DW_SIGN_ARGS+=(--options runtime)
  if [[ "$BOT_DW_SIGN_MODE" == development ]]; then BOT_DW_SIGN_ARGS+=(--timestamp=none); else BOT_DW_SIGN_ARGS+=(--timestamp); fi
fi
# Sign the private worker and the stdio adapter before sealing the outer app.
for BOT_DW_SIGN_NATIVE in "$BOT_DW_SIGN_ROOT/libexec/cua-driver" "$BOT_DW_SIGN_ROOT/bin/dtw"; do
  # The upstream private worker carries build-machine Xcode search paths.
  # System Swift libraries resolve through /usr/lib/swift; remove only those
  # absolute build paths before signing the distributed copy.
  while IFS= read -r BOT_DW_SIGN_RPATH; do
    case "$BOT_DW_SIGN_RPATH" in
      /Applications/Xcode*.app/Contents/Developer/Toolchains/*/usr/lib/swift*)
        install_name_tool -delete_rpath "$BOT_DW_SIGN_RPATH" "$BOT_DW_SIGN_NATIVE" ;;
      /usr/lib/swift) ;;
      *) echo "Unexpected Desktop World runtime search path: $BOT_DW_SIGN_RPATH" >&2; exit 1 ;;
    esac
  done < <(otool -l "$BOT_DW_SIGN_NATIVE" | awk '/cmd LC_RPATH/{p=1;next} p&&/path/{print $2; p=0}')
  codesign "${BOT_DW_SIGN_ARGS[@]}" "$BOT_DW_SIGN_NATIVE"
  codesign --verify --strict "$BOT_DW_SIGN_NATIVE"
done
python3 "$(dirname "$0")/stage-desktop-world-plugin.py" inventory darwin-arm64 \
  "$BOT_DW_SIGN_BUNDLE/Contents/Resources/Plugins"
