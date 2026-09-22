# 后端与宿主能力契约

2026-09-22。这是本产品内部 Go 契约及 Caelis 接入要求，不是新的公共 wire 协议。
当前只有 Codex 注册为可用后端；Caelis 和 Windows 的完整实现仍待后续切片。

## 已落实的依赖与所有权

```text
macOS / 未来 Windows 入口
  ├─ desktop：窗口、输入、位置、菜单与材质
  └─ app：产品装配、启动、观察、明确退出
       ├─ backend.Service → api 能力 → provider adapter → 原生执行 owner
       └─ bot：持续身份、提醒与本地工具
              └─ localipc：当前平台的私有工具通信
```

- `internal/app` 是唯一产品装配入口，factory 当前只注册 Codex。未知 provider 直接报错，
  不回退到 Codex，不覆盖配置或对话。窗口模块不导入具体 adapter 或 Bot 调度器。
- `app.Host` 注入文件选择结果解析/消费、打开 URL/产物、移入废纸篓、角色动作、通知和
  状态展示。系统操作不携带原生任务控制权。隐藏与关闭窗口均不调用 `Application.Close`。
- `Application.Start` 在原生窗口准备好后执行：创建常驻 Bot、建立私有工具连接、绑定
  后端工具、启动提醒/状态观察，再异步连接。工具绑定失败不连接，并回收临时 IPC。
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
| `BotToolBinder` | 接受宿主发出的 stdio command/args/env 及逐工具允许列表，映射至该后端的受限能力 | 必需，在首次连接前配置；禁止全局/server 默认放行 |
| `TaskProvider` | 仅列举、创建、读取、继续和停止此 Bot 拥有的工作 | 必需，聊天-only adapter 不作为完整秘书发布 |
| `TaskReporter` | 空闲时投递有限完成通知，保持回执与去重，未知不重发 | 必需；此版仍由 adapter 管理完成账本，尚未迁移到共享队列 |
| `ExecutionProvider` | 当前模型目录、effort/速度选择、审批选项及保存；先校验和持久化再生效 | 可选；缺失时报不可用；审批模式名称、危险提示和默认值来自 provider |
| `RuntimeConfigurator` | 验证连接、阻止忙碌/待审批/未知结果时替换，保存后才启用 | 可选；目前仅同一 provider 改连接，跨 provider 热切换一律拒绝 |
| `Authenticator` | 后端确实需要时启动/取消登录 | 可选；Caelis Host 认证不必伪装成 Codex 网页登录 |
| `ArtifactResolver` / `ApprovalNavigator` | 将 opaque handle 解析成已校验的文件或原生审批 URL | 可选；未知 handle 不能成为任意路径/URL 打开能力 |
| `AttachmentProvider` | 存储统计与经 native trash 回调清理 | 可选；不据此宣称支持所有上传/下载类型 |
| `HistorySource` / 快照优化接口 | 历史分页、轻量输入/气泡快照与 revision 查询 | 可选；缺少优化时保留基础快照路径 |
| `DiagnosticSource` | adapter 白名单构造的诊断事实 | 可选；禁止日志、正文、参数、token 和私有路径 |

接口实现表示适配器具备此操作；具体时刻能否操作仍以快照、授权与原生握手为准。
类型断言不是权限证明，也不代替真实后端能力协商。Caelis 后续只在对应 Host 合同可用且
完整验收后注册 factory，不通过方法空实现满足上述要求。

模型控制保留 `ExecutionSettings` 产品 DTO，但公共校验只约束输入大小/结构。Codex 自己
验证 `auto/ask/read-only/full-access`，并在连接前拒绝不认识的策略；不能把未知值当默认自动模式。
配置保存回调必须在原生操作串行化范围内执行，磁盘失败不能改变当前配置。

## 数据与兼容范围

`runtime.json` 接受历史无版本格式，保存时写显式 v1。连接读取不再绑定 Codex 字符串，
但只有已注册 factory 可启动。后端验证属于后端，不允许 renderer 传入工具配置。

