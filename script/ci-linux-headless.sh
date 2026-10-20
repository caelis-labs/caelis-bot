#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Linux ]]
# No account, model, desktop, system service or existing profile is used.
go test -race ./internal/managedupdate ./internal/headless ./internal/websurface ./internal/localproxy
(umask 077; go test ./internal/managedupdate -count=1)
node --test script/linux-release.test.mjs
npm run build
export BOT_UPDATE_REUSE_FRONTEND=1 BOT_UPDATE_KIND=qa
base="$BOT_ROOT/.cache/ci-linux-managed"
test ! -e "$base"
mkdir -m 700 "$base"
# Ephemeral CI fixture trust only. Removed even on failure, never compiled into
# production or persisted in GitHub/R2 signing configuration.
trap 'rm -f "$base/qa.key"' EXIT
node --input-type=module - "$base" <<'JS'
import {generateKeyPairSync} from 'node:crypto';import {writeFileSync} from 'node:fs';
const {publicKey,privateKey}=generateKeyPairSync('ed25519'),base=process.argv[2];
writeFileSync(base+'/qa.key',privateKey.export({type:'pkcs8',format:'pem'}),{mode:0o600});
writeFileSync(base+'/qa.pub',publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64'),{mode:0o600});
JS
for arch in amd64 arm64; do
 BOT_UPDATE_VERSION=0.17.1-qa.1 BOT_UPDATE_ARCH="$arch" BOT_UPDATE_OUTPUT="$base/$arch-a" bash script/build-managed-headless.sh
 node script/linux-release.mjs prepare "$base/$arch-a" "$arch" "$base/qa.key" "$base/qa.pub"
done
arch="$(go env GOARCH)"
# Compile only the changed entrypoint for B, reusing the verified identical CLI,
# web assets and build cache. BAD is an explicit failed-health fixture ELF.
node script/prepare-linux-qa.mjs "$base/$arch-a" "$base/$arch-b" "$base/$arch-bad" "$arch" "$base/qa.key" "$base/qa.pub"
(umask 077; python3 script/test-managed-update-linux.py --arch "$arch" --a "$base/$arch-a" --b "$base/$arch-b" --bad "$base/$arch-bad" --public-key "$base/qa.pub" --root "/tmp/caelisbot-managed-update-qa-ci-$$")
