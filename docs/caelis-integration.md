# Caelis 接入与运行时管理

Bot 与 TUI 使用同一 Host 身份、权限和公共 Control 协议。普通 Session 和显式
profile 的会话共用命令、操作回执、审批与观察接口；没有 Application 注册、独立凭据或租约。
公开 schema/wire 及交付版本的源码和哈希由 `protocol/caelis/manifest.json` 记录。
客户端只消费公开 HTTP/SSE 与生成类型，不导入兄弟仓库。

这是统一协议的破坏性切换：旧 Application 路由不再支持。兼容性按握手、所需能力和
Store 格式分别判断，不把发行版本号作为协议白名单。验证命令和证据范围见
[开发与验证](development.md)；历史版本的结果不代替本次集成或发行制品验收。

Desktop World v0.1.0 通过内置 Agent Plugin 的 Skill 与单一 stdio MCP 服务接入
Codex、Caelis；旧 Go host 和 `bot_desktop_*` 工具已退役。MCP 工具遵循各自
Runtime 的原生审批，不进入 Bot 内置工具免审名单。Bot App 的系统权限与
Runtime 工具审批是两层独立边界。

## 基线与发现

`protocol/caelis/manifest.json` 固定公开 OpenAPI、wire 哈希和源码提交。
运行时通过 `/initialize` 协商 protocol 1、API v1、`caelis.control.envelope/v1`，以及：

- `session-profiles-v1`
- `session-hot-configuration-v1`
- `session-native-execution-v1`
- `session-workspace-binding-v1`
- `session-background-activation-v1`
- `session-resource-transfer-v1`
- `session-profile-control-v1`
- `turn-steering-receipts-v1`
- `execution-configuration-v1`
- `session-guardian-review-v1`

CLI 版本号仅供显示，不是兼容性 allowlist。缺少能力时阻止切换，保留安装、更新和服务管理入口；
不静默换用 Codex 或旧 Bot。安装成功也不代表运行中的共享 Host 已更新。

`session-model-capabilities-v1` 是 Ask Bot 的可选增强：普通 Host 身份读取
`GET /sessions/{session_id}/model-capabilities`，返回 `session_id`、
十进制字符串 `configuration_revision`、desired `model` 和可选 `image_input`。
仅 true 启用；false、缺失、断线、无协商均禁用 Ask Bot，复制/标注/贴图不受影响。
发送前再次读取当前配置，模型切换后恢复，无需更换 Bot 身份。该读取不激活模型，
也不是派发授权；服务端正常执行校验继续生效，旧请求始终按原 ID 核对。

## Guardian 装配与工具审批

新建 Bot Runtime 默认显式装配 Guardian，低风险记忆与读取直通；派生 Worker、持续安排修改
由自动审查处理。Computer Use 按一个 App、一个连续任务 Turn 授权一次。
版本升级和同版本重启都不自动发起 Dream；恢复原绑定与原收据。
不热改当前或未决 Runtime；保留历史、Notebook 与原工作状态。完整策略和发布条件见下文。

## 执行环境装配

Bot 的 workspace-write profile 显式选择继承 Host 环境、非登录 shell；Notebook 只指定 CWD。
启动 shell 恢复由原生 Bot 宿主做一次，Core 按通用配置执行，不再次私有化 HOME 或重置 PATH。
不把整张客户端环境和凭据写入持久 profile，也不为连接中的共享 Host 偷改环境或重启进程。
旧 Host 缺少能力时保留已有数据并报告不兼容，用户经原有更新/启用服务路径处理。

环境配置在创建时固定。恢复/未知创建沿用已有 profile 和原请求；已有无配置的 Session
使用新版 Core 默认值，无需更换 Bot 对话。工具重绑定不热修改环境；模型主动交接的新上下文按当前产品版本重新装配。
原生 Worker 使用 Core 的普通 Session 默认环境，保持自身工作目录及模型配置。
继承 HOME、PATH、CLI 配置不扩大 sandbox 的写入范围，审批仍使用原目标与一次性选择。

## 使用入口

1. 「设置 → 连接与模型」管理 Bot 对话、后台工作和运行时；打开详情不会切换当前后端。
2. App 内置经校验的 Caelis，也可显式选择外部二进制和独立 Store。内置版本随 App 更新；外部 CLI 的安装、更新和服务操作通过公开接口完成。共享服务升级流程见下节。
3. 「连接 → 添加连接」提供账号授权、API Key、内置或自定义 ACP Agent。安装和认证依照原生目录，
   凭据只写入 Caelis；Bot 不持久化密钥或授权码。
