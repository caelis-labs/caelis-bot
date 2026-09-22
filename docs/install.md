# Install Caelis Bot on macOS

[English](install.md) · [简体中文](install.zh-CN.md)

## Download and verify

Use only [caelis-labs/caelis-bot releases](https://github.com/caelis-labs/caelis-bot/releases). Choose the newest non-draft release with a matching DMG, **including preview releases**; GitHub's `/releases/latest` API omits previews. Current builds are Apple Silicon (`arm64`). Intel packages are not published yet. CI builds on macOS 14; interactive acceptance has been on Apple Silicon / macOS 27, not every older OS.

Download the `.dmg` and its same-named `.dmg.sha256` into `~/Downloads`. The following block selects the most recently downloaded Caelis DMG, verifies its exact sibling checksum, mounts it read-only and installs it into your user Applications folder. Quit an existing Caelis Bot first; the commands stop if it is running or already installed. For an upgrade, move just the old `.app` to Trash first. Your data under `~/Library/Application Support/Caelis Bot/` is preserved.

```sh
/bin/bash <<'INSTALL'
set -euo pipefail
cd "$HOME/Downloads"
DMG=$(ls -t Caelis-Bot-*-macos-*.dmg 2>/dev/null | head -n 1)
test -n "$DMG"
ARCH=$(uname -m)
if [[ "$(sysctl -in sysctl.proc_translated 2>/dev/null || true)" == 1 ]]; then ARCH=arm64; fi
[[ "$DMG" == *"-macos-$ARCH.dmg" ]]
EXPECTED=$(awk 'NR==1 {print $1}' "$DMG.sha256")
[[ "$EXPECTED" =~ ^[0-9a-f]{64}$ ]]
[[ "$(shasum -a 256 "$DMG" | awk '{print $1}')" == "$EXPECTED" ]]
if pgrep -x caelis-bot >/dev/null; then echo 'Quit Caelis Bot before installing.' >&2; exit 1; fi
APP="$HOME/Applications/Caelis Bot.app"
if [[ -e "$APP" ]]; then echo 'Move the previous app to Trash first; keep your application data.' >&2; exit 1; fi
MOUNT=$(mktemp -d /tmp/caelis-install.XXXXXX)
trap 'hdiutil detach "$MOUNT" -quiet 2>/dev/null || true; rmdir "$MOUNT" 2>/dev/null || true' EXIT
hdiutil verify "$DMG"
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$MOUNT" "$DMG"
codesign --verify --deep --strict "$MOUNT/Caelis Bot.app"
mkdir -p "$HOME/Applications"
ditto "$MOUNT/Caelis Bot.app" "$APP"
codesign --verify --deep --strict "$APP"
printf 'Installed: %s\n' "$APP"
INSTALL
```

## First launch: unnotarized preview

The app already has an ad-hoc signature. It has **no Developer ID certificate and no Apple notarization**. Prefer **System Settings → Privacy & Security → Open Anyway** after an initial blocked launch ([Apple instructions](https://support.apple.com/en-us/102445)).

After verifying the official download above, you can instead copy this app-scoped command. It removes the downloaded-app quarantine check for **this app only**, verifies the existing signature, then launches. It does not grant a Developer ID identity or notarization and needs no `sudo`.

```sh
APP="$HOME/Applications/Caelis Bot.app"
codesign --verify --deep --strict "$APP" &&
xattr -dr com.apple.quarantine "$APP" &&
open "$APP"
```

If you dragged the app into the system Applications folder, use `APP="/Applications/Caelis Bot.app"` instead. If checksum or signature verification fails, download a fresh copy; do not re-sign it to hide corruption. Do not disable Gatekeeper or SIP globally. Local source builds are signed by `make package` with `codesign --force --sign -`; that is a build operation, not an installation repair.

## Connect and use

Caelis Bot appears in the **menu bar and on the desktop**, not as a regular Dock app. Single-click the character to write; double-click to open chat. Menu-bar settings control visibility, size, runtime and updates. Only Quit ends the app.

The app uses an existing compatible local Codex App Server or discovers a local Codex CLI. If automatic discovery fails, select its executable in **Settings → Runtime → Codex**. Installing or opening Codex Desktop alone does not guarantee an accessible runtime. The Bot does not silently install Codex or log in for you. See [runtime compatibility](codex-compatibility.md).

Notifications are opt-in. Resident reminders pause when the app exits. Updating the `.app` preserves preferences, attachments and conversation binding. Keep the `Application Support/Caelis Bot` folder unless you intentionally want to remove your data.
