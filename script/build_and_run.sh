#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'Native run is implemented for macOS only; see docs/development.md.' >&2
  exit 1
fi
BOT_MODE="${1:-run}"
if [[ "$BOT_MODE" == --verify-signed ]]; then export BOT_BUILD_CHANNEL=release; fi
source "$BOT_ROOT/script/app-identity.sh"
if [[ "$BOT_MODE" == --desktop-control-preview ]]; then
  exec bash "$BOT_ROOT/experiments/desktop-control/run-native.sh"
fi
if [[ "$BOT_MODE" == --capture-preview ]]; then
  BOT_CAPTURE_LIVE=1 exec bash "$BOT_ROOT/script/capture-native-test.sh"
fi
if [[ "$BOT_MODE" == --bubble-preview ]]; then
  exec bash "$BOT_ROOT/script/bubble-preview.sh"
fi
if [[ "$BOT_MODE" == --chat-preview ]]; then
  exec bash "$BOT_ROOT/script/chat-preview.sh"
fi
if [[ "$BOT_MODE" == --task-dock-preview ]]; then
  # A native panel fixture with disposable tasks, independent of the daily Bot.
  BOT_TASK_DOCK_LIVE=1 exec bash "$BOT_ROOT/script/task-dock-native-test.sh"
fi
case "$BOT_MODE" in run|--verify|--verify-signed|--debug|--logs|--telemetry|--recall|--restart|--terminal-smoke|--remote-terminal-smoke|--desktop-capture-smoke) ;; *)
  echo "Usage: $0 [--verify|--verify-signed|--debug|--logs|--telemetry|--recall|--restart|--capture-preview|--task-dock-preview|--bubble-preview|--chat-preview|--desktop-control-preview|--desktop-capture-smoke TITLE EVIDENCE PNG ONCE|--terminal-smoke terminal iterm2 ghostty]" >&2; exit 2 ;;
esac
if [[ "$BOT_MODE" == --verify-signed ]]; then
  # Keep the distribution signature intact during native release acceptance.
  bash "$BOT_ROOT/script/verify-signature.sh" "$BOT_BUNDLE" developer-id
fi
if [[ "$BOT_MODE" == --recall ]]; then
  # Exercise the same single-instance recall path as opening the installed app again.
  exec /usr/bin/open -g -n "$BOT_BUNDLE"