4. 能力与模型就绪后显式激活目标运行时；在同一个 App 内准备目标并完成交接，成功后才提交选择。当前对话的工作、审批或未知结果保留原归属；独立 Worker 不因切换而停止。产品身份、Notebook 与 Memory 仍共享。

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

macOS App 在自己的 `Contents/Helpers/caelis` 内携带校验过的正式 Caelis CLI。
默认发现顺序为显式路径、当前 App 内置 CLI、PATH；不修改全局 CLI。内置版本随 Bot App
升级，不在签名包内执行 `caelis update`。协议 schema 与发布二进制分别固定版本和摘要。
共享 Store 仍默认使用 `~/.caelis`，已有模型、认证和 Session 继续由 Core 持有。

程序版本和运行中的服务版本分别显示。内置 CLI 连接时通过公共 `caelis service start`
提交候选版本，由 Core 负责选择、锁、就绪校验和回滚。正在执行的 Session 暂时保留旧 Host；
空闲 Session、后台 Task 和观察连接不阻止换代。Core 的等待器在执行结束后换代，Bot
重新发现新实例并恢复原 Session/回执，不重放请求。早期版本的非原子过渡也由 Core 的
有界兼容路径处理，Bot 不强杀进程，不维护第二套版本选择或升级账本。

显式选择的外部 CLI 保留检查更新和 `caelis update`。安装成功后仍需 `/initialize`
验证协议并读取服务状态；“启用已安装版本”只调用公共生命周期接口，不重复安装。
内置 CLI 缺失或损坏时显示连接设置错误，App 保持可用，不悄悄改用另一个全局版本。
更改 Runtime、程序路径或 Store 必须经过显式激活与原工作交接；不能只改文件来绕过未知回执。运行时切换使用 `internal/app/runtime_switch.go` 的应用内交接流程。

隔离的真实版本互操作验证：

```sh
source script/env.sh
CAELIS_BOT_TEST_PREVIOUS_BINARY=/absolute/path/to/caelis-v0.72.1 \
CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis-v0.72.2 \
go test -race -v -count=1 -timeout 180s ./internal/backend/caelis -run '^TestBundledRuntimeVersionInterop$'
```

该测试从临时 App bundle 运行真实 Bot adapter：验证内置 CLI 优先于全局旧版、旧 Session
执行期间可互操作、结束后自动换代、Bot 自动恢复原身份及回执、旧全局 CLI 可继续执行。
使用隔离 Store 和合成模型；真实供应商模型和原生 GUI 仍单独验收。

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
| 聊天与观察 | `/sessions`、`/{id}/prompt`；canonical Session State、reconnect/SSE；报告使用 `application_summary` |
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

`GET/POST /sessions/{id}/configuration` 使用稳定 operation ID、decimal-string
`expected_configuration_revision` 和局部 patch。模型、effort、service tier、指令、工具目录可在忙碌时保存，
同一 Turn 的下一个尚未发出的请求采用新版本；已发出的请求及其回调保持原版本。
`revision` 是期望配置，`last_request.revision` 是已实际用于模型请求的版本。

同值更新不递增 revision；Notebook 普通写入不更新配置、不强制 compact。
字段缺省保留；instructions/effort/tier 的 `""` 与 tools/native_tools 的 `[]` 按公开契约清空。
adapter 用 map 表达显式空数组，避免生成类型的 `omitempty` 把清空变成未提供。
更新丢响应通过 `/operations/{operation_id}` 取精确历史回执，再读取当前期望配置；
不能用旧回执覆盖后来的配置。目录、执行/继承/权限仍在创建时确定，默认产品装配 `workspace-write` + Guardian auto-review。

原生绑定保存在 `providers/caelis/application.json`，当前本地格式为 v2；文件名沿用以保持
已有安装的发现入口。连接读取 Store 的普通 `runtime/service/auth.token`，不生成或使用
`application-credential.json`。Bot 的工具与 Notebook 通过明确的 profile、标准 MCP/Skills
扩展点装配；它们不构成另一套身份或执行权限。

