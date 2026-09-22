# 在 macOS 安装 Caelis Bot

[English](install.md) · [简体中文](install.zh-CN.md)

## 下载与校验

只从 [caelis-labs/caelis-bot Releases](https://github.com/caelis-labs/caelis-bot/releases) 下载。选择最新的非草稿版本，**包括 preview**；GitHub 的 `/releases/latest` 接口不包含预发布版本。目前只发行 Apple Silicon（`arm64`），尚未发行 Intel 包。CI 在 macOS 14 构建；交互实机验收范围为 Apple Silicon / macOS 27，不能据此声称所有旧系统都已验收。

将 `.dmg` 及同名 `.dmg.sha256` 下载到“下载”目录。下面整段命令可直接粘贴：选择最近下载的 Caelis DMG，核验其对应校验值，只读挂载，安装到 `~/Applications`。安装前先退出已运行的 Caelis Bot；应用仍运行或目标已存在时会停止。升级时先将旧 `.app` 移到废纸篓，保留 `~/Library/Application Support/Caelis Bot/` 中的应用数据。

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

## 首次启动：未公证的预览版

安装包已带有 ad-hoc 本地签名，**尚无 Developer ID 证书与 Apple 公证**。首次启动被拦截后，可到“系统设置 → 隐私与安全性 → 仍要打开”按提示确认（[Apple 官方说明](https://support.apple.com/zh-cn/102445)）。

完成上面的官方来源校验后，也可直接复制以下命令：验证现有签名，仅移除**此应用**的下载隔离标记，然后启动。不需要 `sudo`，不会为应用增加 Developer ID 身份或公证。

```sh
APP="$HOME/Applications/Caelis Bot.app"
codesign --verify --deep --strict "$APP" &&
xattr -dr com.apple.quarantine "$APP" &&
open "$APP"
```

如果通过 DMG 拖入了系统“应用程序”，将第一行改为 `APP="/Applications/Caelis Bot.app"`。
校验或签名失败时重新下载，不要通过重新签名掩盖损坏。不需要全局关闭 Gatekeeper 或 SIP。本地从源码构建时，`make package` 会执行 `codesign --force --sign -`；这是构建签名，不是修复下载包的办法。

## 连接与使用

启动后查看**状态栏和桌面角色**；它默认不显示普通 Dock 图标。单击角色输入，双击打开聊天，从状态栏进入设置、显示/隐藏或退出。隐藏角色不会退出应用。

应用优先连接可用的本机 Codex App Server，否则自动发现 Codex CLI。若发现失败，在“设置 → 接入运行时 → Codex”中指定可执行文件。仅安装或打开 Codex Desktop 不保证存在可连接的入口；Bot 不会静默安装 Codex，也不代替用户登录。详见[运行时兼容性](codex-compatibility.md)。

系统通知需要主动开启，退出时定时提醒暂停。替换 `.app` 会保留偏好、附件和对话绑定；除非有意清除数据，请保留 `Application Support/Caelis Bot` 文件夹。
