# 在 macOS 安装 Caelis Bot

[English](install.md) · [简体中文](install.zh-CN.md)

## 下载与校验

从[最新稳定版](https://github.com/caelis-labs/caelis-bot/releases/latest)下载。目前只发行 Apple Silicon（`arm64`），尚未发行 Intel 包。CI 在 macOS 14 构建；交互实机验收范围为 Apple Silicon / macOS 27，不能据此声称所有旧系统都已验收。历史 preview 包不再作为当前安装入口。

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

## 首次启动

从“应用程序”打开 **Caelis Bot.app**，如出现 macOS 正常的首次打开提示，确认打开即可。稳定版使用 Developer ID 签名与 Apple 公证，App 和 DMG 均附带公证票据，无需执行移除隔离标记的命令。

```sh
open "$HOME/Applications/Caelis Bot.app"
```

如果通过 DMG 拖入了系统“应用程序”，使用 `/Applications/Caelis Bot.app`。校验值、签名或 Gatekeeper 验证失败时，重新下载官方安装包；不要关闭系统安全机制或通过重新签名掩盖失败。

## 连接与使用

启动后查看**状态栏和桌面角色**；它默认不显示普通 Dock 图标。单击角色输入，双击打开聊天，从状态栏进入设置、显示/隐藏或退出。隐藏角色不会退出应用。

首次设置或在“设置 → 运行时”中连接运行时。Codex 优先使用可用的本机 App Server，否则自动发现 CLI；也可手动选择程序或按引导安装、登录。仅安装或打开 Codex Desktop 不保证存在可连接的入口；Bot 不会静默安装 Codex，也不代替用户登录。详见[运行时兼容性](codex-compatibility.md)。Caelis v0.61.0 是已验收的正式基线；兼容性由[通用应用协议](caelis-integration.md)协商决定，不按 CLI 版本号白名单限制。

系统通知需要主动开启，退出时定时提醒暂停。替换 `.app` 会保留偏好、附件和对话绑定；除非有意清除数据，请保留 `Application Support/Caelis Bot` 文件夹。
