# 后端与宿主能力契约

2026-09-23。这是本产品内部 Go 契约，不是新的公共 wire 协议。
Codex 已接通统一产品层；Caelis factory 保留，但新通用协议未接入时禁止执行，
不回退到旧 Bot Mode。历史协议见[接入说明](caelis-integration.md)。Windows 尚未实现。

## 已落实的依赖与所有权

```text
macOS / 未来 Windows 入口
  ├─ desktop：窗口、输入、位置、菜单与材质
  └─ app：产品装配、启动、观察、明确退出
       ├─ backend.Service → api 能力 → provider adapter → 原生执行 owner
       └─ bot：持续身份、提醒与本地工具
              └─ localipc：当前平台的私有工具通信
```

- `internal/app` 是唯一产品装配入口，factory 注册 Codex 与 Caelis。未知 provider 直接报错，
  不回退到 Codex，不覆盖配置或对话。窗口模块不导入具体 adapter 或 Bot 调度器。
- `app.Host` 注入文件选择结果解析/消费、打开 URL/产物、移入废纸篓、角色动作、通知和
  状态展示。系统操作不携带原生任务控制权。隐藏与关闭窗口均不调用 `Application.Close`。
- `Application.Start` 在原生窗口准备好后执行：统一装配 `bot` 工具/提醒和 `tasks` 账本/报告，
  再异步连接 adapter。Codex 使用私有 MCP；未来 callback 与 MCP 共用应用工具业务。
  Caelis 新协议尚未接入，连接明确失败，不创建旧 Bot、绑定 DesktopEffects 或派发旧计划。
- `Application.Close` 只用于明确退出，幂等执行一次：取消连接/观察和提醒，调用 adapter
  清理自有工作，再回收工具连接、等待后台 goroutine。共享服务不因此被整个关闭。
- `internal/botpolicy` 定义固定秘书/工作角色、九项逐一允许的本地工具及工具发现指引，
  由宿主注入 adapter。MCP 与原生审批字段仍由 adapter 翻译；角色不取代执行权限。
- `internal/localipc` 隔离本地传输。macOS 仍使用短路径、私有目录与 0600 Unix socket，
  工具层保留随机 token 校验、消息限长及超时；其他平台明确返回未实现。
- 桌面可选扩展已命名为快捷键、通知、弹层、角色活动/动作、上下文、道具接口。
  上下文缺失返回 unknown/null，道具缺失不启动；它们都不拥有后端 Session/Task。

`internal/app/boundaries_test.go` 守卫直接依赖方向，portability 检查继续阻止 Wails/cgo
泄入共享装配和服务。可编译不代表当地 IPC、进程安全或原生桌面已完成。

## 接口与启用条件

Go 定义在 `internal/backend/api/contract.go`、`capabilities.go`、`tasks.go`、
`work.go` 和 `application_tools.go`。
只把产品 DTO 生成给 renderer；`ToolConnection` 和本机输入路径不进入 UI 协议或诊断。

| 契约 | 语义与责任 | 当前启用要求 |
| --- | --- | --- |
| `Engine` | 连接、快照、提交、中断、审批决定和关闭；adapter 保留原生目标与不确定性 | 必需 |
| `Provider` | 安全名称、帮助链接、连接类型和说明；不含凭据 | 必需，ID 必须与配置及 factory 一致 |
| `SnapshotObserver` | 阻塞至 revision 前进或 context 取消；重连隔离过期实例事件 | 必需，禁止循环返回同一 revision 造成忙等 |
| `BotToolBinder` | 接受宿主角色、工具目录/handler、Notebook 路径与刷新回调、私有传输及逐工具允许列表 | 必需；禁止全局/server 默认放行 |
| `WorkRuntime` | 适配 native 执行、归属、权限、请求回执与原生状态，不分配产品目录/配额 | 必需；每次调用仍受 native admission 约束 |
| `ReportSubmitter` | 空闲时提交应用通知，不把报告提升成新的用户授权 | 必需；不负责选择何时汇报 |
| `ApplicationTools` | 应用工具目录及共享业务回调，不能直接暴露给 renderer | 宿主注入；当前走 MCP，通用 callback 来源/租约待新协议 |
| `TaskProvider` / `TaskReporter` | 应用 `internal/tasks` 的工具入口和有限报告调度 | 由统一产品层实现，adapter 不再持有产品报告循环 |
| `ControlCompanion` | 历史 Caelis 专用 owner 接口 | 桌面装配已不消费，不能替代新端口；随旧 adapter 清理 |
| `PresentationAcknowledger` | 对当前报告的明确用户呈现确认；读取/OS 通知不是 ack | 可选，Caelis 已实现 |
| `ExecutionProvider` | 当前模型目录、effort/速度选择、审批选项及保存；先校验和持久化再生效 | 可选；缺失时报不可用；审批模式名称、危险提示和默认值来自 provider |
| `RuntimeConfigurator` | 验证连接、阻止忙碌/待审批/未知结果时替换，保存后才启用 | 可选；Caelis 与跨 provider 切换检测后保存，下次启动生效；不热替换 owner |
| `Authenticator` | 后端确实需要时启动/取消登录 | 可选；Caelis Host 认证不必伪装成 Codex 网页登录 |
| `ArtifactResolver` / `ApprovalNavigator` | 将 opaque handle 解析成已校验的文件或原生审批 URL | 可选；未知 handle 不能成为任意路径/URL 打开能力 |
| `AttachmentProvider` | 存储统计与经 native trash 回调清理 | 可选；不据此宣称支持所有上传/下载类型 |
| `HistorySource` / 快照优化接口 | 历史分页、轻量输入/气泡快照与 revision 查询 | 可选；缺少优化时保留基础快照路径 |
| `DiagnosticSource` | adapter 白名单构造的诊断事实 | 可选；禁止日志、正文、参数、token 和私有路径 |

