<p align="center"><img src="frontend/public/icons/caelis-avatar.png" width="112" alt="Caelis Bot"></p>

# Caelis Bot

**A little companion on your desktop. An agent ready to help.**

[![Checks](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/caelis-labs/caelis-bot?include_prereleases)](https://github.com/caelis-labs/caelis-bot/releases)
[![Code license](https://img.shields.io/badge/code-Apache--2.0-blue)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

Caelis Bot brings your local agent to the macOS desktop through a small, expressive 3D character. Ask for help, share a file, and review an action when it needs your approval. Conversation stays close at hand, without a permanent dashboard.

- **Always nearby.** A movable, resizable character and a quiet menu-bar home.
- **One ongoing conversation.** Streaming replies, attachments, history and approvals, powered by your local Codex runtime.
- **A character with presence.** Idle, interaction and work animations, with independently versioned character assets.

## Try the preview

**[Download the macOS DMG →](https://github.com/caelis-labs/caelis-bot/releases)**

Apple Silicon only for now. This is an early preview with an **ad-hoc signature, without Apple notarization**. Download the DMG and its `.sha256` file, verify them, then drag **Caelis Bot.app** into Applications. See the [installation guide](docs/install.md) for copy-paste verification and first-launch commands.

A locally installed, signed-in Codex runtime is required for conversations. Caelis Bot discovers it automatically; you can also select it in **Settings → Runtime**. Codex and development tools are not bundled.

Click the character to write, double-click to open the conversation, and use the menu bar for settings or Quit. Hiding the character does not stop work. Updates are currently installed manually.

### Let your local agent install it

Copy this prompt into an agent that can operate your Mac:

> Install and launch Caelis Bot from https://github.com/caelis-labs/caelis-bot/releases. Follow docs/install.md in that repository. Choose the newest non-draft release, including previews, with a DMG matching this Mac's native architecture; verify its SHA-256, mount read-only and install into ~/Applications. I authorize removing quarantine only from this verified Caelis Bot.app because previews are ad-hoc signed and not notarized. Verify its existing signature, preserve all app data and launch it. If the checksum/signature fails or there is no compatible build, stop. Do not disable system security or silently re-sign a damaged app. Check whether a local Codex runtime is available; leave login to me.

## Build locally

On macOS, install the versions in [toolchain.json](toolchain.json), then:

```sh
make setup
make check
make run
```

`make package` produces a verified DMG and checksum. No Blender installation or private asset access is needed. [Development and release workflow →](docs/release.md)

[Product direction](docs/roadmap.md) · [Architecture](docs/architecture.md) · [Character assets](docs/character-assets.md) · [Verification limits](docs/native-acceptance.md)

## License

Code is [Apache-2.0](LICENSE). The bundled character, avatar and brand icons use the separate [Caelis Character Asset License](ASSET-LICENSE.md); modeling sources remain private. The stick figure and generic paper plane are Apache-2.0.
