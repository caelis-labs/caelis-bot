#!/usr/bin/env bash
set -euo pipefail
BOT_CUA_SIGN_BUNDLE=${1:?bundle required}
BOT_CUA_SIGN_IDENTITY=${2:?identity required}
BOT_CUA_SIGN_MODE=${3:-developer-id}
BOT_CUA_SIGN_ROOT="$BOT_CUA_SIGN_BUNDLE/Contents/Resources/ComputerUse"
[[ -d "$BOT_CUA_SIGN_ROOT" ]] || { echo 'Missing Computer Use payload.' >&2; exit 1; }
BOT_CUA_SIGN_ARGS=(--force --sign "$BOT_CUA_SIGN_IDENTITY")
if [[ "$BOT_CUA_SIGN_MODE" != adhoc ]]; then
  BOT_CUA_SIGN_ARGS+=(--options runtime)
  if [[ "$BOT_CUA_SIGN_MODE" == development ]]; then BOT_CUA_SIGN_ARGS+=(--timestamp=none); else BOT_CUA_SIGN_ARGS+=(--timestamp); fi
fi
# All nested native code uses the host signer. The private Node process runs
# --jitless, so no JIT or disabled-library-validation entitlement is introduced.
while IFS= read -r -d '' BOT_CUA_NATIVE; do
  codesign "${BOT_CUA_SIGN_ARGS[@]}" "$BOT_CUA_NATIVE"
  codesign --verify --strict "$BOT_CUA_NATIVE"
done < <(find "$BOT_CUA_SIGN_ROOT" -type f \( -name '*.dylib' -o -name '*.node' -o -name node \) -print0)
