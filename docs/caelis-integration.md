# Caelis 接入与运行时管理

当前必需能力包括 `execution-configuration-v1` 和 `application-guardian-review-v1`，
公开协议固定于已合并 Core PR #91（`e8281aa7bb89`），包含 #85 环境修复、#89 Guardian 与 #91 审批身份修复。
公开 schema/wire 同步固定到该提交；按能力判断兼容性，不以发行版本号代替握手。
`application-terminal-observation-v1` 是可选增强：支持时启用迟到审批命令的结果跟进，
不支持时正常连接，且不访问该观察接口。审批投影、指定工作区和自动 pin 均不依赖它。
Caelis 可独立交付该修复，无需为此单独发布版本。通用应用运行时与共享原生 Worker 的
公开 wire schema 的精确源码提交与哈希见 `protocol/caelis/manifest.json`。使用公开 HTTP/SSE 与生成的 Go wire，
不导入兄弟仓库、不恢复旧 Bot Mode。基线包含共享会话、steering、Host 设置授权流与 Team 角色候选。

验证命令和证据范围见[开发与验证](development.md)。历史 v0.61.0 的真实模型结果不代替新增能力验收。

macOS 13.5+ 安装包为常驻 Bot 装配 Cua 桌面工具时，还要求 Host 声明
`application-tool-result-content-v1`（Caelis #82）。缺少时在注册工具前提示更新并重启 Host，
不能仅凭 v0.62.0 版本号认定桌面集成可用。未装配此能力的纯文本会话仍遵循原基线。
Codex 使用其原生 Computer Use，不经过这条 Cua callback 路径。

## 基线与发现

`protocol/caelis/manifest.json` 固定公开 OpenAPI、wire 哈希和源码提交。
运行时通过 `/initialize` 协商 protocol 1、API v1、`caelis.control.envelope/v1`，以及：

- `application-runtime-v1`
- `application-hot-configuration-v1`
- `application-native-execution-v1`
- `application-workspace-binding-v1`
- `application-background-activation-v1`
- `application-resource-transfer-v1`
- `shared-native-workers-v1`
- `turn-steering-receipts-v1`
- `execution-configuration-v1`
- `application-guardian-review-v1`

CLI 版本号仅供显示，不是兼容性 allowlist。缺少能力时阻止切换，保留安装、更新和服务管理入口；
不静默换用 Codex 或旧 Bot。v0.61.0 不包含共享 Worker 扩展；安装成功也不代表运行中的共享 Host 已更新。

`application-model-capabilities-v1` 是 Ask Bot 的可选增强：应用凭据读取
`GET /application/sessions/{session_id}/model-capabilities`，返回 `session_id`、
十进制字符串 `configuration_revision`、desired `model` 和可选 `image_input`。
仅 true 启用；false、缺失、断线、无协商均禁用 Ask Bot，复制/标注/贴图不受影响。
发送前再次读取当前配置，模型切换后恢复，无需更换 Bot 身份。该读取不激活模型，
也不是派发授权；服务端正常执行校验继续生效，旧请求始终按原 ID 核对。

## Guardian 装配与工具审批

新建 Bot Runtime 默认显式装配 Guardian，低风险记忆与读取直通；派生 Worker、持续安排修改
由自动审查处理。Computer Use 按一个 App、一个连续任务 Turn 授权一次。
版本升级后的恢复启动主动交接到新配置，普通 Dream 也可交接；同版本重启恢复原绑定。
不热改当前或未决 Runtime；保留历史、Notebook 与原工作状态。完整策略和发布条件见下文。

## 执行环境装配

Bot 的 workspace-write profile 显式选择继承 Host 环境、非登录 shell；Notebook 只指定 CWD。
启动 shell 恢复由原生 Bot 宿主做一次，Core 按通用配置执行，不再次私有化 HOME 或重置 PATH。
不把整张客户端环境和凭据写入持久 profile，也不为连接中的共享 Host 偷改环境或重启进程。
旧 Host 缺少能力时保留已有数据并报告不兼容，用户经原有更新/启用服务路径处理。

环境配置在创建时固定。恢复/未知创建沿用已有 profile 和原请求；已有无配置的 Session
使用新版 Core 默认值，无需更换 Bot 对话。工具重绑定不热修改环境，同版本 Dream 交接保留原配置；App 升级交接按当前产品版本重新装配。
原生 Worker 使用 Core 的普通 Session 默认环境，保持自身工作目录及模型配置。
继承 HOME、PATH、CLI 配置不扩大 sandbox 的写入范围，审批仍使用原目标与一次性选择。

