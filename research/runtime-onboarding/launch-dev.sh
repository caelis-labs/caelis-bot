#!/usr/bin/env bash
set -euo pipefail

# Onboarding DevBot has a separate bundle identity and an isolated HOME, CLI
# search path, Runtime stores and Bot data. Never open its bundle from Finder.
DEV_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
DEV_BUNDLE="$DEV_ROOT/dist/Caelis Bot Dev Onboarding.app"
DEV_PROFILE=${BOT_ONBOARDING_PROFILE:-fresh}
[[ "$DEV_PROFILE" =~ ^[a-z0-9-]+$ ]] || { echo 'Invalid onboarding profile.' >&2; exit 1; }
DEV_PROFILE_ROOT="$DEV_ROOT/.cache/runtime-onboarding-dev/profiles/$DEV_PROFILE"
DEV_HOME="$DEV_PROFILE_ROOT/home"
DEV_DATA="$DEV_PROFILE_ROOT/data"
DEV_PATH="$DEV_HOME/.local/bin:$DEV_HOME/bin:/usr/bin:/bin:/usr/sbin:/sbin"

[[ -d "$DEV_BUNDLE" ]] || { echo 'Build the onboarding Dev bundle first.' >&2; exit 1; }
if [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$DEV_BUNDLE/Contents/Info.plist")" != dev.caelis.bot.dev.onboarding ]]; then
  echo 'Refusing to launch a bundle without the onboarding Dev identity.' >&2
  exit 1
fi
for DEV_SYSTEM_CLI in /opt/homebrew/bin/codex /usr/local/bin/codex /opt/homebrew/bin/caelis /usr/local/bin/caelis; do
  if [[ -e "$DEV_SYSTEM_CLI" ]]; then
    echo "Refusing isolated Dev launch: native discovery can see $DEV_SYSTEM_CLI" >&2
    exit 1
  fi
done
for DEV_OWNED_PATH in "$DEV_ROOT/.cache/runtime-onboarding-dev" "$DEV_ROOT/.cache/runtime-onboarding-dev/profiles" "$DEV_PROFILE_ROOT" "$DEV_HOME" "$DEV_DATA" "$DEV_HOME/.codex" "$DEV_HOME/.local" "$DEV_HOME/.local/bin" "$DEV_HOME/bin"; do
  if [[ -L "$DEV_OWNED_PATH" ]]; then
    echo "Refusing isolated Dev launch through a symlinked profile path: $DEV_OWNED_PATH" >&2
    exit 1
  fi
done
umask 077
mkdir -p "$DEV_HOME/.codex" "$DEV_DATA" "$DEV_HOME/.local/bin" "$DEV_HOME/bin"
chmod 700 "$DEV_HOME" "$DEV_HOME/.codex" "$DEV_DATA"

export BOT_BUILD_CHANNEL=onboarding
# The existing native launch argument names are retained for both isolated
# preview channels; desktop verifies the channel-specific profile root.
export BOT_RESEARCH_HOME="$DEV_HOME"
export BOT_RESEARCH_RUNTIME_PATH="$DEV_PATH"
export HOME="$DEV_HOME"
export ZDOTDIR="$DEV_HOME"
export CODEX_HOME="$DEV_HOME/.codex"
export CAELIS_BOT_DATA_DIR="$DEV_DATA"
exec bash "$DEV_ROOT/script/build_and_run.sh" "${1:---restart}"
