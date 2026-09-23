#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'Native run is implemented for macOS only; see docs/platform-baseline.md.' >&2
  exit 1
fi
BOT_MODE="${1:-run}"
case "$BOT_MODE" in run|--verify|--verify-signed|--debug|--logs|--telemetry|--recall) ;; *)
  echo "Usage: $0 [--verify|--verify-signed|--debug|--logs|--telemetry|--recall]" >&2; exit 2 ;;
esac
if [[ "$BOT_MODE" == --verify-signed ]]; then
  # Keep the distribution signature intact during native release acceptance.
  bash "$BOT_ROOT/script/verify-signature.sh" "$BOT_ROOT/dist/Caelis Bot.app" developer-id
fi
if [[ "$BOT_MODE" == --recall ]]; then
  # Exercise the same single-instance recall path as opening the installed app again.
  exec /usr/bin/open -g -n "$BOT_ROOT/dist/Caelis Bot.app"
fi
owned_pids() {
  for BOT_PID in $(pgrep -x caelis-bot || true); do
    if [[ "$(ps -ww -p "$BOT_PID" -o comm=)" == "$BOT_ROOT/dist/Caelis Bot.app/Contents/MacOS/caelis-bot" ]]; then
      echo "$BOT_PID"
    fi
  done
}
for BOT_PID in $(owned_pids); do kill -TERM "$BOT_PID"; done
# SIGTERM now follows the same interrupt/terminal-cleanup path as explicit quit.
# Never SIGKILL an active task just to make a development restart look successful.
for ((BOT_ATTEMPT=0; BOT_ATTEMPT<300; BOT_ATTEMPT++)); do
  if [[ -z "$(owned_pids)" ]]; then break; fi
  sleep 0.1
done
if [[ -n "$(owned_pids)" ]]; then
  echo 'Previous Caelis Bot process did not shut down; refusing to launch another owner.' >&2
  exit 1
fi
if [[ "$BOT_MODE" != --verify-signed ]]; then ./script/build.sh; fi
BOT_BUNDLE="$BOT_ROOT/dist/Caelis Bot.app"
if [[ "$BOT_MODE" == --debug ]]; then
  exec lldb -- "$BOT_BUNDLE/Contents/MacOS/caelis-bot"
fi
BOT_LOG="$BOT_ROOT/.cache/native-run.log"
: > "$BOT_LOG"
BOT_OPEN_ARGS=(-g -n "$BOT_BUNDLE" --stdout "$BOT_LOG" --stderr "$BOT_LOG")
if [[ "${CAELIS_BOT_DATA_DIR+x}" == x ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_BOT_DATA_DIR=$CAELIS_BOT_DATA_DIR")
fi
if [[ -n "${CAELIS_BOT_DESKTOP_TRACE:-}" ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_BOT_DESKTOP_TRACE=$CAELIS_BOT_DESKTOP_TRACE")
fi
if [[ "${CAELIS_BOT_BEHAVIOR_PREVIEW:-}" == 1 ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_BOT_BEHAVIOR_PREVIEW=1")
fi
# Match Finder's environment by default; exercise local installation discovery.
# An explicit developer override remains available for isolated acceptance runs.
if [[ -n "${CODEX_BIN:-}" ]]; then
  BOT_CODEX_RESOLVED="$(command -v "$CODEX_BIN")"
  BOT_OPEN_ARGS+=(--env "CODEX_BIN=$BOT_CODEX_RESOLVED")
fi
if [[ -n "${CAELIS_CODEX_SOCKET:-}" ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_CODEX_SOCKET=$CAELIS_CODEX_SOCKET")
fi
/usr/bin/open "${BOT_OPEN_ARGS[@]}"
case "$BOT_MODE" in
  --verify|--verify-signed)
    # Bounded startup observation, not a workaround for an application race.
    for ((BOT_ATTEMPT=0; BOT_ATTEMPT<50; BOT_ATTEMPT++)); do
      if pgrep -x caelis-bot >/dev/null && rg -q "Desktop native host ready" "$BOT_LOG"; then
        echo 'Caelis Bot native host is ready; inspect its surfaces separately.'
        exit 0
      fi
      sleep 0.1
    done
    echo 'Caelis Bot did not become ready within 5 seconds; see .cache/native-run.log.' >&2; exit 1 ;;
  --logs|--telemetry)
    exec /usr/bin/log stream --info --style compact --predicate 'process == "caelis-bot"' ;;
esac
