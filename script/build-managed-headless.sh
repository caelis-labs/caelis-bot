#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
# Deliberately does not publish or sign. Signing is an explicit separate step.
BOT_UPDATE_ARCH="${BOT_UPDATE_ARCH:-$(go env GOARCH)}"
case "$BOT_UPDATE_ARCH" in amd64|arm64) ;; *) echo 'Unsupported Linux architecture' >&2; exit 1;; esac
BOT_UPDATE_KIND="${BOT_UPDATE_KIND:-development}"
BOT_UPDATE_VERSION="${BOT_UPDATE_VERSION:-$(node -p 'JSON.parse(require("fs").readFileSync("package.json","utf8")).version')}"
BOT_UPDATE_SOURCE="$(git rev-parse HEAD)"
BOT_UPDATE_TAGS=""
case "$BOT_UPDATE_KIND" in
 release)
  test -z "$(git status --porcelain --untracked-files=normal)"
  test "$(git rev-parse "v$BOT_UPDATE_VERSION^{commit}")" = "$BOT_UPDATE_SOURCE"
  test "$BOT_UPDATE_VERSION" = "$(node -p 'JSON.parse(require("fs").readFileSync("package.json","utf8")).version')"
  ;;
 qa)
  [[ "$BOT_UPDATE_VERSION" =~ -qa\.[1-9][0-9]*$ ]]
  BOT_UPDATE_TAGS="managed_update_qa"
  ;;
 *) echo 'Choose explicit release or qa build; QA versions must end in -qa.N' >&2; exit 1;;
esac
BOT_UPDATE_OUTPUT="${BOT_UPDATE_OUTPUT:-$BOT_ROOT/dist/managed/$BOT_UPDATE_VERSION-linux-$BOT_UPDATE_ARCH}"
test ! -e "$BOT_UPDATE_OUTPUT"
mkdir -p "$BOT_UPDATE_OUTPUT/package"
if [[ "${BOT_UPDATE_REUSE_FRONTEND:-0}" != 1 ]]; then npm run build; fi
test -f frontend/dist/web.html
python3 script/caelis-runtime.py stage-headless "$BOT_UPDATE_OUTPUT/package" "linux-$BOT_UPDATE_ARCH"
BOT_UPDATE_LDFLAGS="-X github.com/caelis-labs/caelis-bot/internal/updates.Version=$BOT_UPDATE_VERSION -X github.com/caelis-labs/caelis-bot/internal/managedupdate.Source=$BOT_UPDATE_SOURCE -X github.com/caelis-labs/caelis-bot/internal/managedupdate.BuildKind=$BOT_UPDATE_KIND"
CGO_ENABLED=0 GOOS=linux GOARCH="$BOT_UPDATE_ARCH" go build -tags "$BOT_UPDATE_TAGS" -trimpath -ldflags "$BOT_UPDATE_LDFLAGS" -o "$BOT_UPDATE_OUTPUT/package/caelisbot" .
CGO_ENABLED=0 GOOS=linux GOARCH="$BOT_UPDATE_ARCH" go build -tags "$BOT_UPDATE_TAGS" -trimpath -o "$BOT_UPDATE_OUTPUT/caelisbot-launcher" ./cmd/caelisbot-launcher
cp LICENSE "$BOT_UPDATE_OUTPUT/package/LICENSE"
cp ASSET-LICENSE.md "$BOT_UPDATE_OUTPUT/package/ASSET-LICENSE.md"
cp protocol/caelis/LICENSE "$BOT_UPDATE_OUTPUT/package/Resources/Caelis-Protocol-LICENSE"
cp licenses/openclaw-weixin-LICENSE "$BOT_UPDATE_OUTPUT/package/Resources/OpenClaw-Weixin-LICENSE"
cp licenses/go-qrcode-LICENSE "$BOT_UPDATE_OUTPUT/package/Resources/Go-QRCode-LICENSE"
cp docs/managed-updates.md "$BOT_UPDATE_OUTPUT/package/README.md"
chmod 644 "$BOT_UPDATE_OUTPUT/package/ASSET-LICENSE.md" "$BOT_UPDATE_OUTPUT/package/LICENSE" "$BOT_UPDATE_OUTPUT/package/README.md" "$BOT_UPDATE_OUTPUT/package/Resources/"*
chmod 755 "$BOT_UPDATE_OUTPUT/package/caelisbot" "$BOT_UPDATE_OUTPUT/package/Helpers/caelis"
BOT_UPDATE_ARCHIVE="$BOT_UPDATE_OUTPUT/caelisbot-$BOT_UPDATE_VERSION-linux-$BOT_UPDATE_ARCH.tar.gz"
python3 - "$BOT_UPDATE_OUTPUT/package" "$BOT_UPDATE_ARCHIVE" <<'PY'
import pathlib,sys,tarfile
root=pathlib.Path(sys.argv[1])
with tarfile.open(sys.argv[2],'w:gz',format=tarfile.USTAR_FORMAT) as tar:
    for member in sorted(root.rglob('*')):
        if member.is_file():
            info=tar.gettarinfo(str(member),member.relative_to(root).as_posix())
            info.uid=info.gid=0
            info.uname=info.gname=''
            with member.open('rb') as stream: tar.addfile(info,stream)
PY
node script/linux-update-manifest.mjs create "$BOT_UPDATE_OUTPUT/package" "$BOT_UPDATE_ARCHIVE" "$BOT_UPDATE_VERSION" "$BOT_UPDATE_SOURCE" "linux-$BOT_UPDATE_ARCH" "$BOT_UPDATE_KIND" "$BOT_UPDATE_OUTPUT/linux-$BOT_UPDATE_ARCH-update.json"
echo "Created unsigned $BOT_UPDATE_KIND package and manifest: $BOT_UPDATE_OUTPUT"
