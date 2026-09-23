# Caelis 应用运行时能力补齐：后续实施 Prompt

在 `/Users/xueyongzhi/WorkDir/caelis-labs/caelis` 继续实施。本任务只修改 Caelis；
Caelis Bot 在另一会话推进适配，你不是唯一在工作的 Agent，请保留其他人的修改。
不要重新做旧 Bot Mode 重构，也不要恢复已经删除的 Bot CLI、专用工具、调度或产品存储。

## 用户决定与优先级

本轮以产品功能可用为第一优先级，KV Cache 优化和额外安全加固服从这一目标。
本 Prompt 更新并覆盖上一轮 handoff 中与以下决定冲突的实现约束和验收标准：

1. 应用会话的模型、推理强度、Fast/service tier、角色指令和工具目录必须支持动态更新。
   **不能以保护 KV Cache 为由，将配置固定到整个 Session 生命周期，或要求用户新建对话。**
2. 新配置在下一次尚未发出的模型请求生效，包括同一 Turn 的后续工具回合。
   已发出/正在流式返回的请求不追改；这是自然的执行边界，不是等待整轮任务结束的借口。
3. Bot 优先使用 Caelis 已有的原生文件、命令等工具。不要让 Bot 为弥补 Runtime 缺口，
   再实现一套文件工具、文件编辑器或命令执行器。
4. 采用用户主动安装、主动连接的本机个人助手信任模型。
   **同用户进程级凭据完全隔离不是本轮原生工具可用的前置门槛。**
   不因单项进程凭据隔离探针失败而整体禁用 macOS 应用原生执行。
   复用普通 Caelis 的工具、权限选择和审批机制；如有更强隔离模式，可作为额外能力后续完善。
5. 保留应用鉴权和归属、操作幂等、调用来源、用户选择的权限与审批，以及凭据不进入日志等基本边界。
   不通过静默放宽用户权限或把所有执行改成无条件批准来完成任务；也不另建一套更苛刻的 Bot 沙箱。
6. Caelis 是通用能力框架；Bot 的身份、Notebook、Memory、任务组织、提醒调度、角色动作归应用所有。
   不在 Core 中重新添加 Bot 产品逻辑。

先检查当前基线和工作树。已有实现能复用就扩展，不另建执行器、第二份历史或平行协议。
本任务不授权 push、tag、发布、升级用户日常 Runtime，或删除真实用户数据和凭据。

## 必读材料与基线

上一轮交付基线是 `96b6234685d08c0cab7cb71dca43c2054787936c`，但实施前检查实际 HEAD，
不要将并行的新改动回退到该 SHA。上一轮报告：

- `/tmp/caelis-96b62346.wlX3zZ/caelis-bot-integration-report.md`

Caelis 侧：

- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/AGENTS.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/docs/architecture.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/docs/testing.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/docs/application-runtime.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/api/control/v1/openapi.json`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/control/application`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/control/appserver/httpclient/application.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis/app/gatewayapp/application_host_http_test.go`

Bot 侧只读契约：

- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/docs/runtime-extension-contract.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/docs/bot-platform-architecture.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/docs/caelis-core-rebuild-handoff.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/contract.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/work.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/application_tools.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/backend/api/capabilities.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot/internal/botskills/skills/caelis-bot-memory/SKILL.md`

Bot 已提交的 Notebook 检查点是 `bca03ce`。其工作树中另有未完成、未编译通过的协议适配草稿，
不是已验收契约；不要依赖这些草稿的内部实现或替它们修改文件。
双方通过公开 schema/client 和固定构建产物联调，不通过私有 sibling Go import。

## 1. 动态配置：本轮核心改造

在现有应用会话上增加版本化更新、读取与恢复能力。具体 wire 名称由你设计，但必须表达：

- 稳定 operation ID、预期配置 revision、更新字段与变更后的 revision。
- 字段未提供表示保留；显式空值/空目录表示清空或恢复默认的规则须明确。
- 模型、effort、Fast/service tier、instructions、原生工具选择、应用工具目录及版本可更新。
  模型支持矩阵和不可用组合返回明确错误，不能静默忽略用户选择。
- 配置与执行权限正交。切换模型、修改角色或工具 schema 不应重置用户已选审批策略。
- 持久化的期望配置，以及当前模型请求实际使用的配置 revision/生效状态。
  “请求已接受”“配置已保存”“新配置已用于执行”必须可区分。

生效语义：

1. 空闲时接受更新后，下次请求使用新配置。
2. 忙碌时仍接受并持久化更新；当前已发出的请求用旧快照完成，下一次模型请求用新快照。
   不能排队等整个长 Turn 结束；不能仅更新 UI 而实际沿用旧配置。