fi
owned_pids() {
  for BOT_PID in $(pgrep -x caelis-bot || true); do
    if [[ "$(ps -ww -p "$BOT_PID" -o comm=)" == "$BOT_BUNDLE/Contents/MacOS/caelis-bot" ]]; then
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
# Preserve the exact signed bytes when restarting after an OS permission change.
if [[ "$BOT_MODE" != --verify-signed && "$BOT_MODE" != --restart && "$BOT_MODE" != --terminal-smoke && "$BOT_MODE" != --remote-terminal-smoke && "$BOT_MODE" != --desktop-capture-smoke ]]; then ./script/build.sh; fi
if [[ "$BOT_MODE" == --debug ]]; then
  exec lldb -- "$BOT_BUNDLE/Contents/MacOS/caelis-bot"
fi
BOT_LOG="$BOT_ROOT/.cache/$BOT_BUILD_CHANNEL-native-run.log"
: > "$BOT_LOG"
BOT_OPEN_ARGS=(-g -n "$BOT_BUNDLE" --stdout "$BOT_LOG" --stderr "$BOT_LOG")
if [[ "$BOT_MODE" == --desktop-capture-smoke ]]; then
  shift
  if [[ $# != 4 ]]; then echo 'Specify exact disposable window title, private evidence/image paths and once marker.' >&2; exit 2; fi
  [[ "$BOT_BUILD_CHANNEL" == development ]] || { echo 'Desktop capture smoke requires the isolated Dev bundle.' >&2; exit 2; }
  bash "$BOT_ROOT/script/verify-signature.sh" "$BOT_BUNDLE" development app "$BOT_APP_ID"
  /usr/bin/open "${BOT_OPEN_ARGS[@]}" --args --desktop-capture-smoke "$@"
  for ((BOT_ATTEMPT=0; BOT_ATTEMPT<900; BOT_ATTEMPT++)); do
    if rg -q 'DESKTOP CAPTURE E2E PASS' "$BOT_LOG"; then cat "$BOT_LOG"; exit 0; fi
    if rg -q 'DESKTOP CAPTURE E2E FAIL' "$BOT_LOG"; then cat "$BOT_LOG"; exit 1; fi
    sleep 0.1
  done
  echo "Desktop capture acceptance did not finish; see $BOT_LOG." >&2
  exit 1
fi
if [[ "$BOT_MODE" == --remote-terminal-smoke ]]; then
  shift
  if [[ $# != 3 ]]; then echo 'Specify disposable profile directory, original task and terminal.' >&2; exit 2; fi
  /usr/bin/open "${BOT_OPEN_ARGS[@]}" --args --remote-terminal-smoke "$@"
  for ((BOT_ATTEMPT=0; BOT_ATTEMPT<900; BOT_ATTEMPT++)); do
    if rg -q 'REMOTE TERMINAL E2E PASS' "$BOT_LOG"; then cat "$BOT_LOG"; exit 0; fi
    if rg -q 'original.*unavailable|observer.*changed|Error|error=' "$BOT_LOG"; then cat "$BOT_LOG"; exit 1; fi
    sleep 0.5
  done
  echo "Remote terminal acceptance did not finish; see $BOT_LOG." >&2; exit 1
fi
if [[ "$BOT_MODE" == --terminal-smoke ]]; then
  shift
  /usr/bin/open "${BOT_OPEN_ARGS[@]}" --args --terminal-smoke "$@"
  for ((BOT_ATTEMPT=0; BOT_ATTEMPT<3000; BOT_ATTEMPT++)); do
    if rg -q 'TERMINAL E2E PASS' "$BOT_LOG"; then cat "$BOT_LOG"; exit 0; fi
    if rg -q 'TERMINAL E2E FAIL' "$BOT_LOG"; then cat "$BOT_LOG"; exit 1; fi
    sleep 0.1
  done
  echo "Terminal acceptance did not finish; see $BOT_LOG." >&2
  exit 1
fi
if [[ "${CAELIS_BOT_DATA_DIR+x}" == x ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_BOT_DATA_DIR=$CAELIS_BOT_DATA_DIR")
fi
if [[ -n "${CAELIS_BOT_DESKTOP_TRACE:-}" ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_BOT_DESKTOP_TRACE=$CAELIS_BOT_DESKTOP_TRACE")
fi
if [[ "${CAELIS_BOT_WINDOW_TRACE:-}" == 1 ]]; then
  BOT_OPEN_ARGS+=(--env "CAELIS_BOT_WINDOW_TRACE=1")
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
if [[ -n "${CODEX_HOME:-}" ]]; then
  BOT_OPEN_ARGS+=(--env "CODEX_HOME=$CODEX_HOME")
fi
/usr/bin/open "${BOT_OPEN_ARGS[@]}"
case "$BOT_MODE" in
  --verify|--verify-signed|--restart)
    # Bounded startup observation, not a workaround for an application race.
    for ((BOT_ATTEMPT=0; BOT_ATTEMPT<50; BOT_ATTEMPT++)); do
      if pgrep -x caelis-bot >/dev/null && rg -q "Desktop native host ready" "$BOT_LOG"; then
        echo 'Caelis Bot native host is ready; inspect its surfaces separately.'
        exit 0
      fi
      sleep 0.1
    done
    echo "Caelis Bot did not become ready within 5 seconds; see $BOT_LOG." >&2; exit 1 ;;
  --logs|--telemetry)
    exec /usr/bin/log stream --info --style compact --predicate 'process == "caelis-bot"' ;;
esac