## 使用入口

1. 「设置 → 运行时与模型 → 管理」维护当前 Runtime；“更换”选择其他 Runtime，查看不切换当前后端。
2. 自动发现本机安装，也可选择已有二进制和独立数据目录。安装、更新、服务启动只在明确点击后
   调用 Caelis 官方安装或 `update`、`service` 能力；不捆绑 Runtime。共享服务升级流程见下节。
3. 「连接 → 添加连接」提供账号授权、API Key、内置或自定义 ACP Agent。安装和认证依照原生目录，
   凭据只写入 Caelis；Bot 不持久化密钥或授权码。
4. 能力与模型就绪后，点击「切换至 Caelis」，保存后重启。进行中的工作、审批或未知结果会阻止切换。
   对话、计划和原生执行记录按 Runtime 隔离；产品身份、Notebook 与 Memory 仍共享。

开发联调使用独立 `CAELIS_BOT_DATA_DIR`，其中的 `runtime.json` 可配置：

```json
{
  "version": 1,
  "runtime": "caelis",
  "cliPath": "/absolute/path/to/caelis",
  "caelisStore": "/absolute/path/to/isolated-store"
}
```

不要通过更换 Store 或删除绑定来绕过未确认的操作。原生启动仍使用
`script/build_and_run.sh`；协议夹具使用临时 HOME/Store，GUI 联调临时选择独立 Bot 数据目录，结束后恢复日常 Bot。

## 安装、更新与启用服务

程序版本和运行中的服务版本分别显示。检查更新只读取版本；“更新并重新连接”执行
`caelis update`，随后通过 `caelis service start --format json` 启用已安装版本。
原生生命周期负责服务选择、锁、就绪验证及启动失败回退；Bot 不按 PID 强杀 Host。
安装成功后仍需 `/initialize` 验证 Bot 所需协议，并重新连接当前 Store 的 Bot adapter，
全部成功才显示服务就绪。仅升级同一 Runtime 不要求重启 Bot。

若程序已更新、服务仍旧，直接选择“启用已安装版本”，不重复下载安装。
失败后重新读取安装与服务状态，保留明确错误，允许检测后重试启用。原始安装日志不进入设置页面。
换 Runtime、程序路径或数据目录仍需显式切换并重启 Bot。

升级确认提示其他 Caelis 客户端将短暂断开。执行期间暂停 Bot 的新对话、提醒及任务准入；
本机 Host 状态显示活动工作，或状态无法确认时，拒绝替换服务。下载后再次检查，
其间到达的工作只延后启用，已安装程序保留。公开协议尚无跨客户端原子的“空闲时重启”，
检查与替换间其他客户端仍可能发起工作；应先结束其他终端/应用中的工作，升级期间不要再提交任务。
普通连接、检测、关闭终端或退出 Bot 不触发共享 Host 替换。

可使用两个正式二进制复现隔离升级，不读取日常模型账户：

```sh
source script/env.sh
CAELIS_BOT_TEST_PREVIOUS_BINARY=/absolute/path/to/caelis-previous \
CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis-current \
go test -race -v -count=1 -timeout 180s ./internal/app -run '^TestCaelisReleasedServiceUpgrade$'
```

## 共享模型与 Team 设置

主模型调用 `/configuration/use-model`，Team 使用既有 `/agents/binding-status`、角色绑定、
自定义角色和 binding-set API。所有共享写入都携带界面读取的 revision 和独立 operation ID。
配置冲突要求读取最新状态；结果未知不自动重发。Team 使用 profile ID 和原生明确 effort，
主模型使用公开 selector。角色可选模型来自 `eligible_profile_ids`，不在 Bot 复制能力规则。

账号连接协商 `model-auth-stream-v1`：同一 `/configuration/connect-model` 命令使用 SSE 返回
浏览器链接、设备代码和一次性 challenge；授权码经对应 `auth-input` 路径回填。
SSE 不保存或广播授权历史；流断开后核对原命令结果，不重新发起登录。
缺少该能力时账号入口显示更新提示，API Key 和已有配置管理仍使用公开接口。

ACP 使用原生 launcher 目录、安装计划、preparation ref/digest、认证方式和声明的模型。
选择其他模型创建 preparation 子版本，不重复安装。交互式终端认证仍在 TUI 完成。
`self` 只读；无候选的角色不可绑定。断开外部 Agent 明确移除整个 Agent 的模型连接。

## 所有权与公开接口

