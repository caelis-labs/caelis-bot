#!/usr/bin/env bash
set -euo pipefail

# Prepare an isolated Caelis profile, or read back the exact Host used by it.
# This script never installs a CLI, changes grants, or starts a service.
MODE=${1:---prepare}
[[ "$MODE" == --prepare || "$MODE" == --verify-host ]] || { echo 'Use --prepare or --verify-host.' >&2; exit 2; }
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PROFILE=${BOT_POLISH_PROFILE:?Set a unique BOT_POLISH_PROFILE for native review}
[[ "$PROFILE" =~ ^[a-z0-9-]+$ ]] || { echo 'Invalid PR164 profile.' >&2; exit 2; }
CORE=${BOT_POLISH_CORE_BINARY:?Set the reviewed Core binary absolute path}
EXPECTED_HASH=${BOT_POLISH_CORE_SHA256:?Set the reviewed Core binary SHA-256}
EXPECTED_COMMIT=${BOT_POLISH_CORE_COMMIT:?Set the reviewed Core source SHA}
[[ "$CORE" == /private/tmp/* && -f "$CORE" && -x "$CORE" && ! -L "$CORE" && "$(realpath "$CORE")" == "$CORE" ]] || {
  echo 'Core must be a direct, executable, isolated /private/tmp artifact.' >&2; exit 2;
}
[[ "$EXPECTED_HASH" =~ ^[0-9a-f]{64}$ && "$EXPECTED_COMMIT" =~ ^[0-9a-f]{40}$ ]] || {
  echo 'Core commit or SHA-256 is incomplete.' >&2; exit 2;
}
PROFILE_ROOT="$ROOT/.cache/pr164-polish-dev/profiles/$PROFILE"
DATA="$PROFILE_ROOT/data"
STORE="$DATA/core-store"
PROFILE_FILE="$DATA/runtime-profiles/caelis.json"
ACTIVE_FILE="$DATA/runtime.json"
PREP_HOME="$PROFILE_ROOT/home"
PREP_TMP="$PROFILE_ROOT/tmp"
for path in "$ROOT/.cache/pr164-polish-dev" "$ROOT/.cache/pr164-polish-dev/profiles" "$PROFILE_ROOT" "$DATA" "$DATA/runtime-profiles" "$PROFILE_FILE" "$ACTIVE_FILE" "$STORE" "$PREP_HOME" "$PREP_HOME/.codex" "$PREP_HOME/.config" "$PREP_TMP"; do
  [[ ! -L "$path" ]] || { echo 'Refusing a symlinked isolated profile path.' >&2; exit 2; }
done
mkdir -p "$PREP_HOME/.codex" "$PREP_HOME/.config" "$PREP_TMP"
chmod 700 "$PREP_HOME" "$PREP_HOME/.codex" "$PREP_TMP"
core_cli() {
  env -i HOME="$PREP_HOME" ZDOTDIR="$PREP_HOME" CODEX_HOME="$PREP_HOME/.codex" XDG_CONFIG_HOME="$PREP_HOME/.config" TMPDIR="$PREP_TMP" PATH=/usr/bin:/bin:/usr/sbin:/sbin "$CORE" "$@"
}
actual_hash=$(shasum -a 256 "$CORE")
actual_hash=${actual_hash%% *}
[[ "$actual_hash" == "$EXPECTED_HASH" ]] || { echo 'Core artifact SHA-256 mismatch.' >&2; exit 2; }
version=$(core_cli version -format json)
build_id=$(printf '%s' "$version" | node -e '
  const fs=require("node:fs"), v=JSON.parse(fs.readFileSync(0,"utf8"));
  if(v.commit!==process.argv[1] || v.build_kind!=="dev" || typeof v.build_id!=="string" || !v.build_id.startsWith(v.commit+"@")) process.exit(2);
  process.stdout.write(v.build_id);
' "$EXPECTED_COMMIT") || { echo 'Core version does not match the reviewed source commit.' >&2; exit 2; }

umask 077
mkdir -p "$DATA/runtime-profiles"
chmod 700 "$PROFILE_ROOT" "$DATA" "$DATA/runtime-profiles"
node - "$CORE" "$STORE" "$PROFILE_FILE" "$ACTIVE_FILE" <<'NODE'
const fs=require('node:fs');
const [core,store,file,active]=process.argv.slice(2);
const expected={version:1,runtime:'caelis',cliPath:core,caelisStore:store};
if(!fs.existsSync(file)) {
  try { fs.writeFileSync(file,JSON.stringify(expected)+'\n',{flag:'wx',mode:0o600}); }
  catch(error) { if(error.code!=='EEXIST') throw error; }
}
const saved=JSON.parse(fs.readFileSync(file,'utf8'));
for(const [key,value] of Object.entries(expected)) if(saved[key]!==value) throw new Error('Isolated Caelis profile differs from the verified Core artifact');
if(fs.existsSync(active)) {
  const current=JSON.parse(fs.readFileSync(active,'utf8'));
  if(current.runtime==='caelis' && (current.cliPath!==core || current.caelisStore!==store)) throw new Error('Active Caelis Runtime points to a different binary or Store');
}
NODE

# The public lifecycle status is read only. A running Host must identify the
# same process bytes; a stopped fresh Store is suitable only for preparation.
status=$(core_cli service status --store-dir "$STORE" --format json)
fields=$(printf '%s' "$status" | node -e '
  const fs=require("node:fs"), v=JSON.parse(fs.readFileSync(0,"utf8"));
  if(v.state==="stopped") {process.stdout.write("stopped 0"); process.exit(0);}
  if(v.state!=="running" || v.build_id!==process.argv[1] || v.build_kind!=="dev" || !Number.isSafeInteger(v.pid) || v.pid<=1 || !v.instance_id) process.exit(2);
  process.stdout.write("running "+v.pid);
' "$build_id") || { echo 'Isolated Store reports a different or unverified Host.' >&2; exit 2; }
read -r state pid <<< "$fields"
if [[ "$state" == stopped ]]; then
  [[ "$MODE" == --prepare ]] || { echo 'Isolated Core Host is not running.' >&2; exit 1; }
  echo "Isolated Core profile ready: $PROFILE_FILE / Core $EXPECTED_COMMIT / SHA-256 $EXPECTED_HASH / Host stopped"
  exit 0
fi
process_path=$(ps -ww -p "$pid" -o comm=)
[[ "$process_path" == "$CORE" ]] || { echo 'Running Host executable is not the pinned Core path.' >&2; exit 1; }
process_hash=$(shasum -a 256 "$process_path")
process_hash=${process_hash%% *}
[[ "$process_hash" == "$EXPECTED_HASH" ]] || { echo 'Running Host executable bytes differ from the pinned Core artifact.' >&2; exit 1; }
echo "Isolated Core Host verified: PID $pid / Core $EXPECTED_COMMIT / SHA-256 $EXPECTED_HASH / build_id $build_id"
