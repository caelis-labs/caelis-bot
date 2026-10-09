#!/usr/bin/env bash
# Explicit release builds retain the production identity; all ordinary local
# and PR builds use one stable development identity and independent data.
BOT_BUILD_CHANNEL=${BOT_BUILD_CHANNEL:-development}
if [[ -n "${BOT_RELEASE_TAG:-}" ]]; then
  BOT_BUILD_CHANNEL=release
fi
case "$BOT_BUILD_CHANNEL" in
  development) BOT_APP_NAME='Caelis Bot Dev'; BOT_APP_ID=dev.caelis.bot.dev ;;
  release) BOT_APP_NAME='Caelis Bot'; BOT_APP_ID=dev.caelis.bot ;;
  *) echo 'Unsupported Bot build channel.' >&2; exit 1 ;;
esac
BOT_BUNDLE="$BOT_ROOT/dist/$BOT_APP_NAME.app"
