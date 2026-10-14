#!/usr/bin/env bash
set -euo pipefail

# Run only after the coordinator names the final Bot/Core commits and Core
# binary. All Host/Store/gateway state in these tests is synthetic and local.
BOT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
required=(BOT_EXPECTED_SHA CORE_WORKTREE CORE_EXPECTED_SHA CORE_ELIGIBILITY_SHA CORE_PERMISSION_SHA CORE_BINARY CORE_BINARY_SHA256)
for name in "${required[@]}"; do
  [[ -n "${!name:-}" ]] || { echo "Set $name to the reviewed final value." >&2; exit 2; }
done
[[ "$(uname -s)" == Darwin ]] || { echo 'This contract run requires macOS.' >&2; exit 2; }
[[ "$CORE_WORKTREE" == /* && -d "$CORE_WORKTREE" ]] || { echo 'CORE_WORKTREE must be an absolute directory.' >&2; exit 2; }
[[ "$CORE_BINARY" == /* && -x "$CORE_BINARY" ]] || { echo 'CORE_BINARY must be an absolute executable path.' >&2; exit 2; }
[[ "$CORE_BINARY" == /private/tmp/* || "$CORE_BINARY" == "$BOT_ROOT"/.cache/* ]] || {
  echo 'CORE_BINARY must be an isolated artifact under /private/tmp or this worktree cache.' >&2; exit 2;
}
[[ ! -L "$CORE_BINARY" ]] || { echo 'CORE_BINARY must not be a symlink.' >&2; exit 2; }
for value in "$BOT_EXPECTED_SHA" "$CORE_EXPECTED_SHA" "$CORE_ELIGIBILITY_SHA" "$CORE_PERMISSION_SHA" "$CORE_BINARY_SHA256"; do
  [[ "$value" =~ ^[0-9a-f]{40}$ || "$value" =~ ^[0-9a-f]{64}$ ]] || { echo 'Expected a full lowercase commit or binary SHA.' >&2; exit 2; }
done
[[ ${#BOT_EXPECTED_SHA} == 40 && ${#CORE_EXPECTED_SHA} == 40 && ${#CORE_ELIGIBILITY_SHA} == 40 && ${#CORE_PERMISSION_SHA} == 40 && ${#CORE_BINARY_SHA256} == 64 ]] || {
  echo 'Commit SHAs must have 40 characters and the binary SHA-256 must have 64.' >&2; exit 2;
}
[[ "$CORE_EXPECTED_SHA" != 1a956d256983c45b07ab255f62e4738602877bf1 &&
   "$CORE_PERMISSION_SHA" != 1a956d256983c45b07ab255f62e4738602877bf1 &&
   "$CORE_BINARY_SHA256" != c32ab38092ddbc9df62695a3493af50ab709288a9ce831acf3d925488a637a30 ]] || {
  echo 'The older Core checkpoint cannot qualify connected-model readiness.' >&2; exit 2;
}
[[ "$(git -C "$BOT_ROOT" rev-parse HEAD)" == "$BOT_EXPECTED_SHA" ]] || { echo 'Bot HEAD differs from the reviewed SHA.' >&2; exit 2; }
[[ "$(git -C "$CORE_WORKTREE" rev-parse HEAD)" == "$CORE_EXPECTED_SHA" ]] || { echo 'Core HEAD differs from the reviewed SHA.' >&2; exit 2; }
[[ -z "$(git -C "$BOT_ROOT" status --porcelain)" && -z "$(git -C "$CORE_WORKTREE" status --porcelain)" ]] || {
  echo 'Both source worktrees must be clean for an exact-SHA contract run.' >&2; exit 2;
}
git -C "$CORE_WORKTREE" merge-base --is-ancestor "$CORE_ELIGIBILITY_SHA" "$CORE_EXPECTED_SHA" || {
  echo 'Final Core SHA does not include the specified connected-model eligibility commit.' >&2; exit 2;
}
git -C "$CORE_WORKTREE" merge-base --is-ancestor "$CORE_PERMISSION_SHA" "$CORE_EXPECTED_SHA" || {
  echo 'Final Core SHA does not include the specified P1 permission fix.' >&2; exit 2;
}
actual_binary_sha=$(shasum -a 256 "$CORE_BINARY")
actual_binary_sha=${actual_binary_sha%% *}
[[ "$actual_binary_sha" == "$CORE_BINARY_SHA256" ]] || { echo 'Core binary SHA-256 differs from the reviewed artifact.' >&2; exit 2; }

cached_modules=$(go env GOMODCACHE)
CONTRACT_ROOT="$BOT_ROOT/.cache/pr164-polish-dev/dual-sha-contract"
mkdir -p "$CONTRACT_ROOT/tmp" "$CONTRACT_ROOT/home/.codex" "$CONTRACT_ROOT/go-build" "$CONTRACT_ROOT/home/.config"
export TMPDIR="$CONTRACT_ROOT/tmp" HOME="$CONTRACT_ROOT/home" ZDOTDIR="$CONTRACT_ROOT/home"
export XDG_CONFIG_HOME="$CONTRACT_ROOT/home/.config" CODEX_HOME="$CONTRACT_ROOT/home/.codex"
export GOCACHE="$CONTRACT_ROOT/go-build" GOMODCACHE="$cached_modules" GOWORK=off GOPROXY=off GOFLAGS=-mod=readonly
export CAELIS_BOT_CONTRACT_ROOT="$BOT_ROOT"

echo "Bot $BOT_EXPECTED_SHA / Core $CORE_EXPECTED_SHA / Core binary SHA-256 $CORE_BINARY_SHA256"
echo 'Core source: temporary Store + synthetic Magpie + Host HTTP + Bot InspectSetup ready/models'
(
  cd "$CORE_WORKTREE"
  go test -count=1 -timeout=5m -run '^TestConnectedModelCatalogRetainsDirectoryEligibilityThroughHostHTTP$' ./app/gatewayapp/controladapter/local
)
echo 'Final Core binary: Bot connection directory and isolated native Host/Guardian'
(
  cd "$BOT_ROOT"
  export CAELIS_BOT_ELIGIBILITY_BINARY="$CORE_BINARY" CAELIS_BOT_TEST_BINARY="$CORE_BINARY"
  go test -race -count=1 -timeout=4m -run '^TestExternalHostSetupCatalogEligibility$' ./internal/backend/caelis
  make smoke-caelis
)
echo 'Dual-SHA isolated contract checks passed; native GUI, real account/model, and distribution acceptance remain separate.'
