# 后端与宿主能力契约

2026-09-23。这是本产品内部 Go 契约，不是新的公共 wire 协议。
Codex 与 Caelis 已注册；Caelis 接入范围及验证边界见[接入说明](caelis-integration.md)。Windows 尚未实现。

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
- `Application.Start` 在原生窗口准备好后执行：按 provider 装配常驻能力并异步连接。
  Codex 建立私有 MCP 和本地提醒循环；Caelis 绑定受控 DesktopEffects，由 Control 管理委派与报告。
  Caelis 不创建本地 MCP，不启动第二套提醒唤醒 owner。
- `Application.Close` 只用于明确退出，幂等执行一次：取消连接/观察和提醒，调用 adapter
  清理自有工作，再回收工具连接、等待后台 goroutine。共享服务不因此被整个关闭。
- `internal/botpolicy` 定义固定秘书/工作角色与八项允许的本地工具；Codex-specific 工具发现
  和 MCP 审批字段由 Codex adapter 翻译。角色文字引导分工，不取代原生执行权限。
- `internal/localipc` 隔离本地传输。macOS 仍使用短路径、私有目录与 0600 Unix socket，
  工具层保留随机 token 校验、消息限长及超时；其他平台明确返回未实现。
- 桌面可选扩展已命名为快捷键、通知、弹层、角色活动/动作、上下文、道具接口。
  上下文缺失返回 unknown/null，道具缺失不启动；它们都不拥有后端 Session/Task。

`internal/app/boundaries_test.go` 守卫直接依赖方向，portability 检查继续阻止 Wails/cgo
泄入共享装配和服务。可编译不代表当地 IPC、进程安全或原生桌面已完成。

## 接口与启用条件

Go 定义在 `internal/backend/api/contract.go`、`capabilities.go` 和 `tasks.go`。
只把产品 DTO 生成给 renderer；`ToolConnection` 和本机输入路径不进入 UI 协议或诊断。

| 契约 | 语义与责任 | 当前启用要求 |
| --- | --- | --- |
| `Engine` | 连接、快照、提交、中断、审批决定和关闭；adapter 保留原生目标与不确定性 | 必需 |
| `Provider` | 安全名称、帮助链接、连接类型和说明；不含凭据 | 必需，ID 必须与配置及 factory 一致 |
| `SnapshotObserver` | 阻塞至 revision 前进或 context 取消；重连隔离过期实例事件 | 必需，禁止循环返回同一 revision 造成忙等 |
| `BotToolBinder` | 接受宿主发出的 stdio command/args/env 及逐工具允许列表，映射至该后端的受限能力 | Codex 装配分支必需；禁止全局/server 默认放行 |
| `TaskProvider` | 仅列举、创建、读取、继续和停止此 Bot 拥有的工作 | Codex 装配分支必需 |
| `TaskReporter` | 空闲时投递有限完成通知，保持回执与去重，未知不重发 | Codex 装配分支必需；Caelis 禁止安装第二个触发器 |
| `ControlCompanion` | 受管工作观察与受限桌面执行；远端 Control 唯一拥有委派、完成报告与授权提醒 | Caelis 装配分支必需，与上述三项替代而非叠加 |
| `PresentationAcknowledger` | 对当前报告的明确用户呈现确认；读取/OS 通知不是 ack | 可选，Caelis 已实现 |
| `ExecutionProvider` | 当前模型目录、effort/速度选择、审批选项及保存；先校验和持久化再生效 | 可选；缺失时报不可用；审批模式名称、危险提示和默认值来自 provider |
| `RuntimeConfigurator` | 验证连接、阻止忙碌/待审批/未知结果时替换，保存后才启用 | 可选；Caelis 与跨 provider 切换检测后保存，下次启动生效；不热替换 owner |
| `Authenticator` | 后端确实需要时启动/取消登录 | 可选；Caelis Host 认证不必伪装成 Codex 网页登录 |
| `ArtifactResolver` / `ApprovalNavigator` | 将 opaque handle 解析成已校验的文件或原生审批 URL | 可选；未知 handle 不能成为任意路径/URL 打开能力 |
| `AttachmentProvider` | 存储统计与经 native trash 回调清理 | 可选；不据此宣称支持所有上传/下载类型 |
| `HistorySource` / 快照优化接口 | 历史分页、轻量输入/气泡快照与 revision 查询 | 可选；缺少优化时保留基础快照路径 |
| `DiagnosticSource` | adapter 白名单构造的诊断事实 | 可选；禁止日志、正文、参数、token 和私有路径 |

接口实现表示适配器具备此操作；具体时刻能否操作仍以快照、授权与原生握手为准。
类型断言不是权限证明，也不代替真实后端能力协商。Caelis 依据公开 Host 协商实际能力，不通过方法空实现满足上述要求。

模型控制保留 `ExecutionSettings` 产品 DTO，但公共校验只约束输入大小/结构。Codex 自己
验证 `auto/ask/read-only/full-access`，并在连接前拒绝不认识的策略；不能把未知值当默认自动模式。
配置保存回调在原生操作串行化范围内执行。Caelis 更新先获得原生接受回执；
若后续本地保存失败，明确报告远端已接受并重新读取原生设置，不能声称事务回滚。

## 数据与兼容范围

`runtime.json` 接受历史无版本格式，保存时写显式 v1。连接读取不再绑定 Codex 字符串，
但只有已注册 factory 可启动。后端验证属于后端，不允许 renderer 传入工具配置。

Codex 明确保留原有根目录内的 `conversation.json`、`execution.json`、`Work` 及随绑定管理的
任务目录，不搬移、不重建、不改变原生 ID。新增 provider 的数据只允许放在
`providers/<provider-id>` 下。根目录布局是 **Codex 专属兼容命名空间**，不能被别的 provider 使用。
若后续统一搬移，必须先实现全部文件/任务回执的事务迁移与失败恢复，再移除该分支。

角色、草稿和展示偏好在产品根目录。Codex 本地提醒保留旧布局，Caelis 桌面记录在
`providers/caelis` 下，Control grants 才能授权其唤醒。跨后端设置先检测再原子保存，
下一次启动采用新后端，不能把旧请求、未知提醒或 native ID 自动投递给它。

## Caelis 已消费的公共能力

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