3. 更新与模型请求构建并发时有明确先后关系；每个请求只使用一份完整配置，不能混合 revision。
   给出实际请求开始和更新提交的边界定义，并用确定性测试证明。
4. 不自动取消当前工作。若用户需要立即停止当前请求，复用显式 cancel 后继续的路径。
5. 同值更新不产生无意义的新配置 revision 或 cache 失效；并发更新通过 revision 冲突避免互相覆盖。
6. 断线或 Host 重启后能查询更新结果、恢复期望配置，不重复提交用户消息，不丢掉旧历史。
7. 不把“新建 Session”作为唯一实现。确需更换内部 provider client/context 时，对上层保持逻辑
   对话身份、历史和可观察的配置生效语义；不让用户重新初始化 Bot。

KV Cache 策略：保持未变化的指令内容、工具序列和历史序列化稳定，只在真实配置变更时重建
必要上下文。不能把版本号、时间戳等无关字段不断塞入模型前缀。用户要求更新时，允许因此
失去部分缓存；不要为了缓存拒绝更新、偷偷延迟，或强制 compact/清空历史。
模型/供应商切换若需消息格式转换，转换执行投影，不改写 canonical history。
用模型请求内容证明稳定性，不把“请求前缀相同”夸大为供应商实际 KV Cache 命中证据。

在途工具：已经由旧请求产生的调用绑定当时的工具版本和可信调用身份；配置更新不能使回执
失去路由、重复效果或被错误匹配到新版工具。下一次模型请求只获得新版目录。目录更新不是
撤销在途调用；显式取消/撤权走其独立语义。若旧宿主已不可用，明确失败/未知，不调用同名新版替代。

## 2. 原生工具与 Bot 工作目录

完成 macOS 下应用会话的原生工具执行，复用现有 Runtime 工具和权限机制。
将当前因进程凭据探针失败而整体返回 unsupported 的策略，改成明确、可用的本机执行模式。
如保留严格隔离模式，公开它实际支持的平台和限制，不能把它当成唯一模式。

应用需要显式指定常驻工作目录，以及按需提供其他访问目录。路径来自已鉴权应用配置，
不是模型参数自行授予的权限。支持两种产品用途：

- 常驻助手使用 Bot 创建的 Notebook 目录，直接读写普通 Markdown。
- 独立任务使用应用分配的工作区，或用户明确选择的已有项目目录。

工作目录和配置继承分开控制。使用 Notebook 不应自动加载全局 AGENTS、MCP、skills 或普通
Workspace Memory。应用可以显式注入自身 skill 指令或声明只读 skill 文件；不要把技能
安装到用户全局目录。已有项目需要继承哪些上下文，由明确配置决定。

Bot Notebook 的当前形态仅供联调理解，不得实现为 Core 的专用知识：

- `MEMORY.md`：唯一核心身份与长期认知文件，Bot 通过原生文件工具维护。
- `INDEX.md`：Bot 应用生成目录索引；Runtime 不负责生成。
- `YYYY/MM/DD/*.md`：普通日记式笔记。
- `caelis-bot-memory`：Bot 独有技能，普通会话与 worker 不自动继承。

无需新增 Notebook CRUD API，也不要求同步另一份身份数据库。工作目录/访问范围变更若与
在途原生效果冲突，可以明确要求空闲后应用；不得因此连模型、指令、工具目录更新也全部拒绝。

## 3. 独立任务与后台激活

补齐通用应用会话 create/read/list/resume/prompt/cancel/history/archive 的实际执行链路，
让 Bot 能同时管理常驻助手和独立 worker。Core 提供执行事实；Bot 持有任务标题、任务容量、
工作区分配、状态汇总和结果汇报。常驻配置更新不静默改写正在运行的 worker 配置。
以至少两个并行 worker 完成、其中一个取消、结果返回应用为验收，不只检查方法存在。

提供最小可用的应用后台激活来源/授权表达。用户已在 Bot 创建定时任务后，Bot 应能在授权
范围内触发同一助手或独立任务；不需要每次重问，也不伪装为新的人工输入。
复用现有应用 scope 和原生权限，避免为首版建立复杂的新授权服务。
明确授权来源、作用范围、触发 operation ID、撤销和重复触发的行为即可。

Core 不解析 cron、不持有提醒列表、不运行 Bot 的定时循环。不要把 application_summary
或 external_material 自动提升为用户授权；合法定时触发应有独立来源，并在历史/replay 中保留。
用户已请求的常规工作委派能够正常执行，不因必须说出“创建新会话”而被自动审批拒绝。

