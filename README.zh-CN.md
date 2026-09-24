<p align="center"><img src="internal/desktop/assets/app-icon.png" width="144" alt="Caelis Bot"></p>

# Caelis Bot

**桌面上的小小伙伴，随时可以帮忙的智能助手。**

[![Checks](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/caelis-labs/caelis-bot)](https://github.com/caelis-labs/caelis-bot/releases)
[![Code license](https://img.shields.io/badge/code-Apache--2.0-blue)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

Caelis Bot 通过一个有表情、有动作的 3D 小角色，把本机 Agent 带到 macOS 桌面。说出需求、递给她一个文件，在需要时确认操作；不用一直开着一块聊天面板。

- **随时在身边。** 可拖动、缩放的角色，常驻状态栏的轻量入口。
- **持续的对话。** 接入本机 Caelis 或 Codex，支持流式回复、附件、历史和操作审批。
- **有存在感的角色。** 待机、互动和工作动作，角色资产独立迭代。
- **随身的笔记本。** Markdown 笔记与核心记忆跨运行时保留，你和 Bot 都可以编辑。

## 下载 Caelis Bot

**[下载 macOS DMG →](https://github.com/caelis-labs/caelis-bot/releases/latest)**

目前提供 Apple Silicon Mac 版本。下载 DMG 及其 `.sha256` 文件，核验后将 **Caelis Bot.app** 拖到“应用程序”。[安装指南](docs/install.zh-CN.md)提供校验和首次启动步骤。

首次设置或在 **设置 → 运行时** 中选择 **Caelis 或 Codex**，使用本机安装与账户；安装包不包含运行时。Caelis v0.61.0 是已验收的正式版本，兼容范围见[接入说明](docs/caelis-integration.md)。Bot 的对话模型与实际工作模型可以分别配置；派出的任务默认沿用运行时配置。

单击角色输入，双击打开对话；从状态栏进入设置或退出。隐藏角色不会停止工作。接入更新器的正式版每天检查更新，确认后验证签名并安装；旧版需先手动升级一次，详见[安装说明](docs/install.zh-CN.md)。

### 让你的本地 Agent 安装

将下面这段话发给可以操作你这台 Mac 的 Agent：

> 请安装并启动最新稳定版 Caelis Bot，来源仅限 https://github.com/caelis-labs/caelis-bot/releases/latest，按仓库 docs/install.zh-CN.md 操作。下载匹配本机原生架构的 DMG 及校验文件，核验 SHA-256，只读挂载后安装到 ~/Applications，保留原有应用数据。校验失败或无兼容包时停止。检查本机 Caelis 或 Codex 是否可用，账户登录由我完成。

## 本地开发

在 macOS 安装 [toolchain.json](toolchain.json) 指定的工具版本后：

```sh
make setup
make check
make run
```

[Caelis 接入](docs/caelis-integration.md)已通过 MiMo v2.6 Flash 与 GPT-6 Luna 的隔离真实模型验收，覆盖原生文件工具、审批、热配置与资源传输。具体范围和剩余限制见[验收报告](docs/caelis-live-acceptance.md)。

`make package` 生成经过校验的 DMG 和 SHA-256。无需 Blender，也无需访问私有资产仓库。[开发与发布流程 →](docs/release.md)

在 **设置 → 外观** 导入 `.caelispack`，独立切换角色、完整服装变体和头像。社区创作者可用离线工具完成制作、打包、校验与二创。[内容包开发指南 →](docs/content-packs.md)

[产品路线](docs/roadmap.md) · [架构](docs/architecture.md) · [角色资产](docs/character-assets.md) · [验收范围](docs/native-acceptance.md)

## 许可

代码采用 [Apache-2.0](LICENSE)。内置角色、头像和品牌图标采用独立的 [Caelis Character Asset License](ASSET-LICENSE.md)，建模源工程保持私有；火柴人与通用纸飞机采用 Apache-2.0。