Codex 明确保留原有根目录内的 `conversation.json`、`execution.json`、`Work` 及随绑定管理的
任务目录，不搬移、不重建、不改变原生 ID。新增 provider 的数据只允许放在
`providers/<provider-id>` 下。根目录布局是 **Codex 专属兼容命名空间**，不能被别的 provider 使用。
若后续统一搬移，必须先实现全部文件/任务回执的事务迁移与失败恢复，再移除该分支。

产品身份、提醒、草稿和展示偏好仍在产品根目录。本轮没有开放 provider 切换；真正启用
第二个后端之前，必须补齐稳定 Host 归属绑定、已发送/未知提醒的后端归属和切换事务，
避免把旧请求自动投递给新后端。新建连接配置字段应随 Caelis 协议切片引入，当前不预置无效 URL/token 输入框。

## Caelis 需要提供的能力

后续实现必须走 Caelis Control Host，不能把一般工作 Session 当成主 Bot。主 Bot 的身份、
笔记和历史继续由其既有 Bot owner 管理。以下是下一合同的要求，**不是现有路由或字段**：

1. **受管工作归属。** Control 持久化 principal → Bot → 工作会话/工作目录映射。一个 Bot
   工作句柄可承载多次执行；Caelis 的 Task/Job ID 只标识具体异步执行，二者不互换。
   列举/读取仅返回所属工作，原生 ID 和任意目录参数不能突破这个范围。
2. **用户来源。** 委派工具运行在受信任 Bot 激活中，来源关联已持久化的用户请求或已授权
   提醒。Control 验证此关联；模型传来的 assignment、description、附件与工作结果不生成授权。
3. **独立工作区。** 创建时由 Control 分配工作目录/范围，复用现有 workspace trust 与
   沙箱 owner；后续接入已有项目另走项目授权。背景 participant 本身不是目录隔离证明。
4. **精确变更。** 创建/继续使用稳定 operation ID 和原请求摘要，重复同内容返回既有结果，
   相同 ID 不同内容拒绝。中断锁定实际活动 turn；未知/冲突先观察恢复，禁止生成新 ID 绕过。
5. **执行权限。** 主 Bot 仅获得受限委派工具及自身能力，工作会话保留模型/策略与原生审批。
   不能把新工具加入目前笔记工具的整体放行策略；工作任务不继承秘书端或 Host 的控制凭据。
6. **独立观察与审批。** 工作事件不混进主对话；主 Bot 等待期间仍可接收用户输入。
   待审批要保留原目标、范围和真实选项；过期实例/turn 的决定不得被重连后重新投递。
7. **完成与退出。** 每次原生执行结束只产生一次待汇报通知，携带句柄/状态，不把工作正文
   提升为新用户命令。结果已读、明确停止或未知回执保持去重。客户端退出只能停止其自有工作，
   不能停止共享 Host；重启根据原生事实恢复，不能为了恢复 UI 自动再次执行。
8. **桌面能力连接。** Host 必须提供绑定到 Bot 激活/客户端生命周期的受限工具接入点，
   支持当前 `ToolConnection` 所需的本地动作/提醒。Control 校验授权范围和失效，主 Bot
   无此能力时不能假装已提醒或已驱动角色；不修改全局 MCP/plugin 配置来取得权限。

连接认证、身份/版本/能力协商和 SSE bootstrap 沿用 Caelis 已有协议。新能力在 Caelis
`control/bot`、`control/appserver` 及公开 wire/HTTP 边界实现，SDK 不承担桌面产品调度。
具体 schema/capability 名称在 Caelis 能力切片中固定；Bot adapter 按发布合同做一致性测试。

## 后续独立平台工作

Windows 使用相同的 app/backend/bot 和 renderer。只增加当地 desktop driver、localipc、
原生进程发现/清理、权限/状态替换、材质与分发路径。当前尚未抽出通用进程启动器，也没有
Windows ACL、原子替换或 WebView2 实现；不为收敛边界提前加入成功空壳。
文件落点和分期验收继续见[实施计划](backend-platform-plan.md)。
