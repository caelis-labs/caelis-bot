<p align="center"><img src="internal/desktop/assets/app-icon.png" width="144" alt="Caelis Bot"></p>

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
- **A notebook that stays with you.** Markdown notes and core memory shared across runtimes, editable by you and your Bot.

## Try the preview

**[Download the macOS DMG →](https://github.com/caelis-labs/caelis-bot/releases)**

Apple Silicon only for now. This is an early preview with an **ad-hoc signature, without Apple notarization**. Download the DMG and its `.sha256` file, verify them, then drag **Caelis Bot.app** into Applications. See the [installation guide](docs/install.md) for copy-paste verification and first-launch commands.

Choose a runtime during setup or in **Settings → Runtime**. Codex uses your local installation and account; discovery, installation and login guidance are built in. The Caelis adapter requires the [compatible application-runtime baseline](docs/caelis-integration.md); Caelis 0.60.1 is not compatible, and the tested Core baseline is awaiting release. Neither runtime is bundled.

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

The [Caelis adapter](docs/caelis-integration.md) has passed isolated real-model checks with MiMo v2.6 Flash and GPT-6 Luna, including native file tools, approvals, hot configuration and resource transfer. See the [acceptance report](docs/caelis-live-acceptance.md) for scope and remaining limits.

`make package` produces a verified DMG and checksum. No Blender installation or private asset access is needed. [Development and release workflow →](docs/release.md)

Import local `.caelispack` files in **Settings → Appearance**. Create characters, complete outfit variants or PNG avatars with the offline tool. [Content pack creator guide (中文) →](docs/content-packs.md)

[Product direction](docs/roadmap.md) · [Architecture](docs/architecture.md) · [Character assets](docs/character-assets.md) · [Verification limits](docs/native-acceptance.md)

## License

Code is [Apache-2.0](LICENSE). The bundled character, avatar and brand icons use the separate [Caelis Character Asset License](ASSET-LICENSE.md); modeling sources remain private. The stick figure and generic paper plane are Apache-2.0.
