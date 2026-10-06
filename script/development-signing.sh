#!/usr/bin/env bash
# Sourced by build.sh. Only an explicit local selection enables development
# signing; CI/PR builds never consume that selection or release credentials.
BOT_BUILD_SIGN_IDENTITY=-
BOT_BUILD_SIGN_MODE=adhoc
if [[ -z "${CI:-}" || "${CI:-}" == false ]]; then
  if [[ "${BOT_DEVELOPMENT_IDENTITY+x}" == x ]]; then
    BOT_BUILD_SIGN_IDENTITY="$BOT_DEVELOPMENT_IDENTITY"
  elif [[ -f "$BOT_ROOT/.development-signing-identity" ]]; then
    BOT_BUILD_SIGN_IDENTITY=$(cat "$BOT_ROOT/.development-signing-identity")
  elif [[ -f "$BOT_ROOT/.git" ]]; then
    # A linked worktree has its own ignored files. Reuse only the main checkout's
    # explicit local selection from this same Git repository; never pick a
    # certificate merely because it happens to be installed.
    BOT_BUILD_COMMON_DIR=$(git -C "$BOT_ROOT" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)
    if [[ "$BOT_BUILD_COMMON_DIR" == */.git && -d "$BOT_BUILD_COMMON_DIR" ]]; then
      BOT_BUILD_MAIN_ROOT=${BOT_BUILD_COMMON_DIR%/.git}
      if [[ "$BOT_BUILD_MAIN_ROOT" != "$BOT_ROOT" && -f "$BOT_BUILD_MAIN_ROOT/.development-signing-identity" ]]; then
        BOT_BUILD_SIGN_IDENTITY=$(cat "$BOT_BUILD_MAIN_ROOT/.development-signing-identity")
      fi
    fi
    unset BOT_BUILD_COMMON_DIR BOT_BUILD_MAIN_ROOT
  fi
fi
if [[ "$BOT_BUILD_SIGN_IDENTITY" != - ]]; then
  [[ "$BOT_BUILD_SIGN_IDENTITY" =~ ^[A-Fa-f0-9]{40}$ ]] || {
    echo 'Development signing requires an exact certificate SHA-1 fingerprint, or - for ad-hoc.' >&2; exit 1;
  }
  BOT_BUILD_SIGN_IDENTITY=$(printf '%s' "$BOT_BUILD_SIGN_IDENTITY" | tr '[:lower:]' '[:upper:]')
  BOT_BUILD_IDENTITY_LIST=$(security find-identity -v -p codesigning)
  if ! awk -v fingerprint="$BOT_BUILD_SIGN_IDENTITY" '$2 == fingerprint && /"Apple Development:/ { found=1 } END { exit !found }' <<< "$BOT_BUILD_IDENTITY_LIST"; then
    echo 'Selected Apple Development identity is unavailable. No fallback or release certificate will be used.' >&2
    exit 1
  fi
  unset BOT_BUILD_IDENTITY_LIST
  BOT_BUILD_SIGN_MODE=development
fi
