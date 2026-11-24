#!/usr/bin/env bash
# Package the reviewed transparent cutout consistently across application surfaces.
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p .cache internal/desktop/assets frontend/public/icons
BOT_ICON_WORK=$(mktemp -d "$PWD/.cache/brand-icons.XXXXXX")
trap 'rm -rf "$BOT_ICON_WORK"' EXIT
mkdir -p "$BOT_ICON_WORK/CaelisBot.iconset"
BOT_ICON_SOURCE="$PWD/art/brand/app-icon-1024.png"
BOT_AVATAR_SOURCE="$PWD/art/brand/avatar-transparent.png"
test -f "$BOT_ICON_SOURCE"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$BOT_ICON_SOURCE" --out "$BOT_ICON_WORK/CaelisBot.iconset/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$BOT_ICON_SOURCE" --out "$BOT_ICON_WORK/CaelisBot.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
cp "$BOT_ICON_SOURCE" "$BOT_ICON_WORK/CaelisBot.iconset/icon_512x512@2x.png"
iconutil -c icns "$BOT_ICON_WORK/CaelisBot.iconset" -o resources/macos/CaelisBot.icns
sips -z 512 512 "$BOT_ICON_SOURCE" --out internal/desktop/assets/app-icon.png >/dev/null
sips -z 128 128 "$BOT_AVATAR_SOURCE" --out frontend/public/icons/caelis-avatar.png >/dev/null
sips -z 64 64 "$BOT_AVATAR_SOURCE" --out internal/desktop/assets/status-icon.png >/dev/null
echo 'Prepared bundle icon, runtime icon and chat avatar from the reviewed masters.'
