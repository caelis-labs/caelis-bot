#!/usr/bin/env bash
# Source to obtain the pinned SDK. Never execute an unverified download.
set -euo pipefail
BOT_SPARKLE_VERSION=2.10.0
BOT_SPARKLE_SHA256=c2bf58aa8387266ac179357b1415d6f2635f044da8be41042af32425dae6da0c
BOT_SPARKLE_CACHE="${BOT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}/.cache/sparkle"
BOT_SPARKLE_ARCHIVE="$BOT_SPARKLE_CACHE/Sparkle-$BOT_SPARKLE_VERSION.tar.xz"
mkdir -p "$BOT_SPARKLE_CACHE"
if [[ ! -f "$BOT_SPARKLE_ARCHIVE" ]]; then
  curl --fail --location --proto '=https' --tlsv1.2 --retry 3 \
    "https://github.com/sparkle-project/Sparkle/releases/download/$BOT_SPARKLE_VERSION/Sparkle-$BOT_SPARKLE_VERSION.tar.xz" \
    -o "$BOT_SPARKLE_ARCHIVE.part"
  mv "$BOT_SPARKLE_ARCHIVE.part" "$BOT_SPARKLE_ARCHIVE"
fi
echo "$BOT_SPARKLE_SHA256  $BOT_SPARKLE_ARCHIVE" | shasum -a 256 --check >/dev/null
# Extract afresh: tools and framework must match the validated archive, not stale cache files.
BOT_SPARKLE_DIR=$(mktemp -d "$BOT_SPARKLE_CACHE/sdk.XXXXXX")
tar -xf "$BOT_SPARKLE_ARCHIVE" -C "$BOT_SPARKLE_DIR"