v1 状态导入集中在 `internal/backend/caelis/statemigration`，仅在加载时转换；第一次保存 v2
前先保存私有 `.v1-retired.json` 原始意图归档。原 Session、Turn、callback、请求 ID 与未知
结果保留；`receiptID` 只指向 Core Store v3 导入后的原回执。资源上传使用独立的历史命名空间
映射，避免与旧命令 ID 碰撞。导入器在最低支持的本地格式升到 v2 时删除；历史归档不参与
运行或授权，保留期限独立决定。更早的 Bot Mode 文件不自动导入。

所有 mutation 先持久化 intent；未知 prompt/create 查询原操作，不改 ID 重发。
worker 启动分阶段保存创建配置、原授权和 prompt，重启后可在确认前一步回执后继续；
已经发出的步骤只读回执。callback claim 丢响应不执行 effect；effect 已记账只重送相同 result；
缺失旧 handler 时明确失败，不把旧调用交给同名新版工具。
SSE replacement 完成后原子切换，游标保持不透明；稳定订阅期间进展不依赖状态轮询。

## Shared native Workers and steering

The adapter requires `session-profile-control-v1` and `turn-steering-receipts-v1`.
New workers use `POST /sessions`, then the ordinary Session prompt,
steer and reconnect APIs. They use the Host's normal environment, tools, MCP,
plugins and permissions; resident Bot profiles and private Memory are not copied.
Explicit work model preferences select a per-Session native model; an empty
preference uses the Host default. The terminal entry point is
`caelis attach --control-url ENDPOINT --session SESSION_ID --store-dir STORE --control-token-file PATH`.
Bot task bubbles resolve only owned native Workers and open this command in the
user's external terminal. Closing it detaches observation without stopping work.
No new Session or prompt is created; credential bytes never enter the command.
The terminal and Bot use the same Host principal and credential scope.

Main and Worker submissions during an active Turn preserve its exact target in
the outbound operation journal. A repeated ID never adopts a later Turn or turns
into a fresh prompt. Accepted steering remains distinct from a canonical
`input_status: applied` event. Session SSE stays open across user-initiated Turns;
steady progress requires no state polling. Maintenance restores failed streams and reconciles uncertain receipts; observation has no authorization lease.

Persisted workers retain their original Session and creation profile. Explicit profiles and
ordinary native workers both use the common Session routes; no old protocol executor remains.
Unknown operations are read through `/operations/{operation_id}` and never redispatched.

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

连接通过 `session-guardian-review-v1` 协商，读取对应 Session 的 `reviewer-state`，核对
Session、模式、reviewer 与 ready 状态；unavailable 明确报错，不转为人工或免审批。
ready 只表示本地装配就绪，不保证下一次模型调用成功。

旧版 Bot 的 manual 是代码固定值，没有用户可选的 manual 模式。已存在或创建结果未决的
Runtime 不热修改；版本升级和普通重启均先恢复原绑定及原收据，不自动发起整理或切换。
模型主动交接的新 Runtime 使用当前版本的工具、指令和环境装配，保留 Notebook、任务、
用户选定模型、工作目录和独立沙箱范围。未决结果按原 ID 核对，交接内容只在新上下文首个
请求获持久接受后消费。显式手动验收使用 `RequireApproval`；普通
Session 和 Worker 继续由 Core 各自的原生配置负责，Bot 不改全局审批设置。

## 注入工具的策略与成本

| 工具 / 操作 | 策略 | 原因 |
| --- | --- | --- |
| Notebook 内文件 | 原生文件权限决定 | Bot 按用户授权维护 `MEMORY.md` 和笔记；`bot_memory` 已从两套 Runtime 目录及直通名单移除 |
| `bot_schedule` context/list/sources/test；`bot_tasks` environments/list/read | 直接读取 | 有界查询与纯条件测试不审批；能力不足时不展示相应分支 |
| `bot_gesture`；`bot_tasks` pin/stop | 直接执行 | 本地呈现或停止已拥有的工作；pin 不执行 Worker、不删除历史 |
| `bot_delegate` prompt（可选原 task） | 每次派生 / 新指令由 Guardian 审查 | 新执行或扩大指令；提交身份由原生调用生成，Worker 保持原生 sandbox / reviewer |
| `bot_schedule_update` save/remove/configure | 修改持续安排时审查 | 保留日历/事件的独立原生 grant、回执和撤销，不因合并而扩大权限 |
| Desktop World 插件 MCP `exec` | Runtime 原生审批 | 启用插件后可发现；关闭后旧会话保留的 schema 也不能继续调用 |
| 未明确列出的 callback | `approval_policy: required` | 新能力不会默认为免审；Codex 同样只免审明确列出的工具 |

