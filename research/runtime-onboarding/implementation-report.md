# Runtime 首次连接与设置：本地实现交付

2026-10-13。工作树 `research/runtime-onboarding`，起点 `origin/main` 为 `393b29f5406df739682280c1c87652bbe773c52b`。此前的 [交互草稿](interaction-draft.md) 是评审时的研究快照，其“未正式实现”说明不再描述本交付。当前交付只在此 worktree 和 `Caelis Bot Research` 开发包中；未推送、开 PR、合并或发布。

## 已实现

- 首次先选“连接 AI 模型”或“使用 ChatGPT 登录”。缺 Runtime 时自动开始官方准备，安装中保持“正在进行准备工作”，Caelis 归档有实际已收字节、总量、百分比、五秒滚动速率；没有可靠总量时不伪造百分比。下载无绝对五分钟限时，有连接和无数据保护。安装完成仍须通过账号或至少一个可聊天模型的配置检查。首次完成配置后在当前进程开启对话，无需再重启；选 Caelis 时为装配对应共享 Runtime 在配置之前进行一次重启。
- 默认 CLI、认证与 Store 跟用户终端共用，Codex 默认 `~/.codex`，Caelis 默认 `~/.caelis`。不内置 Runtime、不默认建立第二份 Store。官方脚本安装后重新发现路径、版本和握手；已识别 Homebrew formula/cask 与 npm 全局来源沿原渠道更新。来源不明或收据不一致时拒绝静默并装。共享 Host 更新沿现有 idle、升级和重连生命周期，保留不确定操作的对账。
- Caelis 连接列表取 Host 目录与功能字段；Magpie 放在“三方聚合网关”区域，默认检测本机网关，也可手输地址、手输模型或访问 `usemagpie.ai` 指引。无目录的手输退路和失败后“修改后重试”已提供；密钥草稿在失败返回时清除，凭据由 Host 保存。首次与设置调用同一 `ConnectionWizard` 和 Host 状态机。取消、失败、不确定结果分别保留对应动作。
- 设置有一级 `Caelis Team` 专页，按 Host 返回的成员、系统 Agent、模型能力、effort、Fast 和 binding set 修订读写；未强制给不支持的模型配置字段。使用中模型断开显示用途与共享影响，按主模型/Team/Bot 对话用途给出先换模型、去 Team 解除绑定或显式停止使用的路径，不再只有一个禁用的“移除”。
- 测试专用 `Research` 包有独立 bundle ID、进程单实例键、数据目录、HOME、PATH 与 CodexHome。`runtimeenv` 的真实账号 HOME 恢复在该 build channel 被隔离规则替换；首次选 Caelis 所需的提前重启也显式保留这些隔离根。该 channel 不能当正式安装包。

## 本机原生验收与证据

| 场景 | 结果与截图 |
| --- | --- |
| 空白首次选择、准备、失败恢复 | [选择](screenshots/implementation/01-blank-connection-choice.png)、[准备](screenshots/implementation/02-native-preparing.png)、[安装不确定状态](screenshots/implementation/03-native-install-uncertain.png)、[失败后重做](screenshots/implementation/05-native-redo-preparing.png)、[重做结果](screenshots/implementation/06-native-redo-result.png)。独立 HOME、无复制真实认证。 |
| 官方 Caelis 安装与进度 | `fresh-progress` profile 在隔离 HOME 运行官方脚本。 [实际下载进度](screenshots/implementation/08-measured-progress-live.png) 是 16.8 / 30.7 MB、149.3 KB/s；[安装后待连接](screenshots/implementation/09-install-complete-model-chooser.png)未把 CLI 安装当成就绪。 |
| Magpie 与聊天 | 独立 Caelis Store 经本机 Magpie 连接 `xiaomi/mimo-v2.6-flash`，[已连接](screenshots/implementation/10-magpie-connected-native.png)。`no-restart` profile 首次配置后不重启，提交助手称呼并得到实际模型回复：[聊天截图](screenshots/implementation/18-no-restart-live-chat.png)。这验证此机器本机网关，不代表其他提供方或真实 Codex 账号认证。 |
| 设置视觉与断开 | [设置排版](screenshots/implementation/19-settings-layout-after.png)、[使用中模型断开路径](screenshots/implementation/20-used-model-removal-after.png)。Team [页面](screenshots/implementation/21-team-layout-native-after.png)与 [breeze 模型绑定读回](screenshots/implementation/22-team-binding-readback.png)来自隔离 Host；另存及应用 binding set 的前次原生证据是 [保存](screenshots/implementation/14-team-breeze-saved.png)、[应用](screenshots/implementation/15-team-set-applied.png)。 |
| Magpie 手输 | 修改为无目录地址后出现手输模型退路，[原始截图](screenshots/implementation/23-magpie-manual-fallback.png)、[修正后截图](screenshots/implementation/24-magpie-fallback-final.png)。手输 `127.0.0.1:1/v1` 与测试字符串在 Host 得到失败，未提交到真实服务。最后增加的“修改后重试”按钮已通过 TS/i18n/构建，尚未取得该按钮的原生截图。 |

