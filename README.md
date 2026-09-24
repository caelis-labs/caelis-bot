<p align="center"><img src="internal/desktop/assets/app-icon.png" width="144" alt="Caelis Bot"></p>

# Caelis Bot

**A little companion on your desktop. An agent ready to help.**

[![Checks](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/caelis-labs/caelis-bot)](https://github.com/caelis-labs/caelis-bot/releases)
[![Code license](https://img.shields.io/badge/code-Apache--2.0-blue)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

Caelis Bot brings your local agent to the macOS desktop through a small, expressive 3D character. Ask for help, share a file, and review an action when it needs your approval. Conversation stays close at hand, without a permanent dashboard.

- **Always nearby.** A movable, resizable character and a quiet menu-bar home.
- **One ongoing conversation.** Streaming replies, attachments, history and approvals, powered by your local Caelis or Codex runtime.
- **A character with presence.** Idle, interaction and work animations, with independently versioned character assets.
- **A notebook that stays with you.** Markdown notes and core memory shared across runtimes, editable by you and your Bot.

## Get Caelis Bot

**[Download the macOS DMG →](https://github.com/caelis-labs/caelis-bot/releases/latest)**

Available for Apple Silicon Macs. Download the DMG and its `.sha256` file, verify the download, then drag **Caelis Bot.app** into Applications. See the [installation guide](docs/install.md) for verification and first-launch steps.

Choose **Caelis or Codex** during setup or in **Settings → Runtime**. Use your local installation and account; neither runtime is bundled. Caelis v0.61.0 is the tested release baseline; see [runtime compatibility](docs/caelis-integration.md). The Bot's conversation model and the models used for delegated work can be configured separately. By default, delegated work uses the runtime's configuration.

Click the character to write, double-click to open the conversation, and use the menu bar for settings or Quit. Hiding the character does not stop work. Stable builds with the updater check daily and install signed updates after confirmation. Older builds need one manual upgrade; see [updating](docs/install.md#update).

### Let your local agent install it

Copy this prompt into an agent that can operate your Mac:

> Install and launch the latest stable Caelis Bot release from https://github.com/caelis-labs/caelis-bot/releases/latest. Follow docs/install.md in that repository. Download the DMG matching this Mac's native architecture and its checksum, verify SHA-256, mount read-only and install into ~/Applications. Preserve all app data. Stop if verification fails or there is no compatible build. Check for a local Caelis or Codex runtime; leave account login to me.

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
