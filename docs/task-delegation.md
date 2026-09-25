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
- Codex 通过原生订阅事件跟踪状态，连接恢复只做一次订阅/对账，不长期轮询。每个任务回合结束后最多排队一次汇报激活；
  只在秘书空闲时发送，不 steer 用户的另一个请求，也没有空闲模型轮询。
  秘书已读取结果时取消重复通知；停止所有工作后不再以完成事件重新唤醒模型。
- 暂时隐藏角色、关闭面板不停止任务。明确退出沿用原有清理：中断已拥有的活动回合并清理工具进程。

## 授权边界

创建受管目录和工作任务是履行用户请求的内部调度步骤，五项任务工具逐项加入允许列表，
没有 server/global 默认放行。宿主要求处于用户请求或已授权提醒激活的主 Bot 回合中。
仅工具调用本身不构成对其描述的外部行为的授权。

宿主在账本和原生来源记录中保留用户要求与任务归属，worker 收到 `bot_task_start/send.prompt`
的原始正文，不追加重复的 JSON 包装或通用限制提示。Bot 在核心 skill 的任务模块中学习如何
保留目标、必要上下文及真实用户约束；模型不能用一个 `authorized: true` 字段扩权。工作任务采用
独立的工作模型、effort 和速度档位；审批模式沿用既有产品权限策略，默认仍为 `workspace-write / on-request / auto_review`。
命令、网络、外部写入、发布和额外权限继续接受原生策略审查，拒绝不会被任务层自动重试绕过。
需用户决定的审批保持原生动作、目录和选项，并显示所属任务名称。

工作任务不注入 Bot MCP 凭据，关闭进一步创建 agent 的能力。完成通知只携带可信宿主的任务
句柄与状态，不把工作任务输出当用户指令或新授权。模型需要调用读取工具核对结果。

这是一套范围明确的产品授权与运行时边界，不声称能用字符串匹配判断所有自然语言授权。
专业任务是否需要委派由 Bot 核心 skill 引导；具体执行的权限仍由原生后端保证。

## 后续扩展

本轮不自动管理 Codex App 已存在的任务，也不把“列举本机全部 Thread”等同于拥有它们。
接入已有项目时需要增加用户选择的项目授权记录和工作目录策略：Git 项目使用独立 worktree，
非 Git 项目明确是否就地操作。跨应用任务的只读发现、显式接管、归档和配额回收属于后续切片。
不通过注入 Codex App 当前会话的私有 pipe 来获得这些能力。

验证区分契约测试、真实 Codex 联调和原生界面；最新实际结果见 `preparation-status.md`。

## 工作模型选择（2026-09-24）

Bot 模型与工作模型解耦，设置按 Runtime 分别保存在 `work-execution.json`。
未保存文件或空 `model` 表示沿用 Runtime：新建任务先读取 Runtime 配置，未配置模型才回退
Bot 模型；手动选择优先。模型、effort、速度作为一组解析，不把 Bot 的档位叠加给其他模型。
配置读取失败、无权限或无法确认默认模型时明确报错，不冒充“未配置”静默回退。

Codex 使用公开 `config/read` 的 effective config，而不是 `model/list` 的推荐默认项；
创建回执的模型/provider/effort/tier 写入任务记录。旧任务首次续接不覆盖模型，从原生恢复回执
补齐记录。Caelis 从公开 Host 模型目录读取 `current` 及档位，必要时使用结构化 `/status`
标识；在持久化创建意图前写入独立 application profile。既有 application 会话保持原配置。
保存工作偏好不改运行中的任务、Bot 对话模型或 Runtime 全局默认；模型工具不能提交工作模型参数。

Caelis 的 Agent team 在“运行时与模型”中配置，与本机 TUI `/team` 共用角色绑定和方案。
这是 Runtime 配置，不为每个 Worker 另建 team；新任务相当于用户在本机 Caelis 发起工作。

## 本机工作环境与诊断（2026-09-24）

