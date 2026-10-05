#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
directory="$(mktemp -d "${TMPDIR:-/tmp}/bot-settings-extras.XXXXXX")"
trap 'rm -rf "$directory"' EXIT
clang -fobjc-arc -fblocks -framework Cocoa -framework WebKit script/settings-extras-native-fixture.m -o "$directory/fixture"
node script/settings-extras-native-fixture.mjs "$directory/fixture" "$@"
