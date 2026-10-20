#!/usr/bin/env bash
set -euo pipefail
# The archive comes from the exact-source CI build job. Preserve its signed
# inventory modes without changing the signing job's private-key umask.
tar -xzpf "$1" -C "$2"
