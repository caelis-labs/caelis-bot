#!/usr/bin/env bash
set -euo pipefail
bundle=${1:?app bundle required}
identity=${2:--}
args=(--force --sign "$identity" --identifier dev.caelis.bot.runtime --options runtime)
if [[ "$identity" != - ]]; then
  if [[ "${3:-release}" == development ]]; then
    args+=(--timestamp=none)
  else
    args+=(--timestamp --keychain "${BOT_SIGN_KEYCHAIN:?}")
  fi
fi
codesign "${args[@]}" "$bundle/Contents/Helpers/caelis"
codesign --verify --strict "$bundle/Contents/Helpers/caelis"
