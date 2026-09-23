# Caelis 接入与运行时管理

2026-09-23：Bot 已接通 Caelis 通用应用运行时，固定源码基线为
`4a3c05964d6d240ec55414e189205930588ef099`。使用公开 HTTP/SSE 与生成的 Go wire，
不导入兄弟仓库、不恢复旧 Bot Mode。已通过隔离 Host 的确定性联调以及 MiMo / GPT-6 Luna 真实模型验收（含 Fast）；
**原生 GUI 操作和发行安装仍待验收**。最新证据见 [真实模型报告](caelis-live-acceptance.md)，
早期确定性 B01–B12 见 [联调报告](caelis-application-acceptance.md)。

## 基线与发现

`protocol/caelis/manifest.json` 固定公开 OpenAPI、wire 哈希和源码提交。
运行时通过 `/initialize` 协商 protocol 1、API v1、`caelis.control.envelope/v1`，以及：

- `application-runtime-v1`
- `application-hot-configuration-v1`
- `application-native-execution-v1`
- `application-workspace-binding-v1`
- `application-background-activation-v1`
- `application-resource-transfer-v1`

CLI 版本号仅供显示，不是兼容性 allowlist。缺少能力时阻止切换，保留安装、更新和服务管理入口；
不静默换用 Codex 或旧 Bot。0.60.1 不包含此基线；安装成功也不代表运行中的共享 Host 已更新。

## 使用入口

1. 「设置 → 运行时」选择要管理的 Runtime；标签不切换当前执行后端。
2. 自动发现本机安装，也可选择已有二进制和独立数据目录。安装、更新、服务启动只在明确点击后
   调用 Caelis 官方安装或 `update`、`service` 能力；不捆绑 Runtime，不接管已经运行的共享 Host。
3. 使用已有模型，或通过「连接新模型」配置。模型凭据由 Caelis 保存；Bot 不持久化模型密钥。
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
`script/build_and_run.sh`；本轮协议测试没有启动日常 Bot 或修改日常 Store。

## 所有权与公开接口

| 产品能力 | 应用责任与 Runtime 映射 |
| --- | --- |
| 持续身份、Notebook、Memory | Bot 持有 Markdown、索引、skill 和 recall/remember；常驻应用会话的 CWD 显式绑定 Notebook |
| 原生文件与命令 | Caelis `workspace-write` 的 Read/Write/Patch/Glob/Grep/RunCommand；不增加 Bot 专用文件工具 |
| 聊天与观察 | `/application/sessions`、`/{id}/prompt`；canonical Session State、reconnect/SSE；报告使用 `application_summary` |
| 专业任务 | `internal/tasks` 分配目录、账本和有限汇报；adapter 将 WorkRuntime 映射为独立应用会话，不继承秘书 skill/Notebook 指令 |
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
SSE replacement 完成后原子切换，游标保持不透明；轮询不能覆盖更新的 SSE 事实。

## 复现

```bash
cd /Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot
GOWORK=off make check
GOWORK=off make smoke
GOWORK=off make build
CAELIS_BOT_TEST_BINARY=/tmp/caelis-application-candidate-20260923/caelis GOWORK=off make smoke-caelis
```

`smoke-caelis` 创建临时 HOME、Store、Notebook、worker 目录与合成模型服务，通过真实 Host 的公开接口
运行原生文件/命令。测试不读取日常模型凭据，完成后关闭自己创建的 Host。`make smoke` 的 Codex 部分
仅做已安装 CLI 的握手，不调用模型。

真实模型验收另行显式启用。先在隔离 Store 配置模型认证，再运行（指定 binary 时由夹具启动/停止测试 Host）：

```bash
CAELIS_BOT_LIVE_STORE=/absolute/path/to/isolated-store \
CAELIS_BOT_LIVE_BINARY=/absolute/path/to/candidate-caelis \
CAELIS_BOT_LIVE_MODEL=xiaomi/mimo-v2.6-flash \
CAELIS_BOT_LIVE_ALTERNATE_MODEL=openai-codex/gpt-6-luna \
CAELIS_BOT_LIVE_EFFORT=low \
CAELIS_BOT_LIVE_FAST_MODEL=openai-codex/gpt-6-luna \
GOWORK=off make smoke-caelis-live
```

live fixture 检验 Notebook、资源闭环、待审批重连、双 worker、grant、同 Turn 热配置与 Fast。
会产生模型费用；不复制凭据，不修改日常 Store。Fast selector 使用隔离 Store 已有认证，经公开接口配置。
缺少 Fast selector 或自管 binary 的路径明确跳过相应项目。详见真实模型报告的复现与边界。

## 未完成边界

- 当前基线不支持的配置返回明确 HTTP 400；已验证 Luna priority 正向路径。其他模型仍按能力协商，
  不因 Luna 通过就宣称全部模型支持 Fast。
- Core 尚无按上传 operation ID 查资源描述符的公开接口。上传响应完全丢失时 Bot 保留未知上传 intent，
  不猜 opaque resource ID、不自动重传；需补只读恢复接口或明确支持的恢复契约。
- 主会话和 worker 暂不支持 active Turn steer；忙碌时继续消息明确拒绝。权限选择不热更新。
- 未确认的 cancel/approval 仍保留未知，不通过新 ID 自动重试；此类原生命令的完整恢复需要后续专项验收。
- worker 产物已进入其原生投影，但产品报告尚未汇总成主对话下载项；本轮可点击下载闭环验证的是常驻会话。
- 暂无接管其他应用任务、已有项目/worktree 选择、资源过期清理或历史分页归档；Windows 原生适配未实施。
