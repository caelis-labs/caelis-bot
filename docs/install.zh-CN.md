# 在 macOS 安装 Caelis Bot

[English](install.md) · [简体中文](install.zh-CN.md)

## 下载与校验

从 [GitHub Latest](https://github.com/caelis-labs/caelis-bot/releases/latest) 选择 Stable，下载前核对其 tag 与 [macOS Stable 已签名清单](https://releases.caelis.dev/caelis-bot/latest.json)一致。若不同，或缺少匹配的 DMG 和校验文件，暂停安装，不使用默认旧版。目前只发行 Apple Silicon（`arm64`），尚未发行 Intel 包。CI 在 macOS 14 构建；交互实机验收范围为 Apple Silicon / macOS 27，不能据此声称所有旧系统都已验收。历史 preview 包不再作为当前安装入口。

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

首次设置或在“设置 → 运行时”中连接运行时。Codex 优先使用可用的本机 App Server，否则自动发现 CLI；也可手动选择程序或按引导安装、登录。仅安装或打开 Codex Desktop 不保证存在可连接的入口；Bot 不会静默安装 Codex，也不代替用户登录。详见[运行时兼容性](caelis-integration.md)。Caelis 需要当前应用与 Guardian 能力；兼容性由[通用应用协议](caelis-integration.md)协商决定，不按 CLI 版本号白名单限制。

系统通知需要主动开启，退出时定时提醒暂停。替换 `.app` 会保留偏好、附件和对话绑定；除非有意清除数据，请保留 `Application Support/Caelis Bot` 文件夹。

## 更新

接入更新器的发行版每天自动检查；“设置 → 更新”可选择 Stable 或 Dev、关闭自动检查，也可从状态栏点击
“检查更新”。确认原生更新窗口后，应用下载、验证签名并安装新版；有工作、待审批或
结果未知的操作时等待处理完成再重启，保留对话、Notebook 和设置。

已发布的 v0.1.0、预览版和开发版仍手动安装。按上面的说明安装首个带更新器的正式版后，
后续即可在应用内更新。Stable 只接收验收通过的正式版，Dev 只接收 Dev 预发行版。切换渠道保留同一安装、凭据、任务及数据。Sparkle 只提供更高构建号；若当前 Dev 构建高于 Stable feed，切回 Stable 会等待更高的 Stable 构建，不会静默降级。R2 保留旧版本不可变制品，以便缓存的签名 appcast 仍能下载其准确 DMG；Stable 和 Dev 的可变指针及 appcast 分别隔离。

## 显式安装 Dev 发行版

在[发行历史](https://github.com/caelis-labs/caelis-bot/releases)选择已发布的 `vX.Y.Z-dev.N` **预发行版**，像上文一样核对 DMG 及同名 `.sha256`，只读挂载并验证 Developer ID、公证和 Gatekeeper。DMG 标识仍为 `dev.caelis.bot.dmg`，应用标识仍为 `dev.caelis.bot`。退出 Caelis Bot 后，用 **Caelis Bot.app** 替换 `~/Applications` 中的旧版。Dev 与 Stable 共用 `~/Library/Application Support/Caelis Bot`、凭据、权限、任务和会话。首次安装 Dev 默认选 Dev 更新；替换应用会保留此前明确选择的渠道。可在“设置 → 更新”选择渠道；Dev 读取[独立签名 appcast](https://releases.caelis.dev/caelis-bot/feeds/macos/arm64/dev/appcast.xml)，Stable 读取 Stable appcast。切换渠道不会安装较旧版本。单独的本地 `Caelis Bot Dev.app` 只是开发构建，不是可安装 Dev 发行版。
