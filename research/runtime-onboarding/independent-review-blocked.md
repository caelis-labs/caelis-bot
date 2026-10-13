# Research 独立原生复验受阻记录

2026-10-14 01:13 CST。目标是复验昵称真实模型回复与重启保持、以及断开唯一模型后的协作入口。结论：**未完成原生验收，不能判定两项修复通过或失败。**

- 原实现 worktree `HEAD=efd223cb0508223f5aceecda4a5e4ac7e3b0946b`，`git status --porcelain` 为空；Research `Info.plist` 的 `CaelisSourceCommit` 为相同 SHA，`CFBundleIdentifier=dev.caelis.bot.research`。主程序 `dist/Caelis Bot Research.app/Contents/MacOS/caelis-bot` 的 SHA-256 是 `23f4e30a83c96a6efc4f86f6a0d3f507b445bb509e34ab664b61955b7528f9d6`，复核时未改变。
- 启动前 `.cache/runtime-onboarding-research/profiles/review-efd223cb-1` 不存在。只用 `BOT_RESEARCH_PROFILE=review-efd223cb-1 bash research/runtime-onboarding/launch-isolated.sh --restart` 启动指定包。第一次在 shell 沙箱中 `open` 返回 `kLSNoExecutableErr`；按授权以同一命令在原生环境重试成功，输出 `Caelis Bot native host is ready`，任务日志也记录 `Desktop native host ready`。新 profile 的 `home`、`.codex`、`data` 均为 `0700`，HOME 下没有文件；未复制认证、CLI 或 Store，未点击安装。
- CUA 首次精确绑定 `cua.getApp('dev.caelis.bot.research')` 返回原文：`Computer Use was not approved to use Caelis Bot Research`。遵守拒绝后没有换名称、路径或窗口 ID 重试，也没有用 AppleScript、System Events、CGEvent、Finder 或其他应用控制。没有原生 AX/截图，因此没有昵称回复、断开状态、窄窗或键盘操作证据。
- 沙箱内只读 `curl` 本机 `127.0.0.1:3425/v1/models` 连接失败；原生权限下同一无凭据只读请求返回 HTTP 200，证明当时 Magpie 网关可达。没有打印模型目录或密钥，也没有连接或发送模型请求。

后续需要为精确 Research 目标授予 CUA 使用权限，再由独立 reviewer 用另一个全新隔离 profile 完成原生流程；当前 `review-efd223cb-1` 已由应用启动生成数据目录，不能再称其目录全新。不可将本记录作为两项产品修复的通过证据。
