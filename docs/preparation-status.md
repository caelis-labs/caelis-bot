# 实现与验证状态

本项目已完成 macOS v0.1.0 正式发行。当前状态以产品代码、公共测试和
成品清单为准；完整历史制作记录保存在私有资产库。

2026-09-24 任务气泡与共享 Worker 终端（本地实现，尚未发布）：

- AppKit 原生收起胶囊/编号圆球、原始任务 prompt 两行预览、默认关联终端入口已接入真实账本；
  序号无任务分类含义。悬停和展开不请求 Runtime，输入与审批优先，不新增任务控制台。
- Codex 自建进程使用私有 Unix endpoint，已有标准 socket 继续复用；原生 TUI 与 Bot 可连接
  同一线程。所有已拥有任务保留通知订阅，包含用户从终端新发起的回合；取消长期 Worker 轮询。
- 安装版 Codex 0.156.1 + 本地合成 Responses 的生产 adapter 验收通过：Bot 创建和完成、
  第二客户端发送用户回合、断开观察端后 Bot 继续、结果投影、私有 socket 权限及进程退出清理。
  不是付费真实模型、任意工具或完整外部 Terminal GUI 的证明。
- `make check`、`make smoke`、`make build` 与相关 Go race 通过；契约覆盖原始 prompt 重载、
  所有权过滤、shell 参数转义、重复打开的文件复用、失败日志、早到终止事件和迟到 RPC 回执。
- 经 `script/build_and_run.sh --verify` 启动隔离数据的原生 App，载入三个真实协议的合成任务；
  原生 trace 确认胶囊可见、三个任务与拖动后的锚点恢复，桌宠本身已截图检查。
  当前原生自动化无法单独选择新浮动窗口或执行 hover，因此气泡视觉、两行截断、边缘/横向滚动、
  连续悬停和点击外部终端仍需人工实机复验。之前用户确认的外部 Terminal 属于 POC 证据。
- Caelis 的 `WorkTerminalProvider` 尚未接入，不显示这个入口；主动关怀规则仍保留在独立 POC。
  细节与复现命令见[任务委派](task-delegation.md)。

2026-09-24 设置、Dock 与安装界面整理（本地实现，尚未发布）：

- 七类设置逐页检查；连接页移除重复模型选择和重复检测入口，重启仅在连接更改或需要重新连接时显示。
  模型与权限统一保存，对话与工作设置分别保留成功回执；高级工作模型默认折叠。
  切换分类回到顶部，模型草稿跨分类保留，提供未保存提示和撤销；快捷键读屏名称包含当前组合。
- 聊天/设置打开时启用 Dock，后台与最小化仍属于打开；只有前台聊天被快捷键切换关闭。
  红色关闭键关闭当前页面；最后一个页面关闭后回到菜单栏驻留，不取消工作。
  Dock 唤回替换 Wails 默认的全部窗口显示逻辑，避免带出内部透明窗口；提升到普通应用时重设图标。
- 角色成品包 0.1.4 仅更新 App PNG 与 ICNS。DMG 改为 660×430、128 点图标、左到右拖入 Applications；
  固定版本 dmgbuild 写入布局，挂载验证签名、背景与位置，不给签名 App 写入 FinderInfo。
- `make check`、`make smoke`、`make build`、本地 DMG 签名/布局检查通过。
  原生启动后实测设置、模型草稿保留/撤销、试用入口召回、红色关闭按钮；Finder 安装窗口已截图检查。
  系统读取确认有窗口时为 Regular、关闭最后窗口后为 Accessory，并返回新图标。
- 物理全局快捷键、Dock 点击、VoiceOver 全程、跨显示器/Spaces 仍需人工复验；本轮未调用真实模型、
  未清理用户数据、未覆盖 Applications 安装。开发 DMG 为 ad-hoc 签名，未公证、未发布。

2026-09-24 后端事件兼容与本机环境修复（本地实现，尚未发布）：

- Codex 按通知方法及 item 类型解码，修复 MCP 启动通知的字符串 `error` 被误当作 Turn 错误对象；
  非阻塞组件故障、重试和未消费的通知不进入聊天，真正失败或无法确认的工作状态仍保留提示。
- Codex/Caelis 诊断写入私有 JSONL，保留原生目标、方法、错误分类与载荷指纹；
  每文件 2 MiB、最多 5 个文件，超过 7 天的文件在后续写入时清理，不记录对话、凭据或原始载荷。
- 原生启动恢复用户 login/interactive shell 的导出环境，供 Bot、worker、MCP 和工具子进程继承；
  保留原生配置、工作目录和权限边界。已运行的共享 Caelis Host 保持自身启动环境，见 [任务委派](task-delegation.md)。
- `make check`、`make smoke`、`make build` 与受影响包的 race 回归通过。
  本机 Finder 式最小 PATH 经恢复后可运行 npx 11.12.1；真实 Codex 0.156.1 的 Context7 启动为 ready，未提交模型请求。
- `build_and_run.sh --verify` 构建成功，但已安装正式版占用单实例；隔离测试进程写入环境恢复日志后退出。
  未终止日常应用，未覆盖已安装版本，本轮不宣称新构建的 GUI 或完整真实模型 worker 验收。

2026-09-24 自动更新与 R2（本地接入，尚未发布）：

- 正式构建接入 SHA-256 固定的 Sparkle 2.10.0；签名清单与下载包均验证，默认每天检查、用户确认安装。
- 安装重启前冻结用户、提醒和独立任务的准入；忙碌、审批与结果未知时延后，不为更新取消工作。
- Release CI 在 App/DMG 公证、票据和 Gatekeeper 全部通过后生成签名清单，再发布 GitHub、同步共享 R2 的 `caelis-bot/` 前缀。
  上传完整回读校验成功后切换清单、清理旧 Bot 版本；独立重试 workflow 不重新签名或公证。
