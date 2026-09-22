#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
node --version
npm --version
go version
wails3 version
codex --version
xcode-select -p
echo 'Versions above are installed; make smoke verifies the actual toolchain paths.'
