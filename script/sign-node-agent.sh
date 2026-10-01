#!/usr/bin/env bash
# Sign only the bundled native helper, then seal its exact bytes in the manifest.
# Linux payloads are data resources and are never passed to macOS codesign.
set -euo pipefail
BOT_AGENT_BUNDLE=${1:?APP bundle required}
BOT_AGENT_IDENTITY=${2:--}
BOT_AGENT_MODE=${3:-release}
BOT_AGENT_DIRECTORY="$BOT_AGENT_BUNDLE/Contents/Resources/NodeAgent"
[[ -d "$BOT_AGENT_DIRECTORY" ]] || exit 0
BOT_AGENT_SIGN_ARGS=(--force --sign "$BOT_AGENT_IDENTITY" --options runtime)
if [[ "$BOT_AGENT_IDENTITY" != - ]]; then
  if [[ "$BOT_AGENT_MODE" == development ]]; then
    BOT_AGENT_SIGN_ARGS+=(--timestamp=none)
  else
    BOT_AGENT_SIGN_ARGS+=(--timestamp --keychain "${BOT_SIGN_KEYCHAIN:?}")
  fi
fi
for BOT_AGENT_NATIVE in "$BOT_AGENT_DIRECTORY"/caelis-agent-darwin-* "$BOT_AGENT_DIRECTORY"/caelis-node-darwin-*; do
  [[ -f "$BOT_AGENT_NATIVE" ]] || { echo 'Native node helper missing.' >&2; exit 1; }
  codesign "${BOT_AGENT_SIGN_ARGS[@]}" "$BOT_AGENT_NATIVE"
  codesign --verify --strict "$BOT_AGENT_NATIVE"
done
node --input-type=module - "$BOT_AGENT_DIRECTORY" <<'NODE'
import {createHash} from 'node:crypto';
import {readFileSync,writeFileSync,readdirSync} from 'node:fs';
import {join} from 'node:path';
const directory=process.argv[2], file=join(directory,'manifest.json');
const manifest=JSON.parse(readFileSync(file,'utf8'));
manifest.artifacts=manifest.artifacts.filter(a=>a.os!=='darwin');
for(const name of readdirSync(directory).filter(n=>/^caelis-(agent|node)-darwin-(amd64|arm64)$/.test(n))) {
 manifest.artifacts.push({os:'darwin',arch:name.split('-').at(-1),file:name,sha256:createHash('sha256').update(readFileSync(join(directory,name))).digest('hex')});
}
writeFileSync(file,JSON.stringify(manifest)+'\n');
NODE
