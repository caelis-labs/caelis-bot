#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
output="$BOT_ROOT/.cache/attachment-clipboard-native-test"
mkdir -p "$BOT_ROOT/.cache"
clang -fobjc-arc -fblocks -framework Cocoa internal/desktop/attachment_clipboard_darwin.m script/attachment-clipboard-native-test.m -o "$output"
"$output"
