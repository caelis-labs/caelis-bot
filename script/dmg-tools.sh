#!/usr/bin/env bash
# Project-local packaging dependencies; never install into the user's Python.
set -euo pipefail
BOT_DMG_ENV="$BOT_ROOT/.cache/dmg-tools"
if [[ ! -x "$BOT_DMG_ENV/bin/python" ]]; then
  python3 -m venv "$BOT_DMG_ENV"
fi
if ! "$BOT_DMG_ENV/bin/python" -c 'import importlib.metadata as m; assert m.version("dmgbuild")=="1.6.7" and m.version("ds-store")=="1.3.3" and m.version("mac-alias")=="2.2.3"' 2>/dev/null; then
  "$BOT_DMG_ENV/bin/python" -m pip install --disable-pip-version-check -r "$BOT_ROOT/script/dmg-requirements.txt"
fi