## 4. 文件闭环与可发现能力

复用已有资源上传/读取、原生 ReadResource/PublishArtifact，跑通：用户输入文件实际字节
进入工作区 → 原生工具处理 → 发布产物 → 应用通过公开接口下载与校验。
Bot 不读取 Core SQLite、不猜工作目录、不靠模型生成的路径取得文件授权。
首版资源上限可以保留，但要公开限制和清晰错误；不要求本轮建立复杂的过期/计费/分发系统。

initialize 或公开能力查询应使应用可区分：原生执行、工作目录绑定、配置热更新、
受授权后台触发和资源传输的实际支持。仅有 application-runtime-v1 不足以让设置页判断
能否切换到一个完整可用的 Runtime。提供兼容协商，避免 Bot 通过破坏性试创建来猜能力。

## 可 Review 的验收标准

每项映射到具体测试名、源文件和命令。使用实际 Host/公开 HTTP client 与可控 provider；
配置并发和故障测试用可控制的屏障/注入，不靠 sleep 推断执行先后。

| 编号 | 必须证明的行为 |
| --- | --- |
| B01 | macOS 应用会话使用原生文件与命令工具完成真实操作；不是仅工具注册成功，也不是 tools-only 替代品。 |
| B02 | 常驻会话在应用指定的 Notebook 读写 MEMORY.md 和日记；新上下文能重新读到；普通会话/worker 不自动获得 Bot skill。 |
| B03 | 同一逻辑会话更新模型、effort、tier、instructions、工具目录；实际下一次模型请求使用新值，历史保留且无重复用户消息。 |
| B04 | 阻塞一次在途模型请求后更新配置：当前请求保持旧快照，同一 Turn 的下一次请求使用新快照；不必等待 Turn 结束。 |
| B05 | 配置更新、模型调用、工具回调的并发边界无混版；旧回调完成一次，新目录只用于新请求；同名工具不能错接。 |
| B06 | 同值更新不扰动有效配置；真实变更如需重建缓存允许重建；普通 Notebook 写入不触发配置更新/强制 compact。 |
| B07 | 更新响应丢失、重连及 Host 重启可按原 operation ID 核对；revision 冲突、显式清空、不支持组合均有明确结果。 |
| B08 | 常驻助手加两个并行独立 worker，完成/取消/恢复与结果读取可用；根配置不静默污染 worker。 |
| B09 | 合法后台触发成功；同一触发不重复执行；撤销后拒绝新激活；历史来源与人工输入、摘要可区分。 |
| B10 | 输入文件 → 原生读取/修改 → 产物发布 → 应用下载的字节与摘要闭环；跨应用资源访问按已有 scope 拒绝。 |
| B11 | UI detach 不取消任务、不关闭共享 Host；显式取消仍对准正确任务；未知副作用不靠重试新 ID 掩盖。 |
| B12 | 普通 CLI/TUI、模型登录/配置、MCP、ACP、Workspace Memory、审批及 replay 相关回归通过；旧 Bot Mode 不恢复。 |

真实模型验收可使用已由用户配置的隔离环境；先核对隔离目录和可用模型，不复制日常凭据，
不输出秘密。没有可用配置时先完成可控 provider 的全链路证据，明确报告真实模型未验收。
Windows 本轮保留清晰适配边界，不要求把 macOS 交付等待 Windows；不能将交叉编译称为原生验收。
按照仓库要求执行相关测试、`make commit-check`、`git diff --check`，不要跳过失败来宣布通过。

## 交付与协作

按可独立审阅的功能切片推进：动态配置、原生工具/目录、后台触发与多任务、资源闭环/回归。
尽早提交给 Bot 侧一份实际 wire 草案，尤其配置 revision、生效时点、工具版本路由、目录和
后台来源字段；不要等所有工作结束后才宣布接口。

最终提供：

1. 更新后的 application-runtime.md、OpenAPI、Go/TypeScript 类型、公开 client 和请求示例。
2. 新能力标识，以及相对 96b62346 的兼容/迁移说明。不得默默丢弃已有应用会话或用户数据。
3. B01–B12 证据表，区分单测、真实 Host、可控 provider、真实模型和原生平台验证。
4. 当前准确 SHA 与 dirty 状态、构建命令、独立二进制绝对路径与 SHA-256。
5. 无需 Bot 私有导入即可运行的隔离联调入口；所有剩余缺口明确列出。

产品能力若仍被阻断，应明确报告阻断及可行实现，不再用“严格隔离模式不支持，所以整个
应用原生执行不可用”作为本轮完成状态。
