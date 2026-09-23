# Caelis 应用执行基础设施重构：可直接交给 Agent 的 Prompt

在 `/Users/xueyongzhi/WorkDir/caelis-labs/caelis` 实施本任务。
Caelis Bot 正在另一个会话中同步重构；你负责 Caelis 仓库，不修改 Bot 仓库。
先检查工作树，保留其他人的修改，不覆盖并行工作。

## 授权与产品决定

用户明确授权：**重新实现 Caelis 为上层应用提供的通用执行基础设施，可删除已不需要的 Bot 实现；
旧 Bot Mode 不需要兼容。** 不保留专用 Bot API、TUI、工具或调度实现来照顾旧 Bot 客户端。
这覆盖较早文档中的 Bot Mode 保留/迁移兼容要求。但不授权删除用户实际数据目录、凭据、
普通工作区会话或 Memory 数据；旧数据不支持时给出明确结果，不做破坏性自动清理。

目标是 Caelis Core/App Server 的通用应用扩展能力，不是在 Core 内重新实现一个桌面助手。
Bot 产品的身份、长期笔记、记忆策略、任务编排、定时计划、角色动作和工具业务归 Caelis Bot。
普通 Caelis CLI/TUI、模型配置/登录、Workspace Memory、MCP、ACP 和 Agent 执行能力继续保留。

先完成实现、回归测试与审计证据，交付可 Review 的改动。此 Prompt 不授权 push、tag 或发布；
提交前遵循本仓库要求，最终提供准确的版本/提交基线供 Bot 侧联调。

## 必读资料

Caelis 侧：

- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/AGENTS.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/docs/architecture.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/docs/testing.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/docs/application-runtime.md`（已有时优先阅读并继续完善）
- `control/appserver`、`control/memorybinding`；若并行工作已建立，则读取 `control/application`
  与 `app/gatewayapp/application_*`，不要重复创建另一套方案。
- 旧 `docs/bot.md`、`docs/bot-backend.md`、`app/gatewayapp/bot_runtime.go`、`control/bot`
  仅用作删除范围的历史对照；已移除时从 Git 基线读取，不恢复旧文件。

Bot 侧的并行契约（只读）：

- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/docs/runtime-extension-contract.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/docs/bot-platform-architecture.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/contract.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/tasks.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/work.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/application_tools.go`

Go 消费端口不规定 wire 命名；最终协议需要完整表达下述权限、来源、恢复与资源语义。

若旧文档与本 Prompt 冲突，以这里的用户决定为准；维护的 Caelis 文档应随最终实现更新，
不要把旧 Bot 专属路径继续描述为目标架构。

## 必须建立的通用边界

1. **应用会话与归属**：受鉴权 principal/application 作用域约束的 create/list/read/resume/prompt/
   steer（如支持）/cancel/history/archive。不能仅凭 session ID、metadata 或客户端字符串获得归属。
2. **显式会话配置**：应用提供版本化的角色指令、工具目录、允许工作目录和配置继承策略。
   不隐式继承普通 CWD 的 AGENTS、全局 MCP、技能或 Workspace Memory；继承必须显式且经过授权。
   应用不能声明超出 native sandbox/approval 上限的权限。Agent SDK 保持产品无关。
3. **应用工具注入**：支持会话受限的 MCP 或通用 client-tool callback，由上层实现业务。
   优先采用能携带可信 native 调用归属、重连查询和取消的方案；不要把 `bot_*` 实现搬进 Core 的
   另一目录充当抽象。Runtime 管工具调用历史和策略，应用管自己的效果和业务存储。
4. **可信调用上下文**：绑定 principal、应用连接/租约、session/turn/item/call、工具目录版本和来源。
   模型不能通过参数选择授权对象。来源区分真实用户输入、应用摘要、外部材料和受授权后台触发；
   不将助手生成的任务说明转换成新的用户授权。Core 不理解提醒规则或宠物动作。
5. **幂等与恢复**：稳定 operation/call ID，intent-before-effect，确定成功/失败/未知，精确 native target，
   事件重放或快照对账。断线后查询/重送原 ID 不产生第二个会话、第二次工具副作用或第二个执行。
   如果外部效果无法证明 exactly-once，应如实保留未知；不要用内存去重作持久化保证。
6. **文件输入与成果**：提供通用执行资源契约，明确资源归属、允许根目录、名称/类型/大小/摘要、
   输入交付和输出读取。首版可仅同机、受控复制/共享目录，但不能要求 Bot 直接读取 Control Store
   或猜测 worker 私有目录。模型给出绝对路径不是访问授权。提供可消费的结构化结果描述。
7. **生命周期**：应用工具宿主断线、租约撤销、UI 关闭和用户取消是不同事件；共享 Host 不被客户端
   随意退出。必须工具失联后失败或等待恢复，不能回退到宽权限内置工具。
8. **Memory 边界**：Bot 在自己进程嵌入独立 Memory，并经应用工具供模型使用。同一 Bot 的个人资料、
   Notebook、Memory 可跨 Caelis/Codex 使用。Core 不要求按 Runtime 重建个人身份，也不持有这些资料。
   普通 Caelis Workspace Memory 保持现有语义，不混库、不自动授予应用会话。

会话配置/工具版本只在明确、可恢复的安全边界更新；在途 Turn 不改工具 schema 或已提交的模型前缀。
普通应用笔记变化只是新增读取结果，不触发隐式 compaction。调用原生执行权限仍沿用现有审批，
应用“创建工作”授权不等同于执行任意 shell/外部写入。

## 允许删除与必须保留