CUA 操作还要经过 Bot App 的 Accessibility / Screen Recording 系统授权；
上游私有 worker 保留原生目标、操作与结果边界。未知输入结果按原调用回执核对，
不自动重放。系统授权不代替 Runtime 审批。

## 审查事实与恢复

审查中、批准、拒绝、失败、超时按原 Session/Turn/approval request 关联；保留工具调用及
实际动作证据。审查事实只读，不生成“用户已同意”的回执，也不能通过 Bot 的 Decide 接口
人工抢答自动审查。失败与超时独立于拒绝，不自动回退为手动审批。仅 canonical mirror 的
批准/拒绝进入派生持久记录，进度/失败/超时不伪造成持久决策。

自动审查中和静默通过不消耗可见打扰额度；可见拒绝/失败反馈按对应关怀激活计量。
callbacks 继续沿用原调用 ID、原 catalog 和参数的 claim/result 账本；未知结果只核对，
重连、重复 result、Host 重启都不重新执行副作用。


## Core release acceptance and remaining limits

The exact delivered Core schema and generated wire must be pinned together after integration.
Both the installed executable and the running Host must expose the required capabilities;
a release number alone cannot prove compatibility. Core stays generic; Bot supplies the profile.

The isolated external Host suite passes explicit reviewer assembly/readiness, independent sandbox, callback
review before claim, native command/file allow/deny, invalid replies/cancellation/real 90-second timeout with
no side effects, idempotent results, and restart replay without re-execution. Live review, callback and canonical
replay agree on immutable item identity, including repeated provider call IDs in a Turn. Clearing Bot's derived
cache still reconstructs the same native facts. Upgrade renewal preserves history and adopts Guardian; ordinary
Sessions/Workers retain native defaults. Skills remain progressively loaded and resident-only.

These are release capability conditions, not proof of paid Guardian judgment quality. The App × Turn integration
uses the real Bot/helper pipe with a deterministic underlying UI fixture, not real WPS. Run the external suite against the exact pinned Core binary before declaring a new integration qualified.

Unknown upload responses are reconciled through their original common command receipt; no upload is replayed.
Remaining boundaries: worker artifacts are not yet aggregated into resident download items; cancel/approval uncertainty never retries
under a new ID; optional terminal observation/model image support require their own negotiated capabilities.
Real model, long-running memory quality and cross-Runtime daily Notebook usage need separate evidence.

## Desktop World integration

Both resident backends select the same reviewed v0.1.0 portable package through
PluginManager. The package has one `exec` MCP server and an on-demand Skill.
Bot validates the published archive, BUILD and complete licensing inventory,
re-signs nested Mac executables with the Bot identity, and keeps user-disabled
state on upgrade. Old `bot_desktop_*` tools are removed. No second app or Node
runtime is installed. The two Runtime adapters retain their own MCP approval
policy; no plugin tool joins Bot's auto-approved built-in list.

See [architecture](architecture.md#desktop-and-presentation) for the ownership
contract and [development](development.md#verification-matrix) for real Bot,
fixture, performance and upstream issue evidence. Live WPS success required a
correction; it does not establish first-attempt autonomy or end-to-end speed.

### Host 换代

安装程序与运行中的 Host 分开。用户启用已安装版本时调用公共 `caelis service start`，
由 Core 负责候选版本选择、单 Store 写者、实例校验、空闲准入、启动和失败回滚。
Bot 不复制活动判定；Host 的 `host-idle-shutdown-v1` 决定能否安全替换。
Session 正在执行时保留旧 Host；空闲 Session 的后台任务和常驻订阅不阻止升级。重连核对新实例后继续读取原
Session 和回执，不重新派发旧请求。

早期 Host 的过渡策略由 Core 管理：v0.72.2 之前的已知发行版在确认没有运行中的
Session 后允许公共强制停机，覆盖缺少原子接口以及旧接口将后台 Task 计入 busy 的情况。
新版本保持原子准入保护；Bot 不复制该兼容判断。过渡代码的退役边界在 Core 文档中定义。
