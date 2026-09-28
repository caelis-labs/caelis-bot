#!/usr/bin/env bash
# Stage a self-contained, pinned Cua SDK host. Never use the user's Node install
# at runtime; Homebrew Node has dependencies outside the application bundle.
set -euo pipefail
source "$(dirname "$0")/env.sh"
BOT_CUA_BUNDLE=${1:?application bundle required}
BOT_CUA_CACHE="$BOT_ROOT/.cache/computer-use-runtime"
BOT_CUA_PLATFORM=$(uname -m)
case "$BOT_CUA_PLATFORM" in
  arm64) BOT_CUA_NODE_ARCH=arm64; BOT_CUA_NODE_SHA=bed7eea5325e1108f32ce5228ddd6a5f0f08a499ee42aa7442aea583702f6057 ;;
  x86_64) BOT_CUA_NODE_ARCH=x64; BOT_CUA_NODE_SHA=1462cb3b3046b815cf8ea436d3da450ec1a9f11dac7e5a46b0ada5305d7e8097 ;;
  *) echo 'Unsupported Computer Use build architecture.' >&2; exit 1 ;;
esac
BOT_CUA_NODE_NAME="node-v24.21.0-darwin-$BOT_CUA_NODE_ARCH"
mkdir -p "$BOT_CUA_CACHE"
if [[ ! -f "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz" ]]; then
  curl --fail --silent --show-error --location --retry 3 "https://nodejs.org/dist/v24.21.0/$BOT_CUA_NODE_NAME.tar.gz" -o "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz.partial"
  printf '%s  %s\n' "$BOT_CUA_NODE_SHA" "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz.partial" | shasum -a 256 -c
  mv "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz.partial" "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz"
fi
printf '%s  %s\n' "$BOT_CUA_NODE_SHA" "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz" | shasum -a 256 -c
tar -xzf "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME.tar.gz" -C "$BOT_CUA_CACHE"
npm ci --prefix "$BOT_ROOT/resources/computer-use" --include=optional --ignore-scripts --no-audit --no-fund
BOT_CUA_DEST="$BOT_CUA_BUNDLE/Contents/Resources/ComputerUse"
# Only the owned derived staging directory is replaced.
rm -rf "$BOT_CUA_DEST"
mkdir -p "$BOT_CUA_DEST"
cp "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME/bin/node" "$BOT_CUA_DEST/node"
cp "$BOT_CUA_CACHE/$BOT_CUA_NODE_NAME/LICENSE" "$BOT_CUA_DEST/Node-LICENSE"
cp "$BOT_ROOT/resources/computer-use/host.mjs" "$BOT_ROOT/resources/computer-use/desktop.mjs" "$BOT_ROOT/resources/computer-use/"*.json "$BOT_CUA_DEST/"
cp "$BOT_ROOT/resources/computer-use/THIRD-PARTY-NOTICES.md" "$BOT_CUA_DEST/"
ditto "$BOT_ROOT/resources/computer-use/node_modules" "$BOT_CUA_DEST/node_modules"
cp "$BOT_ROOT/resources/computer-use/MPL-2.0.txt" "$BOT_CUA_DEST/"
"$BOT_CUA_DEST/node" --jitless --version
