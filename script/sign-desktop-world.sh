#!/usr/bin/env bash
set -euo pipefail
BOT_DW_SIGN_BUNDLE=${1:?bundle required}
BOT_DW_SIGN_IDENTITY=${2:?identity required}
BOT_DW_SIGN_MODE=${3:-developer-id}
BOT_DW_SIGN_ROOT="$BOT_DW_SIGN_BUNDLE/Contents/Resources/DesktopWorld"
[[ -d "$BOT_DW_SIGN_ROOT" ]] || { echo 'Missing Desktop World payload.' >&2; exit 1; }
BOT_DW_SIGN_ARGS=(--force --sign "$BOT_DW_SIGN_IDENTITY")
if [[ "$BOT_DW_SIGN_MODE" != adhoc ]]; then
  BOT_DW_SIGN_ARGS+=(--options runtime)
  if [[ "$BOT_DW_SIGN_MODE" == development ]]; then BOT_DW_SIGN_ARGS+=(--timestamp=none); else BOT_DW_SIGN_ARGS+=(--timestamp); fi
fi
# The helper is independent native code signed with the host identity.
codesign "${BOT_DW_SIGN_ARGS[@]}" "$BOT_DW_SIGN_ROOT/bin/dtw"
codesign --verify --strict "$BOT_DW_SIGN_ROOT/bin/dtw"
