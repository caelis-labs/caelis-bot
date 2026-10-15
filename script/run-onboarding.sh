#!/usr/bin/env bash
set -euo pipefail
BOT_ONBOARDING_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export BOT_BUILD_CHANNEL=onboarding
export BOT_ONBOARDING_ROOT="${BOT_ONBOARDING_ROOT:-$BOT_ONBOARDING_REPO/.cache/onboarding-experience}"
[[ "$BOT_ONBOARDING_ROOT" == /* ]] || { echo 'Use an absolute onboarding root.' >&2; exit 1; }
mkdir -p "$BOT_ONBOARDING_ROOT/home" "$BOT_ONBOARDING_ROOT/data" "$BOT_ONBOARDING_ROOT/codex"
# Keep the login-shell discovery pass within this disposable Runtime home.
printf '%s\n' 'export PATH="$HOME/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin"' > "$BOT_ONBOARDING_ROOT/home/.bash_profile"
export CAELIS_BOT_DATA_DIR="$BOT_ONBOARDING_ROOT/data"
export CODEX_HOME="$BOT_ONBOARDING_ROOT/codex"
exec bash "$BOT_ONBOARDING_REPO/script/build_and_run.sh" "${1:---restart}"
