<p align="center"><img src="internal/desktop/assets/app-icon.png" width="144" alt="Caelis Bot"></p>

# Caelis Bot

**A little companion on your desktop. An agent ready to help.**

[![Checks](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/caelis-labs/caelis-bot)](https://github.com/caelis-labs/caelis-bot/releases)
[![Code license](https://img.shields.io/badge/code-Apache--2.0-blue)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

Caelis Bot is a personal desktop assistant with a movable 3D character. Chat with your local Caelis or Codex, share files, delegate background work, and keep an editable Markdown notebook. It stays in your menu bar or system tray; hiding the character does not stop work.

## Install

| Platform | Download | Guide |
| --- | --- | --- |
| macOS · Apple Silicon | [DMG](https://github.com/caelis-labs/caelis-bot/releases/latest) · signed and notarized | [Install](docs/install.md) |
| Windows · x64 | [Setup / ZIP](https://github.com/caelis-labs/caelis-bot/releases) · unsigned | [Install](docs/install.windows.md) |
| Linux / WSL · amd64, arm64 | [Managed tar.gz](https://github.com/caelis-labs/caelis-bot/releases/latest) · signature verification, user service | [Install](docs/install.linux.md) |

Verify the download using its `.sha256` file. Platforms publish independently; follow the guide for the matching package and channel.

## Get started

1. Open Caelis Bot and choose **Caelis or Codex**. Caelis is included; Codex uses your local installation and account.
2. Connect a model service or reuse an authenticated model, then introduce your Bot.
3. Click the character to chat, attach a file, or ask for help. Use **Settings → Connections & models** to change connections.

Optional Computer Use asks for the permissions it needs. Import characters and avatars in **Settings → Appearance**. [Features and limits](docs/status.md) · [Content pack guide](docs/content-packs.md)

## Development

On macOS, install the pinned [toolchain](toolchain.json), then run:

```sh
make setup
make check
make run
```

[Development](docs/development.md) · [Windows builds](docs/windows-release.md) · [Documentation](docs/README.md) · [Issues](https://github.com/caelis-labs/caelis-bot/issues)

## License

Code: [Apache-2.0](LICENSE). Bundled character and brand assets: [Caelis Character Asset License](ASSET-LICENSE.md). Desktop World and other dependencies retain their own licenses and bundled notices.
