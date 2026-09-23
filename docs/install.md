# Install Caelis Bot on macOS

[English](install.md) · [简体中文](install.zh-CN.md)

## Download and verify

Use the [latest stable release](https://github.com/caelis-labs/caelis-bot/releases/latest). Current builds are Apple Silicon (`arm64`); Intel packages are not published yet. CI builds on macOS 14; interactive acceptance has been on Apple Silicon / macOS 27, not every older OS. Historical preview releases are not the current installation path.

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
codesign --verify --strict --test-requirement '=anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "64KZ67PM5J" and identifier "dev.caelis.bot.dmg"' "$DMG"
spctl --assess --type open --context context:primary-signature "$DMG"
hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$MOUNT" "$DMG"
codesign --verify --deep --strict --test-requirement '=anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "64KZ67PM5J" and identifier "dev.caelis.bot"' "$MOUNT/Caelis Bot.app"
spctl --assess --type execute "$MOUNT/Caelis Bot.app"
mkdir -p "$HOME/Applications"
ditto "$MOUNT/Caelis Bot.app" "$APP"
codesign --verify --deep --strict "$APP"
printf 'Installed: %s\n' "$APP"
INSTALL
```

## First launch

Open **Caelis Bot.app** from Applications and confirm macOS's normal first-open prompt if shown. Stable releases use Developer ID signing and Apple notarization, with tickets attached to both the app and DMG. No quarantine-removal command is needed.

```sh
open "$HOME/Applications/Caelis Bot.app"
```

If you dragged the app into the system Applications folder, use `/Applications/Caelis Bot.app`. If checksum, signature or Gatekeeper verification fails, download a fresh official copy. Do not disable system security or re-sign the download to conceal a failure.

## Connect and use

Caelis Bot appears in the **menu bar and on the desktop**, not as a regular Dock app. Single-click the character to write; double-click to open chat. Menu-bar settings control visibility, size, runtime and updates. Only Quit ends the app.

Connect a runtime during setup or in **Settings → Runtime**. Codex uses a compatible local App Server or discovers a local CLI; you can also select an executable or follow the installation and login steps. Installing or opening Codex Desktop alone does not guarantee an accessible runtime. The Bot does not silently install Codex or log in for you. See [runtime compatibility](codex-compatibility.md). Caelis v0.61.0 is the tested release baseline; compatibility is negotiated through the [application-runtime protocol](caelis-integration.md), not a CLI-version allowlist.

Notifications are opt-in. Resident reminders pause when the app exits. Updating the `.app` preserves preferences, attachments and conversation binding. Keep the `Application Support/Caelis Bot` folder unless you intentionally want to remove your data.
