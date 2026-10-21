<p align="center"><img src="internal/desktop/assets/app-icon.png" width="144" alt="Caelis Bot"></p>

# Caelis Bot

**桌面上的小小伙伴，随时可以帮忙的智能助手。**

[![Checks](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/caelis-labs/caelis-bot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/caelis-labs/caelis-bot)](https://github.com/caelis-labs/caelis-bot/releases)
[![Code license](https://img.shields.io/badge/code-Apache--2.0-blue)](LICENSE)

[English](README.md) · [简体中文](README.zh-CN.md)

Caelis Bot 是一个带有可拖动 3D 角色的个人桌面助手。与本机 Caelis 或 Codex 对话、发送文件、委派后台工作，并维护可编辑的 Markdown 笔记。应用常驻状态栏或托盘，隐藏角色不会停止工作。

## 安装

| 平台 | 下载 | 指南 |
| --- | --- | --- |
| macOS · Apple Silicon | [DMG](https://github.com/caelis-labs/caelis-bot/releases/latest) · 已签名、公证 | [安装说明](docs/install.zh-CN.md) |
| Windows · x64 | [Setup / ZIP](https://github.com/caelis-labs/caelis-bot/releases) · 未签名 | [安装说明](docs/install.windows.md) |
| Linux / WSL · amd64、arm64 | [完整 tar.gz](https://github.com/caelis-labs/caelis-bot/releases/latest) · 签名校验、用户服务 | [安装说明](docs/install.linux.md) |

使用安装包附带的 `.sha256` 文件核验下载。各平台独立发布，请按指南选择对应安装包和渠道。

## 开始使用

1. 打开 Caelis Bot，选择 **Caelis 或 Codex**。安装包内置 Caelis；Codex 使用本机安装与账户。
2. 连接模型服务或复用已有认证模型，再介绍你的 Bot。
3. 点击角色聊天、添加文件或交代任务，在 **设置 → 连接与模型** 调整连接。

可选的电脑操控会按需申请权限。在 **设置 → 外观** 导入角色和头像。[功能与限制](docs/status.md) · [内容包创作指南](docs/content-packs.md)

## 开发

在 macOS 安装指定的[工具链](toolchain.json)后运行：

```sh
make setup
make check
make run
```

[开发指南](docs/development.md) · [Windows 构建](docs/windows-release.md) · [文档索引](docs/README.md) · [反馈问题](https://github.com/caelis-labs/caelis-bot/issues)

## 许可

代码：[Apache-2.0](LICENSE)。内置角色和品牌资产：[Caelis Character Asset License](ASSET-LICENSE.md)。Desktop World 及其他依赖保留各自许可和随包声明。
