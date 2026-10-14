#!/usr/bin/env bash
set -euo pipefail

# PR164 review bundle uses its own identity and disposable local Runtime roots.
# Launch through this script so native startup can reject an unisolated Finder open.
POLISH_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
POLISH_BUNDLE="$POLISH_ROOT/dist/Caelis Bot Dev PR164.app"
POLISH_PROFILE=${BOT_POLISH_PROFILE:-review}
[[ "$POLISH_PROFILE" =~ ^[a-z0-9-]+$ ]] || { echo 'Invalid PR164 profile.' >&2; exit 1; }
POLISH_PROFILE_ROOT="$POLISH_ROOT/.cache/pr164-polish-dev/profiles/$POLISH_PROFILE"
POLISH_HOME="$POLISH_PROFILE_ROOT/home"
POLISH_DATA="$POLISH_PROFILE_ROOT/data"
POLISH_PATH="$POLISH_HOME/.local/bin:$POLISH_HOME/bin:/usr/bin:/bin:/usr/sbin:/sbin"

[[ -d "$POLISH_BUNDLE" ]] || { echo 'Build the PR164 Dev bundle first.' >&2; exit 1; }
if [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$POLISH_BUNDLE/Contents/Info.plist")" != dev.caelis.bot.dev.pr164 ]]; then
  echo 'Refusing to launch a bundle without the PR164 Dev identity.' >&2
  exit 1
fi
for POLISH_SYSTEM_CLI in /opt/homebrew/bin/codex /usr/local/bin/codex /opt/homebrew/bin/caelis /usr/local/bin/caelis; do
  if [[ -e "$POLISH_SYSTEM_CLI" ]]; then
    echo "Refusing isolated Dev launch: native discovery can see $POLISH_SYSTEM_CLI" >&2
    exit 1
  fi
done
for POLISH_OWNED_PATH in "$POLISH_ROOT/.cache/pr164-polish-dev" "$POLISH_ROOT/.cache/pr164-polish-dev/profiles" "$POLISH_PROFILE_ROOT" "$POLISH_HOME" "$POLISH_DATA" "$POLISH_HOME/.codex" "$POLISH_HOME/.local" "$POLISH_HOME/.local/bin" "$POLISH_HOME/bin"; do
  if [[ -L "$POLISH_OWNED_PATH" ]]; then
    echo "Refusing isolated Dev launch through a symlinked profile path: $POLISH_OWNED_PATH" >&2
    exit 1
  fi
done
umask 077
mkdir -p "$POLISH_HOME/.codex" "$POLISH_DATA" "$POLISH_HOME/.local/bin" "$POLISH_HOME/bin"
chmod 700 "$POLISH_HOME" "$POLISH_HOME/.codex" "$POLISH_DATA"

export BOT_BUILD_CHANNEL=polish
export BOT_RESEARCH_HOME="$POLISH_HOME"
export BOT_RESEARCH_RUNTIME_PATH="$POLISH_PATH"
export HOME="$POLISH_HOME"
export ZDOTDIR="$POLISH_HOME"
export CODEX_HOME="$POLISH_HOME/.codex"
export CAELIS_BOT_DATA_DIR="$POLISH_DATA"
exec bash "$POLISH_ROOT/script/build_and_run.sh" "${1:---restart}"