- 本地 `make check`、`make smoke`、`make build` 和 actionlint 通过；真实固定框架的 AppKit fixture 已验证启动、开关、延后安装与取消；
  一次性测试密钥 + ad-hoc DMG 验证官方 appcast 生成、独立签名校验及篡改拒绝。
  `internal/app`、`internal/bot`、`internal/tasks`、`internal/desktop` 的 race 测试通过。
- 新设置页原生视觉验收未完成：`build_and_run.sh --verify` 遇到已安装正式版的单实例占用，本轮未退出用户正在运行的应用。
  不把旧版窗口或独立 SDK fixture 当成新 UI/真实应用替换验收。
- `caelis-labs` 组织已配置 `SPARKLE_PUBLIC_KEY` variable、`SPARKLE_PRIVATE_KEY` secret（仅 `caelis-bot`），
  以及三项 R2 secrets（仅 `caelis`、`caelis-bot`）；主仓库同名旧 secrets 已移除，避免覆盖组织配置。
  Sparkle 持久密钥保存在本机钥匙串 `caelis-bot` 账户，已验证公私钥配对、签名与组织公钥一致，临时导出已清理。
  新 R2 账户凭据仅允许 `caelis-releases` 对象读写，2027-08-24 到期；配置见 [release.md](release.md)。
  公开 R2 上传、包含新增 Sparkle helpers 的 Developer ID 公证、跨两版自动替换/重启与数据保留尚未实测。
  已发布 v0.1.0 仍须手动安装首个带更新器的正式版；本轮不变更公开发布状态。

2026-09-24 v0.1.0 正式发行完成：

