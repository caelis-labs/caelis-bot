#!/usr/bin/env bash
set -euo pipefail
[[ "$(uname -s)" == Darwin ]] || exit 0
BOT_LOG_TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/bot-native-log.XXXXXX")"
trap 'rm -rf "$BOT_LOG_TEST_ROOT"' EXIT
BOT_LOG_TEST_HELPER="$(cd "$(dirname "$0")" && pwd)/native-run-log.sh"
validate() {
  CAELIS_BOT_NATIVE_LOG="$1" BOT_ROOT="$BOT_LOG_TEST_ROOT" BOT_BUILD_CHANNEL=development \
    bash -c 'source "$1"; [[ "$BOT_LOG" == "$2" ]]' _ "$BOT_LOG_TEST_HELPER" "${1:-$BOT_LOG_TEST_ROOT/.cache/development-native-run.log}"
}
reject() { if validate "$1" 2>/dev/null; then echo "Unsafe native log accepted: $1" >&2; exit 1; fi; }
validate ''
validate "$BOT_LOG_TEST_ROOT/native.log"
reject 'relative.log'
mkdir "$BOT_LOG_TEST_ROOT/shared"
chmod 755 "$BOT_LOG_TEST_ROOT/shared"
reject "$BOT_LOG_TEST_ROOT/shared/native.log"
ln -s "$BOT_LOG_TEST_ROOT" "$BOT_LOG_TEST_ROOT/linked-directory"
reject "$BOT_LOG_TEST_ROOT/linked-directory/native.log"
(umask 077; echo retained > "$BOT_LOG_TEST_ROOT/native.log")
validate "$BOT_LOG_TEST_ROOT/native.log"
[[ "$(cat "$BOT_LOG_TEST_ROOT/native.log")" == retained ]]
ln -s "$BOT_LOG_TEST_ROOT/native.log" "$BOT_LOG_TEST_ROOT/linked.log"
reject "$BOT_LOG_TEST_ROOT/linked.log"
chmod 644 "$BOT_LOG_TEST_ROOT/native.log"
reject "$BOT_LOG_TEST_ROOT/native.log"
echo 'Native log default and private override validated; shared and symlink destinations rejected without truncation.'
