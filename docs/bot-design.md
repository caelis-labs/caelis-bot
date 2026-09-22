# Bot 身份、唤醒与对话界面

本轮确认：2026-09-19。适用于 macOS 首版；Windows 在 macOS 完整发行后实现，Linux 不在当前计划。
2026-09-20 路线补充：保持本文件的身份/唤醒边界，角色表现与有限空间交互按
[产品发展路线](roadmap.md)推进；技术五段动画已接入，表现力仍需验证。
同日交互框架补充见[桌面概念与交互基线](desktop-behavior.md)：最小桌面/Dock/前台窗口、
待机和鼠标/工作响应，以及独立道具都由本地层执行；纸飞机不引入新的 Bot/Thread 或持续模型调用。

## 一个长期 Bot

用户始终在和同一个 Bot 聊天，不创建/切换 Session，也不选择工作区。产品身份保存在
`bot.json`，独立于 Codex 内部绑定、后台工作 Thread、角色资产和桌面位置。
现有 `conversation.json` 原样延续，不通过清空聊天迁移新功能。

参照同父目录 Caelis 主仓库的 `docs/bot.md`、`control/bot` 和 `app/gatewayapp/bot_runtime.go`：
长期身份、产品私有对话、固定指令前缀、按需运行；不引入私有包依赖。
主仓库当前纯聊天限制不成为桌宠的能力上限。

Codex start/resume 显式传入 `runtimeWorkspaceRoots: []`。私有 `Work` 目录仅是后端
文件输入与产物的落点，不是用户项目。Bot 指令前缀固定；用户 prompt 和定时激活作为新的
输入追加。专业工作经 provider-neutral 任务接口委派到独立目录；宿主跟踪拥有关系、审批目标、
生命周期与完成通知，不把内部流水混进用户聊天。常驻 Bot 是秘书，不是默认的专业执行工作区。
边界与首版限制见[任务委派](task-delegation.md)。

## 三种界面的职责

| 界面 | 内容 | 操作 |
| --- | --- | --- |
| 空闲输入胶囊 | 草稿、附件、插件/技能引用 | Enter 发送、Shift+Enter 换行；接受后收起，未知/拒绝保留 |
| 角色上方消息气泡 | 最新助手回复、必要状态、审批 | 主体打开聊天；运行时停止；完成后 ✓ 收起结果；审批在原气泡展开 |
| 可选聊天窗口 | 用户与助手消息、文件结果、必要审批和生命周期 | 持续发送/运行中补充、停止、处理审批；不显示默认工具流水或新会话 |

全部使用一套语义配色并跟随系统明暗。聊天窗口保留可读性，浮层只使用轻量材料。
草稿由宿主统一持有，跨界面连续；带版本写入避免迟到窗口覆盖。附件是共享的临时选择。
聊天阅读旧消息时保持滚动位置，以“查看新消息”跳回底部；位于底部时跟随输出。

空闲点击角色切换输入；工作中点击打开聊天；待审批时点击展开消息气泡。
到达新消息或审批不会主动取得键盘焦点。只有用户明确展开审批才允许气泡接收键盘输入。
外部点击或 Esc 折叠审批，不构成批准/拒绝。原生选项、动作、对象和授权范围均保留，
长期/规则授权在“更多授权方式”中。一处处理后各表面同步失效，不重发决定。
聊天窗口处于前台时原生宿主隐藏重复气泡；离开、最小化或关闭后恢复。
✓ 只确认当前结果，写入 `preview.json`；后续消息/审批不继承旧确认，不删除历史。
关闭窗口、隐藏宠物、停止工作和退出应用有各自语义。

## 初版定时激活

应用常驻是触发前提。隐藏角色照常触发；系统睡眠期间的同类到期项合并，醒来后处理一次。
明确退出后不另设系统守护进程唤醒；重启时跳过退出期间错过的时点，循环项安排下一次，
过期单次项停用。退出前已排队的提醒仍作为未处理工作保留。

支持单次带时区时间、1–10080 分钟间隔、指定 IANA 时区的每日 HH:MM，最多 32 项。
每日时间随当地日历而非固定 24 小时；夏令时不存在的时点跳过当天。
通过聊天创建、列出、修改、移除提醒，沿用用户授权与后端原生审批，不增加任务控制台。
重复保存同一 ID 和相同内容不改变下次时点。

调度器只是本地时间检查，不持续调用模型。到期先持久保存 occurrence，再提交到现有 Bot。
忙碌、审批或断线时排队，不偷偷 steer 当前工作。一个队列批次合并多项提醒；未知发送不重放，
通过原生 receipt 对账，无法确认时在聊天中提示。不是 exactly-once 执行保证。

## Agent 驱动角色与后端接入

