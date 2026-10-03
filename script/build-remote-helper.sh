#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
output=${1:?remote helper destination required}
mkdir -p "$output"
for arch in amd64 arm64; do
 CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$output/linux-$arch" ./cmd/caelis-remote
 shasum -a 256 "$output/linux-$arch" | cut -d ' ' -f 1 > "$output/linux-$arch.sha256"
done
