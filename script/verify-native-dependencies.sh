#!/usr/bin/env bash
# Check load commands, not LC_ID_DYLIB: an SDK's build-time self ID is not
# a runtime dependency when Node loads that module by its bundled path.
set -euo pipefail
BOT_NATIVE_VERIFY_COMMANDS=$(otool -arch "${1:?architecture required}" -l "${2:?binary required}")
while IFS= read -r BOT_NATIVE_VERIFY_PATH; do
  case "$BOT_NATIVE_VERIFY_PATH" in
    /usr/lib/*|/System/Library/*|@rpath/*|@loader_path|@loader_path/*|@executable_path|@executable_path/*) ;;
    *) echo "Unbundled native dependency or search path: $BOT_NATIVE_VERIFY_PATH" >&2; exit 1 ;;
  esac
done < <(awk '
  $1 == "cmd" { command = $2 }
  ($1 == "name" && command ~ /^LC_(LOAD|LOAD_WEAK|REEXPORT|LOAD_UPWARD|LAZY_LOAD)_DYLIB$/) ||
  ($1 == "path" && command == "LC_RPATH") {
    sub(/^[[:space:]]*(name|path)[[:space:]]+/, "")
    sub(/ \(offset [0-9]+\)$/, "")
    print
  }
' <<< "$BOT_NATIVE_VERIFY_COMMANDS")