允许删除旧 Bot TUI 入口、Bot Mode 路由/专用 client/能力、固定 Bot 产品工具组装、
专用受管工作/提醒/桌面 effect 产品逻辑、仅服务这些路径的测试/fixture/文档。
先画出引用和所有权，再删，不能只删菜单或把已弃用逻辑长期藏在兼容分支中。

必须保留或泛化后保留：Control 的规范执行事实、操作 ID、审批、沙箱与目录约束、
Host 生命周期、普通工作区会话、通用 Memory binding、普通 CLI/TUI 配置和模型登录、
MCP/ACP/runtime SDK 的通用行为。不要用一次全仓 `bot` 字符串替换完成删除。

不要求旧 Bot API 继续成功，不要求导入旧 Bot 数据，也不实现旧 Bot 双轨调度或旧客户端兼容层。
已有数据库打开仍应受 schema 版本约束且不能误删普通数据；旧配置拒绝或忽略的行为要有明确测试。
不得扩大任务沙箱来迁就上层，不把本地绝对路径或用户内容当作授权凭证。

## 交付节奏与跨仓库协作

先在 Caelis 仓库产出或完善 `docs/application-runtime.md`，列出实际 wire 方法、schema、能力标识、
权限语义、错误类别和测试入口；同时提供机器可读 schema/fixtures。不要只交一份概念说明。
能力标识、wire 字段定稿后尽早交还 Bot 侧，再继续实现与回归；Bot 不通过私有 sibling import 接入。

可独立交付的切片：

1. 应用 scope/profile 与通用会话授权；
2. 应用工具注册/回调/结果/取消/恢复；
3. 执行资源与产物传输、历史与归档；
4. 删除旧 Bot 产品路径并更新文档，完整回归。

复用现有 Control 原语，避免再造执行器、通用 workflow 引擎或另一份 transcript。
每片应能独立 Review，记录依赖和暂缺能力，不能通过空实现或永远成功的 stub 声称完成。

## 可供 Review 审计的验收标准

每个编号在交付报告中映射到具体测试名、源文件、命令和结果。安全/恢复断言优先用确定性的
进程/事务/消息边界故障注入，不用 sleep、宽松断言或仅检查工具 schema 作为证据。

| 编号 | 必验场景 | 可接受的证据 |
| --- | --- | --- |
| A01 | 一个不含 Bot 依赖的最小应用创建并恢复自有会话，注入自定义工具，取得模型调用与结果 | 走实际 Host/公开 client 的集成测试及脱敏事件序列；可控模型可用于协议测试，注明不是真实模型验收 |
| A02 | 两应用、两会话、并发 Turn 不串工具、资料或权限 | 交叉使用 session/resource/connection/call ID、伪造 metadata、过期租约、伪造来源均被拒绝；自身合法请求成功 |
| A03 | 会话隔离和配置继承确实生效 | 放置 CWD 指令、全局 MCP/技能和 Workspace Memory 哨兵，检查最终 runtime 组装/模型请求；未显式启用的不出现、不被执行 |
| A04 | Native 权限不因应用工具/任务委派被扩大 | 应用内已授权委派正常；越界路径/网络/外部写入沿原政策拒绝或生成精确审批；无宽权限 fallback |
| A05 | 调用/创建/结果各边界断线重启 | intent 持久后、效果后回执前、结果持久后回包前分别注入失败；恢复原 ID 对账；不重复效果；不同参数复用 ID 冲突 |
| A06 | 工具失联、撤销与取消 | 在途结果和新调用有确定处理；撤销后不能启动新效果；晚到回执不复活已失效绑定；UI 关闭不等于取消 |
| A07 | 来源与自动审批边界 | 模型伪造 userApproved/sessionId/旧对话不新增授权；真实用户请求及合法后台 grant 可在原范围内执行 |
| A08 | 文件闭环与边界 | 输入实际字节→执行→结构化产物→公开接口读取；验证大小/摘要；越界、symlink、TOCTOU/变更、过期资源有确定拒绝 |
| A09 | Prefix 与配置版本 | 模型请求/live-replay round trip 证明在途前缀/工具集不漂移；静止边界升级持久；应用普通数据更新不重写历史 |
| A10 | 规范历史与终态 | 分页/reconnect/归档后保持 identity 和审批目标；生成文字 Done 不代替 native 终态；取消结果未知保持未知 |
| A11 | 旧 Bot 移除完整 | 列出删除入口/路由/工具/依赖；不存在专用 Bot 实现的替身；旧模式失败清晰；普通数据不被迁移脚本或启动过程误删 |
| A12 | 普通 Caelis 无回归 | CLI/TUI、模型连接、工作区 Memory、MCP、ACP、普通 Session/审批 owning tests 通过；变更过的持久化/replay 有 round-trip |

新增/改动 Go 代码执行 gofmt、相应 owning tests；依照 `docs/testing.md` 选补充回归；
提交前 `make commit-check`，并运行 `git diff --check`。模型测试必须使用隔离目录，
不打印凭据或完整私人对话。真实模型/原生平台/发行验收未做就写未做。

最终交付：

- 当前基线和变更列表，通用契约文件与公开 schema 路径；
- Bot 侧可调用的方法、请求/响应样例、错误语义、版本/能力协商方式；
- 可复制的构建与隔离联调命令，明确产出的 Caelis 二进制位置；
- A01–A12 对照表，缺口、风险和仍未覆盖的实机/真实模型场景；
- 明确声明旧 Bot Mode 已移除、不提供兼容，且未删除用户真实数据、未 push/tag/release。