| 产品能力 | 应用责任与 Runtime 映射 |
| --- | --- |
| 持续身份、Notebook、Memory | Bot 持有 Markdown、索引、skill 和 recall/remember；常驻应用会话的 CWD 显式绑定 Notebook |
| 原生文件与命令 | Caelis `workspace-write` 的 Read/Write/Patch/Glob/Grep/RunCommand；不增加 Bot 专用文件工具 |
| 聊天与观察 | `/application/sessions`、`/{id}/prompt`；canonical Session State、reconnect/SSE；报告使用 `application_summary` |
| 专业任务 | `internal/tasks` 分配目录、账本和有限汇报；adapter 将 WorkRuntime 映射为共享原生 Worker，不继承秘书 skill/Notebook 指令 |
| 应用工具 | Bot 注入目录及 handler；Caelis 返回可信 callback；按 opaque call ID、native item、配置 revision、tools_version 路由 |
| 后台提醒 | Bot 保存计划并计时；用户创建/修改时获取 background grant，激活使用 `authorized_background`；一条计划对应一次激活 |
| 中断与审批 | canonical 原生 target 与完整选择；提交审批前重新核对当前 head，不用 prose 推断授权 |
| 上传 | 图片走原生 image part；其他文件上传资源后提供 resource ID，模型通过 ReadResource 使用真实文件；单文件 8 MiB，最多四个 |
| 下载 | PublishArtifact 原生结果投影为 opaque 下载项；公开 content 接口校验 ID、归属、size、SHA-256，再写应用下载缓存 |
| 关闭 | 关闭 UI 不影响执行；adapter Close 仅停止自身观察和回调处理，不撤销连接、不取消 worker、不关闭共享 Host |

Bot 的应用工具经过同一业务层：Codex 使用私有 MCP，Caelis 使用公开 callback；不复制一套 Bot 产品实现到 Core。
本地凭据使用既有私有文件权限，不把“同用户进程无法读取凭据”作为 macOS 原生工具可用的门槛。
这不是强进程隔离；实际文件与命令权限由普通 Caelis 执行策略决定。

## 实时配置与恢复

`GET/POST /application/sessions/{id}/configuration` 使用稳定 operation ID、decimal-string
`expected_configuration_revision` 和局部 patch。模型、effort、service tier、指令、工具目录可在忙碌时保存，
同一 Turn 的下一个尚未发出的请求采用新版本；已发出的请求及其回调保持原版本。
`revision` 是期望配置，`last_request.revision` 是已实际用于模型请求的版本。

同值更新不递增 revision；Notebook 普通写入不更新配置、不强制 compact。
字段缺省保留；instructions/effort/tier 的 `""` 与 tools/native_tools 的 `[]` 按公开契约清空。
adapter 用 map 表达显式空数组，避免生成类型的 `omitempty` 把清空变成未提供。
更新丢响应通过 `/application/configuration-operations/{operation_id}` 取精确历史回执，再读取当前期望配置；
不能用旧回执覆盖后来的配置。目录、执行/继承/权限仍在创建时确定，默认产品装配 `workspace-write` + Guardian auto-review。

原生记录保存在 `providers/caelis/application.json` 和独立的 `application-credential.json`。
旧 Bot Mode 的 `binding.json` 等文件原样保留，不迁移到新协议。注册前持久化应用凭据与操作 ID，
此后执行请求使用应用 credential，Host credential 仅用于 enrollment、发现与显式模型配置。

所有 mutation 先持久化 intent；未知 prompt/create 查询原操作，不改 ID 重发。
worker 启动分阶段保存创建配置、原授权和 prompt，重启后可在确认前一步回执后继续；
已经发出的步骤只读回执。callback claim 丢响应不执行 effect；effect 已记账只重送相同 result；
缺失旧 handler 时明确失败，不把旧调用交给同名新版工具。
SSE replacement 完成后原子切换，游标保持不透明；稳定订阅期间进展不依赖状态轮询。

## Shared native Workers and steering

The adapter requires `shared-native-workers-v1` and `turn-steering-receipts-v1`.
New workers use `POST /application/workers`, then the ordinary Session prompt,
steer and reconnect APIs. They use the Host's normal environment, tools, MCP,
plugins and permissions; resident Bot profiles and private Memory are not copied.
Explicit work model preferences select a per-Session native model; an empty
preference uses the Host default. The terminal entry point is
`caelis attach --control-url ENDPOINT --session SESSION_ID --store-dir STORE --control-token-file PATH`.
Bot task bubbles resolve only owned native Workers and open this command in the
user's external terminal. Closing it detaches observation without stopping work.
No new Session or prompt is created; credential bytes never enter the command.
The host credential belongs to the user's terminal, not to the Bot adapter.

