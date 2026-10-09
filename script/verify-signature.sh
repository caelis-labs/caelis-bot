#!/usr/bin/env bash
# Verify identity as well as integrity; ad-hoc is never a Developer ID fallback.
set -euo pipefail
BOT_VERIFY_PATH=${1:?Usage: verify-signature.sh PATH [adhoc|development|developer-id] [app|dmg]}
BOT_VERIFY_MODE=${2:-adhoc}
BOT_VERIFY_KIND=${3:-app}
case "$BOT_VERIFY_KIND" in
  app) BOT_VERIFY_IDENTIFIER=dev.caelis.bot ;;
  dmg) BOT_VERIFY_IDENTIFIER=dev.caelis.bot.dmg ;;
  *) echo "Unknown artifact kind: $BOT_VERIFY_KIND" >&2; exit 1 ;;
esac
if [[ "$BOT_VERIFY_KIND" == app && "${4:-}" == dev.caelis.bot.dev ]]; then
  [[ "$BOT_VERIFY_MODE" != developer-id ]] || { echo 'Local development identity is not a distribution release.' >&2; exit 1; }
  BOT_VERIFY_IDENTIFIER=dev.caelis.bot.dev
fi
codesign --verify --deep --strict "$BOT_VERIFY_PATH"
BOT_VERIFY_DETAILS=$(codesign -dvv "$BOT_VERIFY_PATH" 2>&1)
grep -Fxq "Identifier=$BOT_VERIFY_IDENTIFIER" <<< "$BOT_VERIFY_DETAILS"
case "$BOT_VERIFY_MODE" in
  adhoc)
    grep -q '^Signature=adhoc$' <<< "$BOT_VERIFY_DETAILS"
    ;;
  development)
    [[ "$BOT_VERIFY_KIND" == app ]]
    codesign --verify --strict --test-requirement \
      "=anchor apple generic and identifier \"$BOT_VERIFY_IDENTIFIER\"" "$BOT_VERIFY_PATH"
    grep -q '^Authority=Apple Development:' <<< "$BOT_VERIFY_DETAILS"
    grep -q 'flags=.*runtime' <<< "$BOT_VERIFY_DETAILS"
    ;;
  developer-id)
    [[ "${BOT_SIGNING_TEAM_ID:-}" =~ ^[A-Z0-9]{10}$ ]] || {
      echo 'BOT_SIGNING_TEAM_ID must be the 10-character Apple team ID.' >&2; exit 1;
    }
    codesign --verify --strict --test-requirement \
      "=anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = \"$BOT_SIGNING_TEAM_ID\" and identifier \"$BOT_VERIFY_IDENTIFIER\"" \
      "$BOT_VERIFY_PATH"
    grep -q '^Authority=Developer ID Application:' <<< "$BOT_VERIFY_DETAILS"
    grep -q '^Timestamp=' <<< "$BOT_VERIFY_DETAILS"
    if [[ "$BOT_VERIFY_KIND" == app ]]; then grep -q 'flags=.*runtime' <<< "$BOT_VERIFY_DETAILS"; fi
    ;;
  *) echo "Unknown signing mode: $BOT_VERIFY_MODE" >&2; exit 1 ;;
esac
if [[ "$BOT_VERIFY_KIND" == app && -d "$BOT_VERIFY_PATH/Contents" ]]; then
  bash "$(dirname "$0")/verify-desktop-world.sh" "$BOT_VERIFY_PATH" "$BOT_VERIFY_MODE"
fi