本次收尾时 CUA 对运行中 Research 包返回 `cgWindowNotFound`；临时诊断日志确认原生 host ready、首次设置窗口 `open=true` 且 `visible=true`，但 CUA 无法再绑定这个窗口。诊断代码已撤回。上表截图是在此前可绑定时留存，最后“修改后重试”按钮与隔离提前重启补丁只有源码/测试/构建验证，**没有冒充新的原生视觉验收**。独立视觉 Worker 应以最终签名包复看这两点。没有操作微信、系统设置或正式 Bot。

## 检查与未覆盖边界

- `make check` 通过，包含来源归属、阶段恢复、Team 修订和隔离重启参数测试。沙箱内的首次全量运行因测试的 `httptest` 无法监听本地端口失败；同一源码在允许 loopback 的执行环境通过。日志在忽略的 `.cache/runtime-onboarding-final-check.log`。
- `make smoke` 在隔离 `HOME/CODEX_HOME` 下通过 Codex stdio initialize 和资源检查，未建立会话或发送模型请求。此结果不是 Codex live auth。日志在 `.cache/runtime-onboarding-final-smoke.log`。
- `BOT_BUILD_CHANNEL=research make build` 通过，并验证完整 Desktop World 嵌套资源与本地开发签名；最终提交后须再构建一次，使 `Info.plist` 的源码 SHA 等于交付提交。
- 没有在真实用户 HOME 执行 brew/npm upgrade、修改 CLI/Store、复制生产认证或进行 Codex 真实账号登录。Homebrew/npm 分支有源码与测试覆盖，真实本机渠道升级、权限提示和回滚仍需独立环境验收。Caelis 官方 `install.sh` 的候选上游改动保存在 [patch](caelis-dev-install.patch)，未改 `caelis.dev`；Bot 当前通过官方脚本完成安装，并在脚本前安全暂存、校验及计量同一官方发布归档。
- Host 对手输错误返回较泛的拒绝原因；UI 可修改后重试，但如需区别地址不可达、认证失败与能力不兼容，Host 需要稳定的错误码。未直接从 Bot 猜测或绕过 Host 认证。Bot skill 未改：本次变更是用户首次引导、设置和本机 Runtime 生命周期，未新增模型可见工具或改变 Bot 的工具使用语义。

## 独立 Worker 复验入口

源码与包：本 worktree `git rev-parse HEAD`；`dist/Caelis Bot Research.app` 的 `Info.plist:CaelisSourceCommit` 应与 HEAD 一致。包仅供本机隔离验收，不是公证 release。

```sh
BOT_BUILD_CHANNEL=research make build
BOT_RESEARCH_PROFILE=manual-error bash research/runtime-onboarding/launch-isolated.sh --restart
```

`manual-error` 是无认证、无模型的独立 profile，CLI 只复制了已在另一隔离 profile 中由官方脚本安装的可执行文件。可用“连接 AI 模型”→ Magpie → 手输地址 `http://127.0.0.1:1/v1`、任意非秘密模型 ID、测试密钥 → 失败 →“修改后重试”复验错误恢复。`no-restart` 已有真实本机 Magpie 模型配置；用于设置、Team 读回和聊天，运行命令只需把 profile 改为 `no-restart`。`fresh-progress` 保留实际下载安装现场，可读截图和隔离 Store；不要重跑未知安装事务。三者的数据在 `.cache/runtime-onboarding-research/profiles/<profile>/`，未纳入 Git。只通过 [隔离启动脚本](launch-isolated.sh)启动，不直接从 Finder 打开 Research 包。