Main and Worker submissions during an active Turn preserve its exact target in
the outbound operation journal. A repeated ID never adopts a later Turn or turns
into a fresh prompt. Accepted steering remains distinct from a canonical
`input_status: applied` event. Session SSE stays open across user-initiated Turns;
steady progress requires no state polling. Maintenance renews the application
lease and restores failed streams or uncertain receipts.

Persisted pre-native workers retain their isolated Application Session binding
and original pending creation profile. Only those existing records use the old
creation/prompt path; new workers never select it. Remove this reader when no
supported saved Bot state contains these workers. Unknown native operations are
reconciled through `/application/operations/{operation_id}` without redispatch.


## Codex compatibility


用户本机 CLI 不按发行版本设白名单、最小值或最大值。较旧、较新、预发布版本只要满足
所需协议就可接入。自动发现、手动路径、共享 Unix socket 使用一致的协议判断。
`toolchain.json` / `TestedVersion` 的 0.153.4 仅为仓库 schema 与回归证据的可复现基线。

当前官方 App Server 并未在 initialize 中协商独立的 protocolVersion，也没有返回完整
服务端能力列表。clientInfo.version 是客户端版本；schema 的 v1/v2 目录也不能作为
已协商的协议版本。不能用 CLI 版本号替代它们，或虚构支持的数字协议范围。

实际兼容切面：

- 连接通过标准 initialize → initialized，保留 experimentalApi 客户端能力声明。
  检查 userAgent 的必要形状，不要求未使用的 codexHome/platform 元数据；新增字段忽略。
- 配置检测额外只读 account/read，校验所需认证字段后才允许保存。检测不发起模型请求；
  账户记录不代表 token 有效性或模型调用权限。握手通过不等于全部扩展均通过验收。
- 真正连接 Bot 时继续通过 thread/start 或 thread/resume 等原生接口验证当前所需语义。
  缺失方法、参数不被支持、必要字段不合法均明确报错；不为兼容而改用宽松审批策略，
  不重新发送未知结果的 prompt/审批，不丢弃原有绑定。
- 扩展字段与不关注的通知可忽略；未知服务端请求仍明确返回 -32601。新的审批选项或
  权限语义不能根据字符串或角色反馈推断。experimentalApi 不表示支持所有实验能力。

开发验证保留一份固定 schema，make schema 仍要求对应基线生成器并审查差异。
运行时和无模型 smoke 不调用 --version 作为准入条件，smoke 的 testedCodex 只报告测试
基线。契约回归覆盖不同发行号但协议相同、没有版本命令、最小/扩展握手响应可以接入，
以及基线发行号但协议错误仍拒绝、配置不保存、自有进程被回收。
这些 fixture 证明判断切面，不冒充每个真实历史/未来版本的完整功能验收。