接口实现表示适配器具备此操作；具体时刻能否操作仍以快照、授权与原生握手为准。
类型断言不是权限证明，也不代替真实后端能力协商。Caelis 暂留拒绝调用的端口以保持设置窗口可用；
连接与激活明确不可用，这些方法不表示新协议已实现。

模型控制保留 `ExecutionSettings` 产品 DTO，但公共校验只约束输入大小/结构。Codex 自己
验证 `auto/ask/read-only/full-access`，并在连接前拒绝不认识的策略；不能把未知值当默认自动模式。
配置保存回调在原生操作串行化范围内执行。Caelis 更新先获得原生接受回执；
若后续本地保存失败，明确报告远端已接受并重新读取原生设置，不能声称事务回滚。

## 数据与兼容范围

`runtime.json` 接受历史无版本格式，保存时写显式 v1。连接读取不再绑定 Codex 字符串，
但只有已注册 factory 可启动。后端验证属于后端，不允许 renderer 传入工具配置。

共享产品状态为根目录的 `bot.json`、`tasks.json`、`Tasks/`、角色和快捷键偏好。
`bot.json` 保留同一身份；提醒及 queued wake 显式绑定 Runtime，历史无标记记录归 Codex。
旧 Runtime 的计划不会在新 Runtime 中执行或被修改，未知回执不自动重发。

原生记录继续隔离：Codex 保留根目录的 `conversation.json`、`execution.json`、`Work`、草稿与展示文件，
其他 provider 使用 `providers/<provider-id>`。这些 Codex 历史文件不能由另一 adapter 接管；
共享任务目录使用 Runtime + requestId 派生的新句柄，原有 Codex 目录和句柄原样保留。
Notebook 使用应用根下的 `Notebook/`，Memory 使用 `personal/`；名字/描述只作一次初始化用户消息；
契约见 `internal/backend/api/personal.go`，实现与限制见[个人空间](personal-memory.md)。
跨后端设置检测后原子保存，下次启动生效；不能把旧请求、凭据或 native ID 自动投递给它。

## Caelis 历史公共能力（当前桌面已禁用）

`internal/backend/caelis` 只使用公开 HTTP/SSE，协议固定在 `protocol/caelis`。
Control 拥有 Bot/来源/工作归属、受限工作目录、执行目标、审批、有限汇报和提醒授权；
Bot 原生层拥有 scoped credential、独立观察、桌面 effect、通知和计时；renderer 只呈现。
主 Bot 使用自身私有文件能力；不恢复旧 notebook 专用工具限制。

工作工具由 Control 绑定真实用户/提醒来源，adapter 不能从 prose/assignment 生成授权。
审批保存所有原生选项与精确当前 target；后端未确认的操作保留 unknown。
执行网络关闭，工作区隔离，原生审批不能扩大强制上限。明确退出只撤销自己的客户端，
不能关闭共享 Host。路由、证据和未完成的异常恢复边界见[接入说明](caelis-integration.md)。

## 后续独立平台工作

Windows 使用相同的 app/backend/bot 和 renderer。只增加当地 desktop driver、localipc、
原生进程发现/清理、权限/状态替换、材质与分发路径。当前尚未抽出通用进程启动器，也没有
Windows ACL、原子替换或 WebView2 实现；不为收敛边界提前加入成功空壳。
文件落点和分期验收继续见[实施计划](backend-platform-plan.md)。


## 用户专用 Runtime Setup

以下安装/账号管理实现保留；Caelis 执行兼容性和激活在通用协议接通前统一拒绝。
旧 Bot 专属执行配置不是新协议的兼容承诺。

`internal/backend/api/setup.go` 定义首次引导与设置复用的能力；`internal/app/setup.go`
负责 provider 调度与独立路径配置。`InspectSetup`、`SetupCatalog`、`ApplySetup`、
`ActivateRuntime` 只通过原生设置桥调用，绝不能注册为 Bot/模型工具。

- `SetupOverview` 区分当前执行后端、待切换后端与首次引导。选择管理标签不改变执行身份。
- `runtime-profiles/{codex,caelis}.json` 仅保存程序位置与 Caelis 数据目录；`runtime.json`
  保存下次启动的选择。活跃对话继续使用已装配的 adapter，重启时执行原生退出清理。
- 干净安装不自动连接默认 Codex；跳过引导写 `setup.json`，不替用户选择 Runtime。
- Caelis 配置使用用户明确发起操作的 Host credential，独立于 Bot scoped credential。
  全局候选与 connect/delete/use-model 使用固定公开协议；配置写入带 revision 和 operation ID，
  网络失败不重发。密钥不进入 Bot 的偏好、日志或操作 journal。
- Codex Setup 拥有独立标准 App Server 客户端，不创建任务。OAuth 完成必须匹配本次 login ID；
  支持提前完成事件、取消、重开授权页、API Key 与退出。凭据持久化及刷新归 Codex。
- 配置已保存/目录可用不代表真实模型推理成功。界面不自动发送计费测试请求。
- 安装/更新由原生层显式调用官方脚本；Codex 的自动更新仅覆盖官方默认安装位置。
  自定义、npm/Homebrew 安装显示原安装方式的更新指引，不擅自接管另一包管理器。
- Caelis 账号 OAuth/ACP 配置没有在本轮固定公开协议中提供完整浏览器回调流程，界面只展示
  已验证的 API Key/本地模型连接；既有账号配置可复用，不伪造通用登录入口。

共享 Setup DTO 不包含平台 shell 或凭据路径。Windows 的原生安装与重启仍属后续平台适配。
