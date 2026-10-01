#!/usr/bin/env bash
# Package only native source from this checkout, without GUI/APP dependencies.
set -euo pipefail
source "$(dirname "$0")/env.sh"
BOT_AGENT_OUTPUT=${1:?Usage: build-node-agent.sh OUTPUT_DIRECTORY SOURCE_COMMIT}
BOT_AGENT_SOURCE=${2:?Source commit required}
[[ "$BOT_AGENT_SOURCE" =~ ^[a-f0-9]{40}$ ]] || { echo 'Invalid node-agent source commit.' >&2; exit 1; }
[[ "$BOT_AGENT_SOURCE" == "$(git rev-parse HEAD)" ]] || { echo 'Node-agent source differs from APP source.' >&2; exit 1; }
mkdir -p "$BOT_AGENT_OUTPUT"
for BOT_AGENT_ARCH in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$BOT_AGENT_ARCH" go build -trimpath -o "$BOT_AGENT_OUTPUT/caelis-agent-linux-$BOT_AGENT_ARCH" ./cmd/caelis-agent
done
BOT_AGENT_NATIVE_ARCH=$(go env GOARCH)
CGO_ENABLED=0 GOOS=darwin GOARCH="$BOT_AGENT_NATIVE_ARCH" go build -trimpath -o "$BOT_AGENT_OUTPUT/caelis-agent-darwin-$BOT_AGENT_NATIVE_ARCH" ./cmd/caelis-agent
node --input-type=module - "$BOT_AGENT_OUTPUT" "$BOT_AGENT_SOURCE" <<'NODE'
import {createHash} from 'node:crypto';
import {readFileSync, writeFileSync} from 'node:fs';
import {join} from 'node:path';
const [directory, sourceRevision] = process.argv.slice(2);
const artifacts = ['amd64', 'arm64'].map(arch => {
  const file = `caelis-agent-linux-${arch}`;
  return {os: 'linux', arch, file, sha256: createHash('sha256').update(readFileSync(join(directory, file))).digest('hex')};
});
writeFileSync(join(directory, 'manifest.json'), JSON.stringify({version: 1, sourceRevision, artifacts})+'\n');
NODE
