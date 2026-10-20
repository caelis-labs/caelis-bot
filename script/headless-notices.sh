#!/usr/bin/env bash
# One inventory for every headless bundle (managed, manual and development image).
set -euo pipefail
root="${1:?package directory required}"
mkdir -p "$root/Resources"
cp LICENSE "$root/LICENSE"
cp ASSET-LICENSE.md "$root/ASSET-LICENSE.md"
cp protocol/caelis/LICENSE "$root/Resources/Caelis-Protocol-LICENSE"
cp licenses/openclaw-weixin-LICENSE "$root/Resources/OpenClaw-Weixin-LICENSE"
cp licenses/go-qrcode-LICENSE "$root/Resources/Go-QRCode-LICENSE"
for file in LICENSE ASSET-LICENSE.md Resources/Caelis-Protocol-LICENSE Resources/OpenClaw-Weixin-LICENSE Resources/Go-QRCode-LICENSE Resources/Caelis-Runtime-LICENSE; do
 test -s "$root/$file"
 chmod 644 "$root/$file"
done
