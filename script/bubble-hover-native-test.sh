#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
mkdir -p "$BOT_ROOT/.cache"
clang -fobjc-arc -fblocks -Werror -I internal/desktop -framework Cocoa script/bubble-hover-native-test.m internal/desktop/bubble_hover_darwin.m -o "$BOT_ROOT/.cache/bubble-hover-native-test"
"$BOT_ROOT/.cache/bubble-hover-native-test"