依据：[官方 App Server 初始化与能力声明](https://developers.openai.com/codex/app-server/#initialization)
及仓库内固定 InitializeParams/InitializeResponse schema。未来官方若增加明确的协议版本
协商，再以官方字段扩展兼容策略；不提前猜测字段或版本含义。

## Guardian contract

## 上层装配

新建 Bot Runtime 显式选择 `permissions.approval_mode: auto-review` 和
`reviewer: {kind: guardian, model: <configured model>}`；默认固定初始 Bot 模型，
原生装配可通过 `Options.ReviewerModel` 指定另一个已配置模型。后续主模型变更不替换 reviewer。
`workspace-write`、HOME/CWD、环境继承和 shell 配置独立保留；没有用 full-access/never 代替审查。

连接通过 `application-guardian-review-v1` 协商，读取应用作用域的 `reviewer-state`，核对
Session、模式、reviewer 与 ready 状态；unavailable 明确报错，不转为人工或免审批。
ready 只表示本地装配就绪，不保证下一次模型调用成功。

旧版 Bot 的 manual 是代码固定值，没有用户可选的 manual 模式。已存在或创建结果未决的
Runtime 不热修改；**版本升级后的首次恢复启动**是新配置交接点。两种后端都记录创建时的
Bot 版本；启动恢复完成且旧工作空闲后，复用有效 Dream 交接，或立即发起一次交接整理，
不等待 15 分钟闲置。完成后自动创建新版 Runtime 并保留历史、Notebook、任务和既有提醒授权。
新 Runtime 使用当前版本的工具/指令与环境装配，保留用户选定模型、工作目录和独立沙箱范围。
同版本普通重启恢复原绑定；普通 Dream 仍在下一次用户输入交接。未决结果按原 ID 核对；
失败不形成空闲重试循环，用户输入仍可中断整理并优先继续工作。交接内容只在新上下文首个
请求获持久接受后消费。显式手动验收使用 `RequireApproval`；普通
Session 和 Worker 继续由 Core 各自的原生配置负责，Bot 不改全局审批设置。

## 注入工具的策略与成本

| 工具 / 操作 | 策略 | 原因 |
| --- | --- | --- |
| `bot_memory` 全部操作；Notebook 内文件 | 产品范围内直接执行 | Bot 工作区与低风险记忆，不增加 Guardian 成本；越界文件访问仍受原生政策约束 |
| `bot_clock`、`bot_task_read`、`bot_reminders_list`、`bot_care_read`（list/test） | 直接读取 | 只读观察与纯条件测试不审批 |
| `bot_gesture`、`bot_tasks` 的列表/固定/锁定/清理、`bot_task_stop` | 直接执行 | 本地呈现或停止已拥有的工作；不扩大执行权限，不删除任务历史 |
| `bot_task_start`、`bot_task_send` | 每次派生 / 新指令由 Guardian 审查 | 新执行或扩大工作指令；审批后直接继续，Worker 内部仍有自己的 sandbox / reviewer |
| `bot_reminders` save/remove、`bot_care` save/remove/configure | 修改持续安排时审查 | 审查未来工作授权；读取使用独立免审入口，匹配每次时钟事件不会再审查注册动作 |
| `bot_desktop_observe`、`bot_desktop_capture` | 只读直通 | 保留原系统权限、模型图片能力门控 |
| `bot_desktop_authorize` | 每个 App、每个连续任务 Turn 审查一次 | Guardian 审查本次任务的 App 访问目的和已观察到的 App |
| `bot_desktop_perform` | 当前 Turn 内已获授权 App 免逐次 Guardian | 每次输入前由原生 helper 校验 App 授权和新鲜目标；不按点击次数或操作批次收费 |
| 未明确列出的 callback | `approval_policy: required` | 新能力不会默认为免审；Codex 同样只免审明确列出的工具 |

Computer Use 授权属于当前连续任务，不向用户暴露 Session 概念。相同 App 的窗口共享授权，
另一个 App 必须单独授权。当前实现用原生 PID 与应用名称绑定本次运行的 App，不按窗口标题授权，
不同进程实例不会因同名而继承授权。授权仅在 helper 内存中存在，由 Bot 原生生命周期通过私有
管道提供 Turn 标识；模型参数不能创建或延长 Turn。完成、停止、中断、新 Turn 或 helper 重启后
不复用授权。长任务不设置按分钟或点击次数重新审批的限制。

App 授权不取消输入的新鲜目标检查、不扩大系统权限，也不把页面里的指令变成用户授权。
每次输入仍返回新观察并重规划，未知结果不自动重放。系统权限、登录和必要信息选择保留原交互。
Codex 使用原生 Computer Use 的应用授权语义，不另装 Bot Cua 绕行路径。

## 审查事实与恢复

审查中、批准、拒绝、失败、超时按原 Session/Turn/approval request 关联；保留工具调用及
实际动作证据。审查事实只读，不生成“用户已同意”的回执，也不能通过 Bot 的 Decide 接口
人工抢答自动审查。失败与超时独立于拒绝，不自动回退为手动审批。仅 canonical mirror 的
批准/拒绝进入派生持久记录，进度/失败/超时不伪造成持久决策。

自动审查中和静默通过不消耗可见打扰额度；可见拒绝/失败反馈按对应关怀激活计量。
callbacks 继续沿用原调用 ID、原 catalog 和参数的 claim/result 账本；未知结果只核对，
重连、重复 result、Host 重启都不重新执行副作用。


## Core release acceptance and remaining limits

The public pin is Core #91, `e8281aa7bb89b0ae330044eb5bd6a13916fd68fc`, including #85 environment,
#89 Guardian and #91 item identity. An installed release **and the running Host** must expose the required
capabilities; a release number alone cannot prove compatibility. Core stays generic; Bot supplies the profile.

The isolated external Host suite passes explicit reviewer assembly/readiness, independent sandbox, callback
review before claim, native command/file allow/deny, invalid replies/cancellation/real 90-second timeout with
no side effects, idempotent results, and restart replay without re-execution. Live review, callback and canonical
replay agree on immutable item identity, including repeated provider call IDs in a Turn. Clearing Bot's derived
cache still reconstructs the same native facts. Upgrade renewal preserves history and adopts Guardian; ordinary
Sessions/Workers retain native defaults. Skills remain progressively loaded and resident-only.

These are release capability conditions, not proof of paid Guardian judgment quality. The App × Turn integration
uses the real Bot/helper pipe with a deterministic underlying UI fixture, not real WPS. Core v0.65.0 darwin-arm64 was independently downloaded, checksum-verified and passed this external suite
on 2026-09-29. Rerun it for subsequent Core releases before declaring those artifacts qualified.

Remaining boundaries: unknown upload responses lack a public resource-descriptor lookup and are not retried;
worker artifacts are not yet aggregated into resident download items; cancel/approval uncertainty never retries
under a new ID; optional terminal observation/model image support require their own negotiated capabilities.
Real model, long-running memory quality and cross-Runtime daily Notebook usage need separate evidence.

## Bot-owned Computer Use

Codex owns native Computer Use; Bot never injects a Cua fallback or relaxes those tool policies.
Caelis currently uses the Bot helper when native ownership is absent. An ownership tag is not readiness;
denied permissions or unavailable native tools cannot select an alternate driver automatically.

Observe visible windows, then select a short host-issued reference for AX metadata/actions. Default observation
does not capture pixels. Window contents are untrusted data. Pagination preserves a single snapshot and returns
all bounded pages without discarding unseen windows. New enumeration/helper restart expires old references;
window refs last at most five minutes, observations at most sixty seconds. A fresh observation invalidates old
input targets. The driver rechecks actual window identity/position and selected-window subtree before dispatch.

`bot_desktop_authorize` reviews the observed App and task purpose once per App × native Turn. The helper receives
Turn identity privately from Go; the model cannot create/extend it. PID plus app name binds a running instance.
Grants stay in helper memory and expire on completion, stop, next Turn or restart; multiple windows of the same
App reuse the grant. Observing/capturing is direct; every input still validates the grant and fresh target.

`perform` supports click, text insertion, targeted key and scroll. Up to eight candidate steps are accepted but
only the first mutation executes; new observation and remaining steps are returned. This is not an approval batch.
No arbitrary coordinates, scripts, PID or native tokens from the model. Text insertion does not imply replacement.
Modifier keys use the pinned driver's window/element-scoped background hotkey; unsupported input rejects without
switching execution routes. Dispatch acknowledgment alone is not proof of the intended UI effect.

Full content-v1 results (text plus JSON-escaped structuredContent) fit 32 KiB; outer IPC frames fit 512 KiB.
Truncated targets are removed from the selectable directory; remaining steps drop only as whole steps with a
truncation marker, never by shortening intended input. Optional screenshot requires current model image support,
at most one image/256 KiB; unavailable images leave metadata usable. Window text/images go to the selected model
only for the requested task. OS consent stays independent of Guardian.

`desktopcontrol` owns private pipes to packaged Node 24.21.0 `--jitless` and Cua 0.30.2. Lockfile integrity and Node
SHA-256 are pinned; no global Node, MCP listener or model credentials in the helper. Calls are bounded to eight
seconds. Timeout/cancel/transport loss kills the helper before closing input, preventing delayed validation from
sending input after stop. Only a fresh observe can restart it; uncertain effects are never replayed. Input already
sent to the OS cannot be withdrawn. Stop cancels local input before interrupting the native Turn.

Packaged Cua requires macOS 13.5+ (Node baseline). All nested Mach-O code is signed with the app identity;
`--jitless` avoids new JIT/library-validation exceptions. `script/verify-computer-use.sh` verifies versions,
architecture, signatures, dynamic dependencies/rpaths, licenses and loads the actual binding without requesting
OS access. Tagged historical recovery permits an absent payload only when its version marker is also absent.

Cua is MIT with MPL-covered UniFFI/Node runtime components. Bot remains Apache-2.0 and character assets retain
their own license. Preserve the actual bundled notices, matching source/rebuild references and dependency list
under `resources/computer-use`; do not relabel third-party payloads or assume an unmodified binary needs no notices.
The fixed upstream Cua source is `a2229c5b829153ec3b1828387bc72ca8f1f18704`; package notices identify corresponding
UniFFI sources. Experimental AXorcist/gamepad work is not another production driver.
