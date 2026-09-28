# 本机开发签名与权限复用

默认本地/PR 构建使用 **Caelis Bot Dev.app**，bundle ID 为 `dev.caelis.bot.dev`，
单实例与默认数据目录 `~/Library/Application Support/Caelis Bot Dev` 独立于生产版。
原生产数据与 TCC 授权不会自动迁移。正式 tag 构建（或显式 `BOT_BUILD_CHANNEL=release`）
才使用 `Caelis Bot.app` / `dev.caelis.bot`；开发启动只重启对应 bundle 路径。
默认签名仍为 ad-hoc。需要反复验收辅助功能、自动化和静态快照时，
可以显式选择本机已有的 **Apple Development** 身份。私钥留在登录钥匙串，
不导出、不修改钥匙串 ACL、不使用正式发行证书；开发包不因此成为已公证发行包。

1. 在 Xcode → Settings → Apple Accounts 中登录，选择团队 → Manage Certificates →
   ＋ → Apple Development。登录、创建和系统钥匙串确认由用户完成。
2. 用 `security find-identity -v -p codesigning` 核对可用的 Apple Development 身份，
   将其 40 位证书指纹保存到 `.development-signing-identity`（一行，无引号）。
   该文件已被 Git 忽略，只保存公开指纹，不保存凭据。
3. 用 `bash script/build_and_run.sh --verify` 构建并启动。构建会验证该身份类型、
   签署内部 Sparkle、Computer Use 原生组件及应用、核验 Apple 签名链和应用标识。
   身份失效/缺少私钥时停止，不静默退回 ad-hoc 或发行证书。

单次覆盖用 `BOT_DEVELOPMENT_IDENTITY=<指纹>`；显式 `-` 表示 ad-hoc。
CI 忽略本机选择和该覆盖，保持凭据无关的 ad-hoc 构建。正式发行仍由原有隔离
Developer ID / 公证流程完成。

首次从 ad-hoc 切换到开发签名仍需系统授权。之后保持证书、bundle identifier 和
应用路径稳定，可以避免每次代码哈希变化导致的身份切换；不能保证 macOS 永不
再次询问。证书过期、变更或系统权限政策变化仍可能需要重新授权。

授权后若当前进程仍报告未授权，使用 `bash script/build_and_run.sh --restart`：
先正常退出开发进程，再启动**同一份二进制**，不重新编译或签名。
`--recall` 仅唤回已有应用，不刷新进程权限。普通用户可从 Bot 菜单退出后再打开。
不要为解决“开关已开”而自动 reset 系统授权。

稳定身份验证应比较两次不同代码的 `codesign -d -r-` designated requirement；
它应保持相同且不依赖 cdhash。签名一致并不等于真实权限/窗口截图已验收，仍需
在用户授权后的同一开发副本完成点击、最小化、恢复与卡片快照检查。
