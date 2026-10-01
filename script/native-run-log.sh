#!/usr/bin/env bash
# Explicit launch output can live in an owned temporary directory when launchd
# cannot open a developer checkout under Documents. Validate before process work.
BOT_LOG="$BOT_ROOT/.cache/$BOT_BUILD_CHANNEL-native-run.log"
if [[ -n "${CAELIS_BOT_NATIVE_LOG:-}" ]]; then
  BOT_LOG="$CAELIS_BOT_NATIVE_LOG"
  if [[ "$BOT_LOG" != /* ]]; then
    echo 'CAELIS_BOT_NATIVE_LOG must be an absolute path.' >&2; exit 1
  fi
  BOT_LOG_PARENT="$(dirname "$BOT_LOG")"
  BOT_LOG_OWNER="$(id -u)"
  if [[ ! -d "$BOT_LOG_PARENT" || -L "$BOT_LOG_PARENT" || "$(stat -f %u "$BOT_LOG_PARENT")" != "$BOT_LOG_OWNER" ]]; then
    echo 'Explicit native log directory must be owned and not a symlink.' >&2; exit 1
  fi
  BOT_LOG_PARENT_MODE="$(stat -f %Lp "$BOT_LOG_PARENT")"
  if (( (8#$BOT_LOG_PARENT_MODE & 077) != 0 )); then
    echo 'Explicit native log directory must be private.' >&2; exit 1
  fi
  if [[ -e "$BOT_LOG" || -L "$BOT_LOG" ]]; then
    if [[ ! -f "$BOT_LOG" || -L "$BOT_LOG" || "$(stat -f %u "$BOT_LOG")" != "$BOT_LOG_OWNER" ]]; then
      echo 'Explicit native log file must be owned, regular, and not a symlink.' >&2; exit 1
    fi
    BOT_LOG_FILE_MODE="$(stat -f %Lp "$BOT_LOG")"
    if (( (8#$BOT_LOG_FILE_MODE & 077) != 0 )); then
      echo 'Explicit native log file must be private.' >&2; exit 1
    fi
  fi
fi