macOS 原生宿主在启动 Runtime 前，从用户的登录/交互 shell 恢复导出的环境变量，
包括 PATH、工具管理器路径和用户导出的 Runtime 配置。Codex App Server、原生子 agent、
独立工作任务，以及 Bot 调用 `caelis service start` 创建的 Host 都继承这套环境，
不以 Finder 的最小 PATH 代替用户终端环境。shell 探测只执行一次，有超时与输出上限；
失败保留原环境并记入私有日志。用户显式指定的隔离数据目录和 CODEX_HOME 保留。
已经运行的共享 Caelis Host 继续使用其启动时的环境；普通连接不重启它。
显式升级经原生服务生命周期选择新版，忙碌检查和共享客户端边界见 [接入契约](caelis-integration.md#安装更新与启用服务)。

普通工作任务继续使用 Runtime 自己的用户配置、MCP、插件和工具发现，不复制另一
Codex App 的会话身份/IPC，也不继承秘书专属工具凭据与 Notebook 指令。任务目录、
模型偏好和原生审批仍由既有产品合同提供，环境恢复不扩大文件或操作授权。

错误日志位于应用数据目录的 `Logs/error.jsonl`；单文件最多 2 MiB，含活动文件共
最多 5 份（`error.1.jsonl` 至 `error.4.jsonl`），超过 7 天的文件在下次写入时清理。
启动也写入环境恢复结果，因此会清理上次运行遗留的过期文件。目录/文件分别为
0700/0600。记录 UTC 时间、组件、错误分类、事件 method、原生关联标识、脱敏原因、
载荷大小和 SHA-256，供与 Runtime 日志交叉核对；不保存原始载荷或凭据。
诊断导出只带日志写入状态和保留策略，不包含日志正文。日志写入失败计入诊断状态，
不生成额外聊天错误。

## 轻量任务气泡与共享终端（2026-09-24）

macOS 桌宠有任务时，脚边显示一个收起的 `···` 胶囊。悬停或点击展开相同的编号小圆球，
移出约 420 ms 后收起；悬停某个圆球在角色上方显示创建任务时的原始 `prompt`，最多两行，
长文本尾部省略。这里不生成摘要、分类或图标，也不增加任务详情/审批/结果管理页面。
超过一行的任务使用原生横向滚动；悬停期间只冻结顺序，状态仍实时更新，避免新事件使鼠标下的目标改变。
存在活跃任务时收起胶囊显示转圈动画，展开后在对应编号周围显示进度环。完成立即停止；
审批、失败和结果未知不冒充运行。隐藏后停止动画，减少动态效果时使用静态进度环。
窗口先设置目标内容尺寸，再安装内容视图，避免 AppKit 将展开视图裁剪到旧胶囊尺寸。
旧任务没有原始 prompt 时明确显示缺失提示，不用标题冒充。

点击由宿主解析当前 Runtime 中 Bot 自有的任务，再用默认关联终端打开一个私有 `.command`。
命令只包含宿主确认的 CLI、endpoint、原生会话、工作目录及 Runtime 数据路径。
Codex 使用 `codex --remote unix://… resume …`；Caelis 使用
`caelis attach --control-url http://127.0.0.1:… --session … --store-dir … --control-token-file …`。
不把任务文本或凭据内容拼进 shell，不创建替代任务。
脚本目录/文件为 0700，同一任务原子覆盖同一个文件，数量随现有任务账本上限受限。
终端是用户的观察与输入端，Bot 继续通过标准 App Server 协议通信。

Codex 自建 Runtime 由 stdio 改为短路径私有 Unix socket：目录 0700、socket 0600，
仍继承用户本机导出环境与配置。现有标准控制 socket 优先复用，关闭客户端不杀共享服务；
Bot 自建进程继续遵守显式退出和工具清理。原生握手报告的 Codex home 用于终端 attach。
需要安装版 CLI 同时支持 Unix App Server 和 remote TUI；本轮扩展实测为 0.156.1，
既有协议 schema 基线不作为 CLI 版本允许名单。

任务创建已经订阅，重连时对所有已拥有任务做一次 `thread/resume`，包括空闲任务；
用户从终端发起的新回合也由相同通知更新任务状态和结果。`item/completed` 保存最终消息，
不要求 `turn/completed` 重复携带全部 items。迟到的 Bot 提交回执不得回退已经完成的回合
或覆盖更新的用户回合。主动读取/未知回执的一次核对仍可使用标准读取接口。

收起气泡、隐藏角色、关闭终端均不触发取消。输入/审批优先；原生气泡不抢键盘焦点，
展开与点击只触发既有 attention/nod 展示动作。明确打开失败使用短提示，并记录任务句柄、
错误分类与指纹到现有滚动诊断日志，不回灌为聊天消息。

`WorkTerminalProvider` 由 Codex 和 Caelis adapter 分别实现。Caelis 只开放 Bot 已拥有、
已确认原生 Session ID 的 Worker；旧应用会话、未知创建及其他应用任务不会生成 attach 命令。
Host instance 改变时先等待 Bot 重连。终端使用本机用户的 `runtime/service/auth.token` 文件，
Bot 仍使用受限应用凭据；脚本不会复制 token。双方分别订阅同一 Session，用户新回合和 steering
仍进入 Bot 原有 SSE 投影。主动关怀规则保留在独立 POC。

Caelis 的隔离 Host 验收使用 `CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis make smoke-caelis`，
覆盖原生 Worker 的终端目标、同一 Session、重连和结果。shell 参数执行测试验证不重发 prompt。
这些测试不代替原生任务气泡的实际点击与外部 Terminal GUI 验收。

安装版隔离验收（合成 loopback Responses，无个人凭据、真实模型或收费请求）：

```sh
source script/env.sh
CAELIS_BOT_TEST_CODEX="$(command -v codex)" go test -v -count=1 -timeout 90s ./internal/backend/codex -run '^TestNativeSharedWorkerTerminal$'
```

该测试走生产 Session、任务协调器与自建进程，覆盖 Bot 完成、另一个标准客户端发送用户回合、
该客户端断开后 Bot 继续、原生结果投影及退出清理。它不代替外部终端 GUI 与原生 hover 的人工验收。
