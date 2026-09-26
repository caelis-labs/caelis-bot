# Caelis 接入与运行时管理

协议生成与最低能力基线仍为正式 Caelis **v0.62.0**。
`application-terminal-observation-v1` 是可选增强：支持时启用迟到审批命令的结果跟进，
不支持时正常连接，且不访问该观察接口。审批投影、指定工作区和自动 pin 均不依赖它。
Caelis 可独立交付该修复，无需为此单独发布版本。通用应用运行时与共享原生 Worker 的
公开 wire schema 仍固定于 `812264e567875fe8db6e678fcf7e0e439b18fc6d`。使用公开 HTTP/SSE 与生成的 Go wire，
不导入兄弟仓库、不恢复旧 Bot Mode。基线包含共享会话、steering、Host 设置授权流与 Team 角色候选。

当前验证范围见 [实现与验证状态](preparation-status.md)。v0.61.0 的真实模型和原生 GUI
历史证据见 [正式版联调报告](caelis-release-acceptance.md)，不能代替新增功能的验收。

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

CLI 版本号仅供显示，不是兼容性 allowlist。缺少能力时阻止切换，保留安装、更新和服务管理入口；
不静默换用 Codex 或旧 Bot。v0.61.0 不包含共享 Worker 扩展；安装成功也不代表运行中的共享 Host 已更新。

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
CAELIS_BOT_TEST_PREVIOUS_BINARY=/absolute/path/to/caelis-0.61.0 \
CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis-0.62.0 \
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
不能用旧回执覆盖后来的配置。目录、执行/继承/权限仍在创建时确定，目前产品暴露 `workspace-write` + `manual`。

原生记录保存在 `providers/caelis/application.json` 和独立的 `application-credential.json`。
旧 Bot Mode 的 `binding.json` 等文件原样保留，不迁移到新协议。注册前持久化应用凭据与操作 ID，
此后执行请求使用应用 credential，Host credential 仅用于 enrollment、发现与显式模型配置。

所有 mutation 先持久化 intent；未知 prompt/create 查询原操作，不改 ID 重发。
worker 启动分阶段保存创建配置、原授权和 prompt，重启后可在确认前一步回执后继续；
已经发出的步骤只读回执。callback claim 丢响应不执行 effect；effect 已记账只重送相同 result；
缺失旧 handler 时明确失败，不把旧调用交给同名新版工具。
SSE replacement 完成后原子切换，游标保持不透明；稳定订阅期间进展不依赖状态轮询。

## 复现

```bash
cd /Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot
GOWORK=off make check
GOWORK=off make smoke
GOWORK=off make build
CAELIS_BOT_TEST_BINARY="$HOME/.local/bin/caelis" GOWORK=off GOFLAGS=-count=1 make smoke-caelis
```

`smoke-caelis` 创建临时 HOME、Store、Notebook、worker 目录与合成模型服务，通过真实 Host 的公开接口
运行原生文件/命令。测试不读取日常模型凭据，完成后关闭自己创建的 Host。`make smoke` 的 Codex 部分
仅做已安装 CLI 的握手，不调用模型。

真实模型验收另行显式启用。先在隔离 Store 配置模型认证，再运行（指定 binary 时由夹具启动/停止测试 Host）：

```bash
CAELIS_BOT_LIVE_STORE=/absolute/path/to/isolated-store \
CAELIS_BOT_LIVE_BINARY=/absolute/path/to/installed-caelis \
CAELIS_BOT_LIVE_MODEL=xiaomi/mimo-v2.6-flash \
CAELIS_BOT_LIVE_ALTERNATE_MODEL=openai-codex/gpt-6-luna \
CAELIS_BOT_LIVE_EFFORT=low \
CAELIS_BOT_LIVE_FAST_MODEL=openai-codex/gpt-6-luna \
GOWORK=off make smoke-caelis-live
```

live fixture 带入完整产品工具目录并调用无参数 `bot_clock`，检验 Notebook、资源闭环、待审批重连、双 worker、grant、同 Turn 热配置与 Fast。
会产生模型费用；不复制凭据，不修改日常 Store。Fast selector 使用隔离 Store 已有认证，经公开接口配置。
缺少 Fast selector 或自管 binary 的路径明确跳过相应项目。详见真实模型报告的复现与边界。

## 未完成边界

- 当前基线不支持的配置返回明确 HTTP 400；已验证 Luna priority 正向路径。其他模型仍按能力协商，
  不因 Luna 通过就宣称全部模型支持 Fast。
- Core 尚无按上传 operation ID 查资源描述符的公开接口。上传响应完全丢失时 Bot 保留未知上传 intent，
  不猜 opaque resource ID、不自动重传；需补只读恢复接口或明确支持的恢复契约。
- 主会话和 Worker 的真实用户输入支持 active Turn steering；请求持久绑定期望 Turn，重试不改投新 Turn。权限选择不热更新。
- 未确认的 cancel/approval 仍保留未知，不通过新 ID 自动重试；此类原生命令的完整恢复需要后续专项验收。
- worker 产物已进入其原生投影，但产品报告尚未汇总成主对话下载项；本轮可点击下载闭环验证的是常驻会话。
- 暂无接管其他应用任务、已有项目/worktree 选择、资源过期清理或历史分页归档；Windows 原生适配未实施。


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

The vendored public schema and wire are pinned to the exact source commit in
`protocol/caelis/manifest.json`. The pin matches official v0.62.0. Protocol
unit tests and isolated Host integration are separate from native GUI and
real-model acceptance.
