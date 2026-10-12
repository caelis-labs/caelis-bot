#!/usr/bin/env bash
set -euo pipefail
BOT_UI_REVIEW_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export BOT_BUILD_CHANNEL=development
export CAELIS_BOT_DATA_DIR="$BOT_UI_REVIEW_ROOT/.cache/ui-renovation-devbot"
exec bash "$BOT_UI_REVIEW_ROOT/script/build_and_run.sh" "${1:---restart}"
