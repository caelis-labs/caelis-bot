#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory="$(mktemp -d "${TMPDIR:-/tmp}/bot-machine-settings.XXXXXX")"
trap 'rm -rf "$directory"' EXIT
clang -fobjc-arc -fblocks -framework Cocoa -framework WebKit script/machine-settings-native-test.m -o "$directory/fixture"
node script/machine-settings-native-test.mjs "$directory/fixture"