应用自带 stdio MCP 工具 `bot_clock`、`bot_reminders`、`bot_gesture` 及五项 `bot_task*` 工具；以当前进程专用配置
注入 Codex start/resume，不修改全局 `~/.codex/config.toml`。这样已有对话也能接入新工具，
无需重新创建绑定。Codex 0.153.4 为开发/回归基线，不限制用户 CLI 发行版本；
按[兼容策略](codex-compatibility.md)握手。MCP 子集固定为 2025-06-18 的 initialize/tools。

stdio 子进程通过每次启动的私有 Unix socket 联系宿主：目录 0700、socket 0600、随机令牌、
有界消息/超时；没有公开 HTTP 端口、任意脚本或任意 renderer 调用。单实例所有权确认后
才创建调度器/endpoint，避免第二次打开应用改写运行中提醒。退出关闭 endpoint 和调度器。

当前 `bot_gesture` 支持 `attention`、`nod`、`celebrate`，已映射小爱实际骨骼 clips；
idle/working 根据后端事实选择，隐藏/减少动态效果时停止短动作，不抢焦点、不改位置、不替代审批。
这已替代火柴人时期的灯光脉冲占位，但不具备注视/主动转向、空间移动或完整动作回执。
接收成功不等于可见动作完成。下一步的本地行为调度、角色能力与回执见
[计划中的架构边界](architecture.md#planned-embodied-behavior-boundaries-2026-09-20)，不能按现有 API 宣传。

Codex 默认显式使用 `approvalPolicy: on-request`、`approvalsReviewer: auto_review`，
保留 `workspace-write` 沙箱。只有 `caelis_bot` 中的 `bot_clock`、`bot_reminders`、
`bot_gesture` 和五项限定于 Bot 自有任务的工具逐项设置 `approval_mode: approve`；不设置全局或 server 默认批准，
新工具不会自动继承此允许列表。定时任务本身只创建激活计划，激活后的命令、外部工具与
额外权限仍沿用各自原生策略；需要用户决定的事件继续进入气泡/聊天。

固定版本的发现有两条原生路径：普通工具模式的 `tool_search` 在发现前展示来源名称与
可选简介；Code Mode 提供可查询的 `ALL_TOOLS` 元数据（工具名称 + description），具体
MCP 声明可以先不放进初始提示。代码转换器不会把 ToolSearch spec 变成 Code Mode
嵌套工具，所以不能强制模型调用一个当前模式没提供的 `tools.tool_search`。
**基础元数据可检索，但不等于每项已直接展示在初始上下文。**

0.153.4 的 `features.tool_search` 和 `features.tool_search_always_defer_mcp_tools` 已是
removed/no-op 兼容项，不设置它们。应用保留原生渐进发现，只在接入自有 MCP 时追加
“名称 + 用途”目录和按当前模式发现的提示，不注入完整 schema，不改模型模式。
真实调用已验证旧绑定恢复后也能发现并执行这些工具，未采集模型初始完整请求体，
不声称所有模型/远程配置都具有相同初始列表。

固定 tag 依据：[tool_search 来源列表](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/core/src/tools/handlers/tool_search_spec.rs)、
[MCP 检索元数据](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/core/src/tools/handlers/mcp.rs)、
[Code Mode 发现说明](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/code-mode-protocol/src/description.rs)、
[工具转换](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/tools/src/code_mode.rs)、
[失效配置标志](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/features/src/lib.rs)。

自有 App Server 清除启动者的 CODEX 运行身份与桌面 IPC 环境，只保留 CODEX_HOME/API_KEY
及常规环境，防止 Bot 偶然依赖开发宿主任务。后台任务兼容旧版 collab 与新版
`subAgentActivity` 原生事件；按原生目标保存拥有关系，活跃任务必要时通过 `thread/read`
核对，终态后停止观察。工作内容仍不混入用户聊天。

## 验证与限制

行为测试覆盖休眠长间隔合并、忙碌不 steer、写盘失败不派发、未知不重发及 receipt 对账、
退出期间跳过、每日时区、重试幂等、删除不丢其他排队项、私有通道鉴权、草稿版本隔离、
结果确认持久化、工作 Thread 审批目标及停止路径。真实/native 结果见 backend-acceptance.md。

仍需发行级验证：真实硬件睡眠/长时间运行、明暗切换组合、长历史阅读锚点、工作 Thread
常见崩溃/断线恢复、正常账户首次授权。聊天已有 Markdown/复制、长历史分页；原生系统通知
已接入且授权链路已验，横幅/点击返回仍待确认。角色隐藏时定时工作仍执行，
不承诺声音或退出后的后台唤醒。最新证据与限制见[准备记录](preparation-status.md)。

来源：[Codex App Server](https://learn.chatgpt.com/docs/app-server)、
[Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)、
[MCP 生命周期](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle)。
能力以仓库固定 schema 和真实二进制验收为准。