- [正式版](https://github.com/caelis-labs/caelis-bot/releases/tag/v0.1.0)已公开，非 draft、非 prerelease；源码标签保持 `6d60b22779b9ffa8927f9a306219e5bccb699320`。
- [发行任务 35931144917](https://github.com/caelis-labs/caelis-bot/actions/runs/35931144917)的构建、签名打包和发布均成功。App 与 DMG 的 Apple 公证均为 Accepted，并已写入和验证票据。
- 从公开 release 重新下载 DMG，在本机验证 SHA-256、内外 Developer ID 签名、票据、Gatekeeper（Notarized Developer ID）、arm64、版本及内嵌源码 SHA 全部通过。
- 最终 DMG SHA-256：`33e8db682b4e8d5e9a1955c88470c4a9f22bdb86a8a78798f33aa9fa7eea1ff8`。
- CI 公证等待改为每次 60 分钟，打包作业 150 分钟。超时后重新查询；Accepted 继续，Invalid/Rejected 失败并输出日志，In Progress 保存 14 天恢复检查点、跳过发布。恢复核对来源、签名、字节哈希与 Apple 回执，并复用原提交 ID。
- 完整 `make check`、actionlint、原生 product CI 和公证状态/恢复回归检查通过。此发行验收不增加 Windows、Intel 或全量 macOS 版本的原生兼容性声明。

2026-09-24 v0.1.0 发行准备记录（已完成）：

- 恢复独立签名 stash；正式发行强制 Developer ID + Hardened Runtime + 安全时间戳，App 与 DMG 均签名、公证并附带票据，任一验证失败保留草稿。
- 构建不接触签名凭据，独立 `macos-release` 环境仅允许 main；临时钥匙串与凭据在退出/失败时清理。
- 本机 Developer ID 身份、团队、时间戳、Hardened Runtime 验证通过；隔离数据目录的签名 App 原生启动、设置与桌宠渲染通过。
- `make check`、`make smoke`、`make build`、原生签名拒绝测试与 actionlint 通过。真实签名验证发现并修复 inline requirement 缺少 `=` 的参数错误。
- CI 公证凭据已验证并配置；最终 Apple 公证、公开 DMG 下载及 Gatekeeper 证据见上述正式发行记录。

2026-09-24 工作模型解耦：

- 新任务优先手动指定，其次 Runtime 配置，仅未配置模型时回退 Bot；读取失败明确报错。
- 独立设置文件、原生创建回执/worker profile 保持模型，覆盖续接、重启和重复请求；不改变权限策略。
- `make check`、`make smoke`、`make build` 通过；相关后端 race 回归通过。
- 本机正式 Caelis v0.61.0 + 隔离合成模型验收通过：Bot 为 `gpt-5.4-mini/low`，新 worker 为 Runtime 的 `gpt-5.4/high`；其他 B01–B11 路径继续通过。
- 本机 Codex 0.156.1 原生 `config/read` 与 ephemeral `thread/start` 参数验证通过，未发送模型请求。
- 协议基线仍为 0.153.4；新增 ConfigRead schema 已固定哈希。当前二进制版本不同，`make schema` 的整版精确比对未通过版本门；单独生成 0.156.1 schema 确认消费的 model/effort/tier 字段定义一致。
- 原生设置 GUI 已验证 Runtime/手动切换、MiMo Pro 保存和刷新回读；Bot 保持 Luna/low/Fast，切回默认清空覆盖。
- 日常 Bot 已恢复，隔离 Host 已关闭，未更改日常 Runtime/模型设置。
- 本轮不新增真实模型端到端调用或 team 实现；team 公开入口记录为 [Caelis #74](https://github.com/caelis-labs/caelis/issues/74)。

2026-09-24 正式 Caelis v0.61.0 联调：

- 本机 `release` 二进制与官方 macOS ARM64 发布包一致；公开协议与候选版字节一致，固定提交推进至 `5e2546f`。
- 原生 GUI 验证自动发现、独立 Store、启用重启、身份写入和读回、Bot 工具回调、聊天及 Luna Fast 切换。
- 修复无参数产品工具的非法 `required:null`，并将完整产品目录加入真实模型验收，避免只测合成工具。
- 修复 Caelis 请求失败时聊天无提示；保留原生错误/失败原因，重连可恢复，新请求清除旧错误。
- 已恢复日常 Bot；日常 Runtime、认证、对话与 Notebook 未切换或覆盖。范围、证据和未完成项见 [正式版报告](caelis-release-acceptance.md)。

以下为历史候选验收记录：

2026-09-23 候选 Core `4a3c059` 真实模型验收（本地改动，未提交/发布）：

- MiMo Notebook、文件交付、审批重连、双 worker 取消/恢复、后台 grant、同 Turn 切换 Luna 与 Fast 通过。
- 修复空闲头省略活动 Turn target 时，Notebook 完成刷新和 worker 完成身份/结果过滤的问题，补回归及 race。
- 实际公开回执、摘要、日志和 B01–B12 范围见 [真实模型报告](caelis-live-acceptance.md)。
- make check、相关 race、smoke、ad-hoc App 构建通过；未启动日常 App，未升级日常安装或合并 Release PR。
- 用户后续明确授权复制 Codex 认证到独立验收 Store，日常认证文件未修改；未把凭据放进仓库或报告。
- GUI、发行安装、上传未知回执和 worker 制品主对话交付等限制仍保留，不据此宣称全部产品发行验收完成。

以下为更早的确定性联调记录：

2026-09-23 Caelis 通用应用联调（本地工作树，尚未提交/发布）：

- 固定 Core `3e4675a`、公开 schema/wire；保留既有改动，无私有 sibling import，无旧 Bot Mode fallback。
- 真实隔离 Host + 合成模型驱动 macOS 原生 Notebook 文件/命令、同 Turn 热配置/revision、旧版回调、
  两个 worker 的取消/重连、后台 grant/撤销、资源上传/发布/下载及 SHA-256；详见 [B01–B12](caelis-application-acceptance.md)。
- 修复工具结果 content union 导致下载项遗漏、同时到期提醒的 grant 合并，以及 worker 创建/提交
  丢响应后的阶段恢复；已确认的资源 payload 不再永久积累在绑定 journal 中。
- 完成相关 race、`make check`、`make smoke` 和 ad-hoc App 构建；未启动日常 Bot、未修改日常 Store。
- 本轮未调用真实模型或验证 GUI/Windows。Fast 成功路径、资源上传未知回执查询、worker 产物进入
  主对话报告等限制见报告；不要把以下旧 Bot Mode 的 MiMo 验收外推到新协议。
- Developer ID stash 保持独立，本轮没有恢复签名流水线。

以下按时间保留历史检查点；与最新报告冲突时以最新报告为准。

2026-09-23 Notebook 收敛（检查点 `bca03ce`，未发布）：

- 普通 Markdown Notebook：应用生成 INDEX、创建日期目录；MEMORY 是唯一核心记忆，正文不自动裁剪。
- 应用专属 `caelis-bot-memory` skill 只注入秘书，使用普通文件工具；worker 保持独立目录与指令。
  维护记忆作为内置核心能力，不主动向用户介绍 skill 或内部流程。
- 首次初始化必填名字、可选描述，保存为待发普通用户消息；接受后不保留第二份身份配置。
- 旧资料/笔记 UI、专用笔记 CRUD 已退出。旧格式与 Facts 资料复制为日期笔记，原件保留。
- Memory v0.6.1 线索能力保留，Bot 可 recall/remember/correct/forget；不自动摄取所有笔记。
- `make check`、`make smoke`、原生构建启动及相关 Go race 测试通过。
- Codex CLI 0.153.4 的隔离原生 App 验收：必填/可选表单、介绍以普通消息显示，真实模型写入
  MEMORY；后续对话更新长期偏好、创建日期笔记，完成后 INDEX 自动刷新。介绍前与接受后重启均通过，
  后者不再显示初始化表单。修复重复准备关闭目录句柄、从未提交的空会话恢复及回执持久化。
- 自动用例还覆盖未知结果不重发、明确拒绝后手动重试、旧数据不复活和 worker 隔离。
  完整边界与测试入口见 [个人空间](personal-memory.md)。
- 新 Caelis 通用协议仍待接入，旧联调与 fixture 不作为新协议真实模型验收。

2026-09-23 统一 Bot 产品层基础（检查点 `b449aad`，未发布）：

- `internal/tasks` 接管目录分配、任务容量、Runtime 归属、产品 intent/账本与有限完成报告；
  `WorkRuntime` 保留 adapter 的 native 绑定、源请求、执行审批和未知回执。Codex 原任务和报告回执
  可接续，旧 requestId 不重复创建；任务账本不复制规范会话 transcript。
- `internal/bot` 统一持有工具目录与 handler、秘书/worker 角色、稳定身份和提醒；
  MCP 连接与未来应用 callback 共用业务入口。提醒和 queued wake 显式绑定 Runtime，
  此检查点个人资料/Notebook/Memory 尚待下一片实现（见上方增量）。
- Caelis 并行任务使用 [重构 Prompt](caelis-core-rebuild-handoff.md)，允许移除旧 Bot Mode，
  不要求旧模式兼容或数据导入。桌面连接/检测/切换明确禁用旧路径；不创建旧 Bot、执行旧计划、
  回退另一 Runtime 或删除真实数据。旧 adapter 与 fixture 暂留作新 wire 替换参考。
- Developer ID 改动已独立保存到本地 stash `deferred: Developer ID signing pipeline (2026-09-23)`。
  当前构建继续使用 ad-hoc 签名，未创建证书、未发布、未应用签名流水线。

统一产品层检查点验证记录（2026-09-23）：

- `go test -race ./internal/tasks ./internal/app ./internal/backend/codex ./internal/bot ./internal/backend/caelis` 通过。
- `make check` 通过：前端构建及 34 项 JS/native 行为测试、Go vet/单测、共享核心检查与
  macOS/Windows 架构编译；不是 Windows 原生适配证明。旧 Caelis schema 校验仍只验证旧固定协议。
- `make smoke` 通过：本机 Codex 0.153.4 标准 stdio initialize，未创建会话、未发送模型请求；
  GLB 解析/动画检查通过，不是视觉验收。
- `make build` 通过，产出 `dist/Caelis Bot.app`，使用原有 ad-hoc 签名。保留既有非阻断
  Vite 大 chunk 和 macOS 重复链接库提示。本轮未重新启动日常实例。

可审计的 owning tests：

| 边界 | 文件与测试 |
| --- | --- |
| 同一产品策略不绑定具体 adapter | `internal/tasks/manager_test.go` / `TestSameProductPolicyForDifferentRuntimeAdapters`（codex / generic-fixture 两个模拟标识，非两套真实 Runtime） |
| 未知创建不再次派发 | 同文件 / `TestUnknownCreationPersistsWithoutRedispatch` |
| intent 持久化失败阻止效果、可恢复 | 同文件 / `TestFailedPersistenceCannotDispatchAndIsRetried` |
| 有限报告、终态读取/停止抑制重复 | 同文件 / `TestReportPersistsAndUnknownDeliveryNeverReplays`、`TestReadAndStopSuppressReportsButNewTurnReportsOnce` |
| 旧 Codex 记录接续、跨 Runtime 不接管 | 同文件 / `TestNativeReportImportAndLegacyRequestDoNotDuplicate`、`TestProviderSwitchPreservesLedgerWithoutAdoptingWork` |
| 目录不接管已有内容/符号链接 | 同文件 / `TestWorkspaceNeverAdoptsExistingOrSymlinkDirectory` |
| 共享身份、提醒归属、重启保留目标 | `internal/bot/runtime_test.go` / `TestIdentitySharedButSchedulesCannotCrossRuntime`、`TestQueuedWakeRetainsRuntimeAcrossRestart` |
| 工具取消/停止与工具目录快照 | 同文件 / `TestApplicationToolHandlerHonorsCancellationAndShutdown` |
| 禁止旧 Caelis fallback | `internal/backend/caelis/application_test.go` / `TestApplicationModeNeverFallsBackToLegacyBot` |
| Codex 精确审批、请求来源、原生回执 | `internal/backend/codex/tasks_test.go`，全部 owning tests 通过 |

边界：新 Caelis wire、通用 callback 的可信来源/租约、资源交付、两套真实 Runtime 的记忆联调、完整持久
Automation 仍待后续切片；未新增真实模型、原生多窗口、长期常驻或发行级验收。此基础不能
替代 A01–A12 或两套真实 Runtime 的发布闭环。

2026-09-23 本地内容包 v1（本地实现，尚未发布）：

- 早期发行范围为应用预置内容与第三方包导入。设置新增“外观”，支持自包含 GLB 角色、
  完整服装变体、静态 PNG 头像；头像可跟随角色或独立选择。版本并存、主动切换及移除，
  原选择不可用时使用内置形象；对话、Runtime 与权限独立。
- `internal/contentpack` 共享受限 ZIP/GLB/PNG 校验、不可变归档、原子安装、选择持久化和
  受限资源服务。未知能力、外部引用、主动内容、路径越界及超预算资源被拒绝。
- 离线 `content-pack init/pack/verify/unpack` 与 App 共用验证器；制作、导出、清单、
  二创授权、版本更新及 CI 操作见 [开发指南](content-packs.md)。
- `make check`、`make smoke`、`make build` 通过。测试覆盖归档边界、坏包修复、版本冲突、
  选择重载与过期失败隔离、持久化失败时内存回退、资源路由和缺省动作降级。
  创作者命令实际跑通火柴人和独立头像包，并验证解包再打包字节一致。
- macOS 隔离实例经 `build_and_run.sh --verify` 启动，实际完成文件选择、两版本导入、
  GLB 变体切换、跟随角色的聊天头像显示及使用中移除保护。测试实例已退出，未改日常配置。
  内容导入后应用签名仍通过校验；仅证明 ad-hoc 签名资源未被修改，不是 Developer ID 验收。
- 原生观察覆盖受控基础模型；持久化重载由 Go 测试验证，未完成任意社区作品的原生重启、
  长时资源压力、旧 macOS 或多屏验收。Windows 只完成共享核心交叉编译，未实现原生宿主。


2026-09-23 桌宠原生点击重做（本地检查点，尚未发布）：

- 单击、双击、拖动分别由 AppKit Click/Pan recognizer 仲裁，删除阻塞式鼠标跟踪循环。
  双击直接唤回窗口，不先打开输入框；单击等待系统双击判定结束。拖动仍交给 Window Server。
  隐藏、外部点击、切换应用或 Space 会取消未完成的点击，防止延迟动作重新打开窗口。
- 窗口唤回不再先恢复其他应用焦点，也不在 AppKit 事务内获取服务锁。读取原生窗口打开状态，
  避免 Wails 将完全遮挡误判为关闭而遗漏设置；已有设置保留当前页面并置前，关闭的设置不重开。
- `make check`、`make smoke`、原生构建启动通过。无界面原生测试覆盖识别器依赖、点击捕获、
  取消与拖动互斥；实际应用验证单击输入框、双击直接打开聊天及连续三轮关闭后双击恢复，
  trace 中没有双击前的单击动作。最终构建也已复测双击。
- 用户已确认被其他应用遮挡后反复双击可唤回聊天和设置，真实按住拖动正常；原生 trace
  也记录到实际窗口位移。最小化设置与多屏/Spaces 尚未完整复测。

2026-09-23 运行时切换入口与窗口唤回（本地检查点，尚未发布）：

- 管理标签与实际切换分开，未使用的运行时始终提供「切换至 …」；连接就绪后确认重启，
  不兼容时置灰，悬停/辅助功能说明原因。检查更新结果不会覆盖协议不兼容提示。
- 协议能力不满足与认证失败、暂时不可达分开分类，失败时不加载模型或允许切换。
  双击桌宠唤回聊天与已打开/最小化的设置，设置放在最前；关闭的设置不重新打开。
- `make check`、`make smoke` 与原生构建启动通过。原生设置确认当前 Codex 保留、Caelis
  0.60.1 置灰与帮助提示；检查更新返回已是最新版后仍保持禁用。关闭设置后双击桌宠仅打开
  聊天已验证。后台设置唤回随后通过用户复核，见上面的点击重做记录；最小化尚未复核。
  没有升级或切换日常运行时。

2026-09-23 原生聊天窗口关闭修复（本地检查点，尚未发布）：

- 删除聊天可见性的独立缓存，关闭始终隐藏窗口。AppKit 激活已显示窗口时同步 Wails
  的隐藏标记，避免原生关闭按钮被框架忽略；已过期的激活事件不能重新唤出窗口。
- 原生复测聊天红色关闭按钮、关闭后重开、设置在后台时关闭聊天、Cmd+W，以及设置关闭后
  桌宠保留。`make check`、`make smoke` 与原生构建启动通过；关闭不调用后台停止或退出。

2026-09-23 Runtime Setup 落地（本地检查点，尚未发布）：

- 首次引导与「运行时」设置复用真实的检测、安装、账号和模型配置；干净安装不先连接 Codex。
  Caelis 和 Codex 的管理视图、执行身份、各自路径和数据隔离；切换通过有工作检查的原生重启生效。
- Caelis 使用公开 Host 配置接口连接/选择/移除模型，候选来自 `/connect` 目录。
  Codex 使用官方 App Server OAuth/API Key/账号状态，保留登录取消和准确回执。
  Bot 不保存模型密钥，不打包 Runtime，不发送隐式计费测试。
- 统一 40 pt 控件、行内说明、1040×710 设置窗口；欢迎页与成品 App/Dock/菜单栏图标同步
  完整透明头像，成品包升级 `caelis-default@0.1.3`，GLB 与动态 SVG 未改变。
- 新增测试覆盖首次跳过不选 Runtime、路径/草稿隔离、重启准入保护、登录早到/错位回执、
  secret 错误脱敏、Host 配置权限及 unknown 不重发。真实外部 Host 在全新 store 中
  完成模型配置与候选查询，没有先创建 Bot，也没有发起推理。
- 原生窗口已检查欢迎、已有 Codex/账号自动识别、管理 Caelis 不切换当前 Codex、程序与 store
  选择、真实模型表单提交和失败后密钥清空。测试模型为本机合成配置，未改日常模型凭据。
- `make check`、`make smoke`、原生构建启动和 Setup/真实 Host 相关 race 通过。
  隔离 Bot 配置实际完成 Codex → Caelis → Codex 原生重启切换，恢复各自的路径与模型；
  没有向测试模型发起推理，未迁移或合并两个运行时的对话。
- 官方下载安装在本轮实现但未替用户实际重装/升级日常 Runtime；Codex 完整浏览器授权
  使用协议测试，已有真实登录通过读取验收。Caelis 账号 OAuth 新连接尚无完整公共回调路径，
  本轮不展示；Windows 原生安装/重启继续后置。

2026-09-23 Caelis Control Host 接入（检查点 `e6699a8`，未发布）：

- 新增独立 `internal/backend/caelis` adapter，消费固定公开 schema，无 sibling Go import。
  主聊天/审批、独立工作观察、来源对账、桌面动作、提醒授权与退出接入产品装配。
  Control 独占委派和自动汇报；Caelis 不加载 Codex MCP 或本地 prompt 提醒循环。
- 设置可选 Caelis、自定义二进制和数据目录；检测、安装、检查更新、更新和服务启动复用
  官方 CLI。连接先检查能力和旧绑定，保存后下次启动生效。Codex 记录保留原位置。
- 真实临时 Host + 可控模型 + race 已通过：两个模型创建的独立工作、原生审批、
  审批期间主 Bot 可用、一次报告/呈现 ack、同一工作继续执行、桌面 claim/receipt、
  grant/fire、Host 重启保留身份并重新激活、不重放动作、明确退出后共享 Host 仍存活。
- 协议/产品回归覆盖 unknown 不重发、旧审批、丢失 claim/receipt、原子替换、乱序 HTTP/SSE、
  凭据隔离、enrollment 后续读取失败、运行时切换及单一桌面执行 owner。
- `make check`、`make smoke`、原生构建启动和相关 race 已通过；共享核心 macOS/Windows
  双架构交叉编译通过，仍不代表 Windows native/ACL/worker 隔离可用。
- 实际 macOS 设置窗口已检查：Codex/Caelis 切换展示、自动/自定义路径检测、服务未就绪错误，
  检测失败后原 Codex 配置保持不变。未执行日常 Caelis 的安装/升级/服务替换。
- 真实 `xiaomi/mimo-v2.6-flash` + race 已通过：目录返回并选中模型、真实聊天、两个独立
  工作的精确一次性命令审批及完成报告、同一工作继续执行、桌面 clock/gesture、一次
  reminder grant 的准入与真实回复；明确退出后隔离 Host 仍存活。模型由用户通过
  Caelis `/connect` 配置，未读取或复制日常模型凭据。`make smoke-caelis-live` 为显式启用
  的付费模型验收入口，普通 CI 不运行；此结果不代表其他模型、图片或任意专业任务均已验证。
- 原生隔离 profile 已验证模型页加载、保存、聊天输入与 MiMo 精确回复，以及一个独立
  工作的审批详情、Allow once 和完成报告。发现并修复初始配置事件、内部报告上下文误入
  聊天的问题；依据原生事件来源过滤，正常用户正文与助手报告不受影响。旧展示缓存重建
  保留 Bot 身份、未知操作与桌面 journal；已在原生重启后确认历史恢复。验收结束后恢复
  日常 Bot profile，隔离 Caelis Host 仍运行。原生桌面动作渲染尚未在此模型验收中单独复测。
- 旧安装 0.60.1 不包含这轮能力；联调用固定提交的独立构建。恢复边界、最近 64 turns、
  未知配置/审批回执及手动重新授权等限制见 [Caelis 接入说明](caelis-integration.md)。

以下条目是各历史检查点的状态，不能覆盖上面的最新接入结果。

2026-09-22 公共后端与平台边界首片（本地工作区，尚未发布）：

- `internal/app` 统一产品装配、启动、状态观察与幂等退出；macOS 入口只提供原生能力，
  不再直接构造 Codex 或调度常驻 Bot。新增依赖方向守卫和生命周期行为测试。
- 后端能力改为具名接口，模型/审批选项和连接信息由 provider 提供；固定角色移至
  `botpolicy`，本地工具传输移至 `localipc`，Codex 审批配置仅在其 adapter 中生成。
- Codex 记录和任务目录保留原位置；新 provider 使用独立命名空间。未知 provider、
  不认识的审批策略与仅聊天的 adapter 不会静默回退。跨 provider 切换仍未开放。
- [能力契约](backend-contract.md)明确 Caelis Control 的任务归属、用户来源、独立工作区、
  幂等请求、原生审批、完成汇报与受限桌面工具要求；Caelis adapter 尚未实现。
  Windows 原生接口与发行落点见[实施计划](backend-platform-plan.md)，尚无 Windows IPC/宿主实现。
- `make check`、`make smoke`、`make build` 和 app/backend/bot/localipc 竞态测试通过。
  共享核心通过 macOS/Windows 的 amd64、arm64 交叉编译，不代表 Windows 原生验收。
- `make smoke-bot` 在 Codex 0.153.4 上通过旧绑定恢复、工具发现、提醒唤醒、两个独立目录的
  工作任务和一次继续执行，共三次请求收到接受回执。此联调使用隔离合成任务。
- `script/build_and_run.sh --verify` 已重新启动本机应用。原生设置页能加载 Codex 连接信息、
  模型、推理强度、速度和审批选项；关闭设置后桌宠保留。没有更改用户偏好；
  此次未重做所有桌面交互、多屏/Spaces 和旧系统视觉验收。

2026-09-22 秘书与专业任务分离（本地工作区，尚未发布）：

- 新增可选 `api.TaskProvider` 与五项 Bot MCP 任务工具，Codex 通过标准 App Server
  创建、读取、继续和中断自有任务；每项任务有独立受管工作目录。未接管 Codex App 的其他任务。
- 常驻 Bot 的固定职责改为协调和汇报，专业工作默认委派；任务运行时可继续接收用户消息。
  工作任务保留执行设置和原生审批，不携带秘书的 MCP 凭据。
- 请求和归属先持久化再发送；未知回执不重发。原生完成事件排队一次汇报激活，结果已读或
  用户停止后不重复唤醒。已有项目/worktree 授权、任意 App 任务导入和归档仍未实现。
- Codex 0.153.4 合成真实联调已通过：旧绑定恢复、工具发现、提醒唤醒、两个独立工作任务、
  两个不同工作目录和一次继续执行，共三个任务请求收到接受回执；不访问已有私人任务。
  首次联调发现 disabled MCP 仍要求有效 transport，已修复后重测通过。
- 契约测试覆盖任务归属、审批目标、重复/未知请求、主对话可用性、完成通知、继续/steer、
  工作目录防重定向和保存失败前不发送。真实联调是无外部操作的合成文本任务，
  不代表任意项目任务、外部发布权限或所有 provider 已验收。
- `make check`、`make smoke`、任务相关竞态测试和 `make build` 通过。
  `script/build_and_run.sh --verify` 已启动新版；原生聊天可打开并恢复已有消息，关闭后保留桌宠。

已实现：菜单栏常驻、透明可拖动/缩放角色、轻量输入与独立聊天、原生审批、附件、
Codex App Server 自动发现和协议握手、CLI 手动配置、历史恢复、常驻提醒、设置和诊断。
角色支持五段 clips、六套待机、近身回应、手指/眼嘴与视角修正、拖动跑步及独立飞机。

2026-09-22 仓库拆分：产品代码公开，当前 GLB/头像/图标按独立素材授权分发；
建模源、离线工具、旧版本和制作验收迁入 private `caelis-labs/caelis-bot-assets`。
成品按 `character-pack.json` 校验，主库构建不再访问 Blender 或制作目录。

2026-09-22 附件浮层修订（本地工作区，尚未发布）：

- 详情页“＋”菜单优先向上展开，与输入框等宽、左右对齐，不挤压聊天记录；长列表只在菜单内滚动。
- 快捷输入根据当前屏幕可用区域向上或向下展开。输入胶囊的位置、宽高与玻璃底保持独立；
  菜单展开时置于角色上方，关闭后恢复原层级，不改写角色显示偏好。
- 点击菜单外、按 Esc、选择引用或打开文件选择器均收起浮层；方向键可选择菜单项。
- 已通过 `make check`、`make smoke` 和原生构建启动。浏览器验证详情菜单等宽、向上、
  空白点击、Esc 和引用选择；原生验证 420pt 就近输入向上展开及 620pt 居中输入向下展开，
  展开/收起前后胶囊仍在同一屏幕位置、保持 64pt 高度，菜单层级高于角色；取消文件选择后恢复输入焦点。
  屏幕边界/负坐标/多行高度组合另由布局测试覆盖，不代表外接显示器硬件验收。

2026-09-22 交互与运行偏好修订（本地工作区，尚未发布）：

- 单击立即展开输入，双击仍打开完整对话。移除等待系统双击判定的计时器；轻量输入
  不再复制完整历史，文本窗口也不再加载角色的 Three.js 入口。
- 默认 Ctrl+Shift+Space 在鼠标所在屏幕中央唤出/收起；点击角色保持就近展开。
  设置支持自定义、停用、恢复默认和试用，检测冲突并在保存失败时回滚注册。
  快捷键表示保留跨平台能力，原生注册目前只实现 macOS。
- 聊天详情主内容不再限制最大宽度，跟随窗口伸展。
- 设置增加“模型与权限”：从当前 Codex 获取模型、推理强度及 Fast 档位，持久化
  用户选择和审批方式。保存时重新验证选项；工作中或有待确认事项时拒绝修改，
  保存成功后用于新请求，不改写正在运行的任务或待处理审批。

本轮验证：`make check`、`make smoke`、`make build`、`make schema` 和
`go test -race ./internal/backend/... ./internal/desktop` 通过。协议回归覆盖选项分页、
持久化失败、活动任务拒绝修改、新请求参数及关闭 Fast；schema 对照 Codex 0.153.4。
本机原生窗口已检查角色单击/双击、详情放大后的宽度、模型目录与 Fast 选项加载。
用户用真实快捷键确认可直接输入，重复收起不再闪窗口。

性能诊断显示旧单击路径固定等待本机的 500 ms 双击间隔；修订后一次角色点击到
输入就绪约 83 ms，数次快捷唤出为 57–89 ms（小样本、已启动应用，不代表冷启动或
跨设备保证）。2,000 条合成历史的基准中，完整快照约 10.18 ms / 7.74 MB，轻量快照
不分配历史副本。尚未以新保存的模型偏好发送真实生成请求；模型参数和权限映射由
传输夹具回归覆盖。此次未新增多屏/Spaces、Windows 或长期运行验收。

消息气泡随后精简为正文与横排操作按钮，去掉名称/状态标题，默认高度由 96 调整为
64 pt，并同步原生圆角及高度校验。检查、smoke 和原生构建启动通过；静态布局预览
确认处理中/完成两种摘要的按钮横排。此项尚未重做展开审批的原生视觉验收。

2026-09-22 UI 精简与原生材质（本地工作区，尚未发布）：

- 设置默认 960×680、最小 760×540；六个页面统一为左侧标签、右侧控件的分组行。
  常规页首屏包含快捷输入、角色大小和通知；详细说明按需展开，保留权限范围、
  错误、清理确认和失败重试。没有改动用户的连接或执行偏好。
- macOS 26+ 的输入胶囊、消息气泡采用 NSGlassEffectView Clear，设置侧栏采用 Regular；
  正文保持接近侧栏的明亮实色。旧系统回退到 NSVisualEffectView；减少透明度或
  增强对比度时使用实色。WebKit 保留原有响应链，材质为不接收输入的背景层。
- WebKit 完成导航后重新同步透明样式标记，覆盖首次加载及渲染进程重载；不依赖
  定时重试。材质诊断仅记录样式和颜色，不记录消息内容。
- 根据本机对比反馈，气泡进一步缩至 56 pt、28 pt 圆角，移除额外 NSWindow 阴影，
  横排操作改用一致的线条图标。

验证：`make check`、`make smoke`、原生构建与 `script/build_and_run.sh --verify` 通过。
本机 macOS 27 已逐页检查设置、快捷输入焦点与草稿清空/恢复；正文与侧栏底色调整后
重新检查常规页。发送一次不调用工具的文字请求获得“气泡显示正常”；用户截图确认
Clear 材质可以透出背景，并据此继续调整圆角、明度和阴影。原生诊断确认气泡/输入
使用 glass-clear，网页背景为透明。最后一版气泡的跨背景观感、深色模式、辅助功能
动态切换、最小窗口尺寸和旧系统回退仍未完成原生视觉验收；不将代码路径视为视觉证明。

2026-09-22 阅读优先的气泡收尾（取代上面的 Clear 试验配置）：

- 输入框与消息气泡统一使用无额外 native tint 的 Regular 原生材质，共用随明暗变化的
  半透明阅读底色。原生层负责模糊与折射，正文保持不透明；非激活气泡不为视觉效果抢焦点。
- 气泡正文 14 pt、20 pt 行高、中等字重；56 pt 胶囊外留 6 pt 透明阴影空间，原生窗口
  默认高 68 pt。只保留轻薄的渲染层阴影，横排按钮与展开审批的高度上限同步保留。
- `make check`、`make smoke` 通过；最后的共享底色修改后，原生构建和
  `script/build_and_run.sh --verify` 再次通过。已在本机观察输入框与非激活消息气泡的
  明度差异；共享阅读底色版本启动后，用户确认“观感已经好很多了”，本轮保留此版。
  这不替代复杂背景、深色模式、辅助功能切换和旧系统的完整原生验收。

2026-09-22 聊天运行态反馈（本地工作区，尚未发布）：

- 输入区移除重复的“正在处理 / 停止”行。聊天输入框右侧在空草稿运行中显示停止方块；
  有文字、附件或引用时显示箭头，是否可发送仍取决于后端能力和有效正文/附件。
  空输入框 Enter 不触发停止，停止请求待状态刷新后才解除按钮忙碌态。
- 等待时在消息流显示静态头像和三个动态圆点；流式正文出现后让位，完成/断线/待审批
  不显示无意义的 Loading。自动审查和停止中保留明确状态，减少动态效果时圆点静止。
- 根据用户新增调研要求，撤掉头像固定周期倾斜的试验，只交付评估，见
  [头像动效评估](avatar-motion-evaluation.md)。没有改动角色资产合同或私有制作工程。
- `make check`、`make smoke` 通过，新增键盘与状态回归纳入标准检查；实际前端的本地
  状态夹具验证了停止/发送切换、空 Enter、补充提交、正文取代圆点、审批和断线限制。
  此夹具不调用模型；不将它等同于新增的真实后端运行中操控验收。
  最终原生构建启动通过，已检查实际聊天窗口布局及关闭返回桌宠；未发送新的模型请求。

2026-09-22 简化 Bot SVG 头像（本地候选，取代上面的静态头像评估阶段）：

- 使用纯色椭圆眼睛、淡腮红和简化粉发饰品的 2D 成品，约 2 KiB。母版、参考图在私库，
  公开项目只接收合同 v2 的成品；不加载 3D 或新增动画依赖，旧 PNG 保留回退。
- 等待、自动审查、流式输出只运行一个活动头像。增加弹起、抬头、点头、侧身倾听、
  左右观察五组动作，整体位移/伸缩与独立眼神、眨眼组合；各组之间有停顿。
- 历史、结束、停止中的头像静止；隐藏、离屏、减少动态效果取消动画帧。待审批与断线
  保留角色头像，把状态及操作放入右侧气泡。审批气泡宽 480 pt（受窗口可用宽度限制），
  减少纵向留白，按钮横排靠右。
- `make check`、`make smoke` 与最终原生构建启动通过。新增测试覆盖 SVG 主动内容拒绝、
  有界动作及五组调度、暂停/恢复/卸载、唯一活动回复与审批/断线限制。浏览器夹具验证
  长对话的 22 个历史头像静止、唯一活动头像离屏复位，流式正文仍有动态头像，待审批/
  断线头像保留及内容位于气泡内；实际量得审批气泡为 480×155 pt。浅深背景的小尺寸
  SVG 已检查，原生 WebKit 已确认历史头像显示。未新增真实模型请求；原生运行态、
  辅助功能切换与长时电耗尚未作完整验收，不以夹具代替这些证明。
- 0.1.2 是本地候选包，未经跨仓库 CI 发布。私库已保存制作源、成品和后续发布步骤，
  现有 release/ 与锁定版本保持可用，待公共合同提交并可在远端获取后再更新私库 pin。

2026-09-23 聊天唤出与滚动修订（本地工作区，尚未发布）：

- 显式打开/唤回聊天会重新定位到最新消息；观察内容和可视区尺寸，在输入区挂载、
  多行草稿恢复和窗口缩放后保持贴底。手动翻阅与加载更早历史保留阅读位置，普通
  应用焦点切换不重新定位。
- 全局快捷键及设置中的“试用”切换聊天显示/隐藏；唤回时聚焦输入区，最小化时恢复，
  附着的原生文件选择框保持可达。菜单与桌宠双击仍只唤回。
  删除居中快捷输入路径，角色单击的就近输入胶囊保留，快捷键配置与冲突处理不变。
- `make check`（含 37 项前端/资源行为测试）、`make smoke`、`make build` 通过。
  初次沙箱检查被本机 Unix socket 限制阻断，本机重跑通过。
  `script/build_and_run.sh --verify` 启动新版，原生检查了历史向上翻阅后关闭/双击重开、
  末条长消息完整显示、五行草稿撑高与重开后恢复；测试草稿已清除，未发送新消息。
  自动化按键未能触发 Carbon 系统热键；用户已确认真实键盘可以唤出。
  后续显示/隐藏切换的服务回归通过，系统级连续按键切换仍需人工观察。

2026-09-23 透明窗口回归修复（本地工作区，尚未发布）：

- 用户提供桌宠与飞行纸飞机各自带黑色矩形背景的完整截图。根因是 Wails beta.23
  默认构建将 WKWebView 透明开关变为空操作；原生 NSPanel 与 Three.js 的 alpha 配置
  原本已正确。按上游合同启用 `private_mac_apis`，构建、vet/test 一致，不修改角色素材。
- `make check`、`make smoke` 与 `script/build_and_run.sh --verify` 通过；应用二进制的
  Go build info 确认 `production,private_mac_apis`。已启动原生版本并检查桌宠窗口，
  原黑色背景在窗口截图中消失；已通过既有开发预览触发纸飞机绕行。
  上游固定版本的 public/private 原生 WKWebView 桥接断言均通过，验证默认分支保持
  背景、private 分支关闭背景绘制。窗口截图工具会将透明区域显示为白色，未单独
  捕获飞行中的飞机窗口；用户在修复版运行时确认桌宠和纸飞机的黑框均已消失。

本地 `make check` / `make smoke` / `make build` 和公开 Actions 给出可复查的当前验证结果。
历史人工观察只覆盖 Apple Silicon 单屏核心流程；原生多屏/Spaces、旧系统、Intel、
长时运行、完整动作穿模与整体自然度仍需针对性验收。当前自动化未证明这些场景。

公开发行要求 Developer ID 签名、公证与 Gatekeeper 验证；历史预览包不补签或覆盖。
主分支要求 PR 与 product CI；版本 PR 经 release-please 管理，DMG 在精确 tag 上重新检查、
挂载核验和计算 SHA-256 后发布。自动化配置与恢复命令见[发布维护](release.md)。
本地内容包 v1 已提供设置内角色/完整服装变体与头像切换，详见 [内容包指南](content-packs.md)。
官方内置成品仍经独立资产更新 PR 交付；CI 不会自动批准或合并。
