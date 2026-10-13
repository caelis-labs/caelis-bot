#!/usr/bin/env bash
set -euo pipefail

# Research-only native launcher. Build with BOT_BUILD_CHANNEL=research make build
# first so local development signing can read the selected login keychain.
# Its bundle, process identity, HOME,
# Runtime stores and Bot data are separate from production and existing Dev.
RESEARCH_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
RESEARCH_BUNDLE="$RESEARCH_ROOT/dist/Caelis Bot Research.app"
RESEARCH_PROFILE=${BOT_RESEARCH_PROFILE:-fresh-implementation}
[[ "$RESEARCH_PROFILE" =~ ^[a-z0-9-]+$ ]] || { echo 'Invalid research profile.' >&2; exit 1; }
if [[ "$RESEARCH_PROFILE" == legacy ]]; then
  RESEARCH_PROFILE_ROOT="$RESEARCH_ROOT/.cache/runtime-onboarding-research"
else
  RESEARCH_PROFILE_ROOT="$RESEARCH_ROOT/.cache/runtime-onboarding-research/profiles/$RESEARCH_PROFILE"
fi
RESEARCH_HOME="$RESEARCH_PROFILE_ROOT/home"
RESEARCH_DATA="$RESEARCH_PROFILE_ROOT/data"
RESEARCH_PATH="$RESEARCH_HOME/.local/bin:$RESEARCH_HOME/bin:/usr/bin:/bin:/usr/sbin:/sbin"

[[ -d "$RESEARCH_BUNDLE" ]] || { echo 'Build the Research bundle first.' >&2; exit 1; }
if [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$RESEARCH_BUNDLE/Contents/Info.plist")" != dev.caelis.bot.research ]]; then
  echo 'Refusing to launch a bundle without the research identity.' >&2
  exit 1
fi
for RESEARCH_SYSTEM_CLI in /opt/homebrew/bin/codex /usr/local/bin/codex /opt/homebrew/bin/caelis /usr/local/bin/caelis; do
  if [[ -e "$RESEARCH_SYSTEM_CLI" ]]; then
    echo "Refusing research launch: native discovery can see $RESEARCH_SYSTEM_CLI" >&2
    exit 1
  fi
done
umask 077
mkdir -p "$RESEARCH_HOME/.codex" "$RESEARCH_DATA" "$RESEARCH_HOME/bin"
chmod 700 "$RESEARCH_HOME" "$RESEARCH_HOME/.codex" "$RESEARCH_DATA"

export BOT_BUILD_CHANNEL=research
export BOT_RESEARCH_HOME="$RESEARCH_HOME"
export BOT_RESEARCH_RUNTIME_PATH="$RESEARCH_PATH"
export HOME="$RESEARCH_HOME"
export ZDOTDIR="$RESEARCH_HOME"
export CODEX_HOME="$RESEARCH_HOME/.codex"
export CAELIS_BOT_DATA_DIR="$RESEARCH_DATA"
exec bash "$RESEARCH_ROOT/script/build_and_run.sh" "${1:---restart}"
