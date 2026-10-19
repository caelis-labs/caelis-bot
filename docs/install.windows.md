# Install Caelis Bot on Windows / Windows 安装

Windows x64 packages are available as **Setup.exe** and a portable **ZIP**. The compatibility target is Windows 10 22H2 or newer with WebView2; interactive validation is on Windows 11. The packages are currently unsigned. [Platform limits](status.md)

Windows x64 提供安装版 **Setup.exe** 和免安装 **ZIP**。兼容目标为 Windows 10 22H2 及以上，并需要 WebView2；交互验证在 Windows 11 上完成。当前安装包未签名。

## Download / 下载

Open [GitHub Releases](https://github.com/caelis-labs/caelis-bot/releases). Choose a release containing `windows-amd64-stable-Setup.exe` or `windows-amd64-stable.zip` and its same-named `.sha256`. Preview tags contain `-windows.preview.N`; use them only when you intend to test that channel. GitHub Latest may point to a macOS-only release.

在发行页选择带有 `windows-amd64-stable` 的安装包及同名 `.sha256` 文件。只有明确需要测试版时才选择 `-windows.preview.N`。GitHub Latest 可能只包含 macOS 包。

In PowerShell, replace the example filename with the exact download:

```powershell
$package = "$HOME/Downloads/Caelis-Bot-X.Y.Z-windows-amd64-stable-Setup.exe"
$expected = ((Get-Content -Raw -LiteralPath "$package.sha256").Trim() -split '\s+')[0]
$actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $package).Hash
if ($expected -notmatch '^[a-fA-F0-9]{64}$' -or $actual -ne $expected) {
    throw 'Checksum mismatch; do not install this download.'
}
```

## Install and connect / 安装与连接

1. Run Setup and select an installation directory, or extract the complete ZIP into a new folder and launch `caelis-bot.exe`. Keep the bundled runtime, Plugin and license files together.
2. If Windows or an organization policy blocks the unsigned package, do not disable system protections. Confirm the source or ask your administrator.
3. Open the tray entry or desktop character. Choose included Caelis or your local Codex, complete account/model setup, then introduce your Bot.
4. Click the character to open chat. Connections, appearance, optional features and updates are in Settings.

运行 Setup 选择目录，或把完整 ZIP 解压到新目录后启动 `caelis-bot.exe`，不要只复制主程序。若系统或组织策略阻止未签名程序，请核对来源或联系管理员，不关闭系统保护。启动后从托盘或桌面角色进入，选择内置 Caelis 或本机 Codex，完成模型连接即可聊天。

## Update and data / 更新与数据

Stable, Preview and local Dev builds have separate application identities and data. Installing Stable does not migrate Preview data or change its update channel. User data lives under `%LOCALAPPDATA%` in the corresponding Caelis Bot directory; retain it when replacing the application.

Use Settings → Updates when the release includes a valid signed update manifest, or install a newer official Setup into the existing directory. For ZIP upgrades, quit the app and follow the release instructions; preserve the user-data directory. Hiding the character is not quitting. Updater signatures and installer Authenticode signatures are separate: a signed update manifest does not make the installer Authenticode-signed.

Stable、Preview 和 Dev 的安装与数据相互独立，不自动迁移。支持签名更新清单的版本可从“设置 → 更新”更新，也可安装新版官方 Setup。手动替换 ZIP 前先退出应用，并保留 `%LOCALAPPDATA%` 下对应数据目录。更新清单签名不等于安装包具有 Authenticode 签名。

ZIP is an installation-free distribution with persistent per-user data. Within the
same channel, ZIP and Setup share the data directory. Deleting the extracted ZIP
or uninstalling Setup preserves notes, memory, attachments and settings. Deleting
user data is a separate explicit action, never part of ordinary uninstall.

ZIP 免安装版与同渠道 Setup 共用用户数据；删除 ZIP 解压目录或卸载 Setup 都保留笔记、记忆、附件与设置。
当前源码仅允许已确认的 Setup 安装注册通知、协议、开始菜单及自启动入口；ZIP 不注册这些入口，
因此不提供依赖这些注册的系统通知和自启动，应用内消息仍保留。
旧 ZIP 版本已创建的入口不会由新 ZIP 启动时擅自删除，以免影响同渠道安装版。
若历史自启动项的渠道值名与完整命令行精确匹配当前 ZIP，可在设置中显式关闭；关闭后不能在 ZIP 中重新启用。其他安装的条目不会被覆盖或删除。
共享 Host 的退出与剩余接入要求见 [Windows 发布与更新](windows-release.md#shared-host-lifecycle)。

For build, channel and recovery details, see [Windows release operations](windows-release.md).
