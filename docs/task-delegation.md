# Bot 与工作任务

Bot 是用户与专业工作之间的常驻秘书。简单问答、提醒、日常协调直接处理；代码、调研、
分析和制品制作默认委派给有独立工作目录的任务。用户只需要表达目标，不必额外说“创建 Thread”。
Bot 负责准确传达范围、继续和停止任务、核对产物并汇报；工作任务的流水不进入主聊天。

## 已实现的首个切片

`internal/tasks` 实现应用层 `TaskProvider`，通过 `internal/backend/api/work.go` 的
`WorkRuntime` / `ReportSubmitter` 调用原生执行，不携带 Codex 原生 ID 给模型。
Bot MCP 与通用 `ApplicationTools` 共用业务实现，提供 `bot_tasks`、`bot_task_start`、`bot_task_read`、`bot_task_send`、`bot_task_stop`。
Codex adapter 映射至 App Server 的 `thread/start/read/resume`、`turn/start/steer/interrupt`。
这些是 Bot 自己的接口，不依赖 Codex App 的 `codex_app` 插件或其私有 IPC。
后续 provider 实现同一能力契约；没有该能力时明确返回不可用，不降级为秘书直接执行专业工作。

- 每个任务在应用数据目录的 `Tasks/<opaque-handle>` 中独立工作；模型不能指定原生 Thread ID、
  自选任意目录或扩大 writable roots。主 Bot 的 `Work` 仍只用于临时输入与协调。
- 最多三个未结束任务；初版记录最多 100 个任务、每任务 100 次请求，达到容量时明确拒绝。
  创建与继续均需要稳定 requestId；同一 ID 不同内容拒绝，未知回执先查证，不自动重发。
- `tasks.json` 是产品账本，记录 Runtime 归属、创建 intent 和汇报回执；`conversation.json`
  仍由 Codex adapter 保存 native binding、原始用户要求和原生请求回执，不复制规范 transcript。
  先保存产品 intent 再分配目录；创建 Thread 后先保存 native 归属再提交工作；未知不自动重建。
  现有 Codex 任务句柄与已投递报告可导入，重试原 requestId 不会再建任务。切换 Runtime
  保留产品账本，但不能操作另一 Runtime 的工作；旧 Caelis Bot 数据不自动导入。
- 主 Bot 回合结束后，后台任务继续运行，聊天可接受新的用户请求。必要审批仍优先处理。
- 原生事件和仅针对活动任务的只读检查跟踪状态。每个任务回合结束后最多排队一次汇报激活；
  只在秘书空闲时发送，不 steer 用户的另一个请求，也没有空闲模型轮询。
  秘书已读取结果时取消重复通知；停止所有工作后不再以完成事件重新唤醒模型。
- 暂时隐藏角色、关闭面板不停止任务。明确退出沿用原有清理：中断已拥有的活动回合并清理工具进程。

## 授权边界

创建受管目录和工作任务是履行用户请求的内部调度步骤，五项任务工具逐项加入允许列表，
没有 server/global 默认放行。宿主要求处于用户请求或已授权提醒激活的主 Bot 回合中。
仅工具调用本身不构成对其描述的外部行为的授权。

宿主从实际用户提交保存原始要求，连同委派内容送入工作任务；模型不能用一个 `authorized: true`
字段扩权。原始请求、当前请求与秘书委派都被明确区分，委派不能扩大用户范围。工作任务继承
用户选择的模型、effort、速度档位与审批模式；默认仍为 `workspace-write / on-request / auto_review`。
命令、网络、外部写入、发布和额外权限继续接受原生策略审查，拒绝不会被任务层自动重试绕过。
需用户决定的审批保持原生动作、目录和选项，并显示所属任务名称。

工作任务不注入 Bot MCP 凭据，关闭进一步创建 agent 的能力。完成通知只携带可信宿主的任务
句柄与状态，不把工作任务输出当用户指令或新授权。模型需要调用读取工具核对结果。

这是一套范围明确的产品授权与运行时边界，不声称能用字符串匹配判断所有自然语言授权。
专业任务是否需要委派由固定角色指令引导；具体执行的权限仍由原生后端保证。

## 后续扩展

本轮不自动管理 Codex App 已存在的任务，也不把“列举本机全部 Thread”等同于拥有它们。
接入已有项目时需要增加用户选择的项目授权记录和工作目录策略：Git 项目使用独立 worktree，
非 Git 项目明确是否就地操作。跨应用任务的只读发现、显式接管、归档和配额回收属于后续切片。
不通过注入 Codex App 当前会话的私有 pipe 来获得这些能力。

验证区分契约测试、真实 Codex 联调和原生界面；最新实际结果见 `preparation-status.md`。
