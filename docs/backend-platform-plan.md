# Caelis 接入与 Windows 实施边界

2026-09-22 代码审查与后续计划。Bot 基线为 `ef2f557`；Caelis 核对至
`f136ebaeb46608a69bf7732af57bb2f2a2382756`（0.60.1）。下文新增模块、接口和切片均是
**分阶段方案**，不代表已经支持第二个后端或 Windows。其后的公共边界首片已实现，
当前状态与 Caelis 所需能力见[能力契约](backend-contract.md)。此次没有启动 Caelis Host 做联调。

## 结论与现有缺口

保留当前 Wails/Go + React/Three.js。后端与操作系统是两条独立适配轴：

```text
聊天 / 审批 / 设置 / 角色
              │
       Bot 产品服务与能力契约
          ┌───┴─────────────────────┐
     后端 adapter                原生宿主服务
     ├─ Codex App Server        ├─ macOS（已实现）
     └─ Caelis Control Host     └─ Windows（待实施）
```

Caelis 接入可以现在推进，不依赖 Windows，也不需要重写前端。Windows 原生实施仍在 macOS
完整发行后开始；现在只收敛共享边界。以下是 `ef2f557` 审查时的缺口；装配、显式接口及 IPC 已在后续首片收敛，任务账本和原生平台仍按后续切片推进：

| 当前证据 | 影响与修订落点 |
| --- | --- |
| `internal/desktop/runtime_darwin.go` 直接创建 `codex.Session`，绑定 ChangeCLI、Bot MCP、附件清理与 WaitSnapshot | 原生窗口入口同时承担产品装配。把与 OS 无关的装配移至拟建 `internal/app`；平台入口只注入系统服务 |
| `internal/backend/runtime.go` 只接受 `Runtime == codex`；连接帮助与设置也包含 Codex 固定内容 | 增加 provider factory、分类型连接配置及能力描述，不能只加下拉框 |
| `api.Engine` 已隔离基本对话；扩展接口散落，`ExecutionSettings` 有 Codex 默认值与固定审批选项 | 明确 host-only 能力接口；模型、速度、审批选项由当前后端提供，不把 auto_review 当通用协议 |
| `internal/backend/codex/tasks.go` 同时含原生 thread/turn、目录、角色指令、配额和汇报激活 | 提取产品规则与有限唤醒契约；原生归属、回执和执行状态继续由 adapter/后端负责，不能把此文件复制成 Caelis 版本 |
| `desktop.driver` 与 build tags 已隔离基本原生窗口；快捷键、桌面上下文和道具另有可选接口 | 把现有可选能力显式命名、声明缺失行为；无需创造一个包揽全部 OS 功能的大接口 |
| `internal/bot/tools.go` 使用 `/tmp` 和 Unix socket；Codex 进程实现仅 darwin；状态文件使用 POSIX 权限/rename | Windows 还需要 IPC、进程和私有存储适配，仅实现窗口 driver 不够 |

## Caelis：使用产品 Host，而非嵌入 Runtime

Caelis 已有 `/api/control/v1` HTTP/SSE API、Bearer 认证、版本/能力握手、操作回执与恢复。
审查依据是其[公开 OpenAPI](https://github.com/caelis-labs/caelis/blob/f136ebaeb46608a69bf7732af57bb2f2a2382756/api/control/v1/openapi.json)、
[Host 架构](https://github.com/caelis-labs/caelis/blob/f136ebaeb46608a69bf7732af57bb2f2a2382756/docs/architecture.md)和
[Bot Mode](https://github.com/caelis-labs/caelis/blob/f136ebaeb46608a69bf7732af57bb2f2a2382756/docs/bot.md)。
这是本地代码基线，不据此宣称所有已安装 Caelis 版本都兼容。

拟建 `internal/backend/caelis` 消费该 wire contract，固定所用 schema 与哈希并测试漂移。
不导入兄弟仓库的 `internal`、不使用本地 replace/go.work、不直接打开其 Session JSONL/SQLite、
不在 Bot 内创建第二个 Caelis Runtime/Store owner。首个目标是本机受认证的现有 Host；
Host 未运行时给出明确连接状态，后续自动启动只能经 Caelis 自己的 service 生命周期入口。
原生 ACP 是另一类接入协议，不能替代这里需要的 Bot、任务目录和 Control 操作语义。

| 产品能力 | 当前 Caelis 接口与限制 |
| --- | --- |
| 发现、连接、恢复 | `app/controlserver/discovery.go` 有版本化发现记录；`initialize` 校验协议、Envelope、API 和所需能力。发现文件只是定位信息，不能代替认证握手 |
| 持续 Bot 身份 | `/bots/create`、`/bots/{bot_id}` 和 Bot update；建立本产品身份与后端 Bot 的私有绑定，不通过普通 Session 冒充 Bot |
| 聊天、历史、取消 | Session prompt/state/reconnect/cancel。恢复使用原子 bootstrap 与后续 SSE；保持 revision、cursor、operation outcome，未知提交不自动重发 |
| 模型、effort、Fast | Bot config 已有对应字段，并有模型能力校验；使用 Bot 专属更新路径，不能对 Bot 调普通 Session model/config 路径 |
| 工作会话的审批、任务 | 普通 Session 已有 approval resolve、participant start/prompt/cancel、Task directory/watch/events；这些路径不等于当前 Bot 已获准使用 |
| 附件与产物 | wire 有文本/图片 content parts；不能推导为任意文件上传或产物下载协议。逐项确认本机文件暂存、权限、类型/大小和产物读取路径后再开放 |

**完整接入的主要前置项在 Caelis Bot Mode 的能力边界。** 当前
`app/gatewayapp/bot_runtime.go` 只组装聊天和笔记工具，拒绝 workspace、plugin、collaboration；
`control/appserver/authorizer.go` 对 Bot 只放行检查、prompt、cancel 和 Bot 读写。
因此仅接上现有 HTTP 接口，可以做聊天联调，却无法达到本产品的“秘书安排专业工作”。
修改 Bot 描述或系统提示、给客户端更大的全局权限都不能解决这个能力缺口。

需要在 **Caelis 仓库** 增加受限的 Bot 委派能力，继续由 Control 负责：

- 在 `control/bot` 增加 Bot 到受管工作会话的归属/授权契约，经 `control/appserver`、
  `wirev1`、`app/controlserver` 暴露版本化接口。具体 wire 名称由该能力切片确定，当前没有这些新路由。
- 专业工作仍进入既有工作 Session/Task 执行路径；目录分配、workspace trust、权限、
  请求账本和取消归既有 Control owner。Bot 主对话不因此获得 shell 或项目文件权限。
  现有 participant 的 `background` 不自动提供独立目录，不能等同于“隔离工作区”。
- 扩展 Bot 工具装配及策略，明确委派工具的允许列表和能力协商。现有 `bot-notebook`
  策略允许全部已装配笔记工具，不能直接把新工具塞进去后沿用该放行逻辑。
- 原始用户要求由宿主保存，委派引用明确的用户请求/授权提醒；工作结果、笔记和模型自述
  不生成授权。工作任务继承适用的执行策略，审批目标保持真实 Session/run/item/request 身份。
- 桌面动作、提醒走有生命周期的本地能力连接，只提供必要工具，不给模型 Host Bearer。
  Caelis 主 Bot 是否有该连接必须显式协商；任务不得继承秘书端凭据。不要靠全局插件配置扩权。

这些是产品 Control 能力的扩展，不把 Bot/桌面调度塞进通用 Agent Runtime/SDK。
Caelis 的 notebook 和会话真相继续由 Caelis 持有，桌面端不再维护第二套副本。

## 共享部分与持久化

`api.Engine` 保留基本对话接口；观察、连接、执行选项、任务、附件和 Bot 工具绑定作为明确的
host-only 能力接口放在 `internal/backend/api`。renderer 只获得安全的可用动作、选项和错误，
不拿到原生配置 map、凭据或后台任务导航。必需能力缺失时阻止启用对应功能；可选能力给出
明确不可用状态。不能静默改用别的模型、后端或让秘书自己执行专业工作。

- `internal/app` 已负责创建并连接 backend、bot、系统服务；`desktop` 平台入口负责窗口。
  factory 在装配层选 adapter，避免 `backend` 反向依赖平台，也不让两个 adapter 互相导入。
- `internal/bot` 拥有稳定产品身份、提醒与展示动作，固定角色已抽至 `botpolicy`。
  汇报去重目前仍由 adapter 管理；后续共享化只提取通知规则，不复制执行账本。
  `TaskProvider` 仍只操作本 Bot 拥有的 opaque handles。原生任务事实与变更回执不搬进 renderer。
- Codex 的持久化绑定仍由 Codex adapter 管理；Caelis 的委派归属和操作账本由 Control 管理。
  共享的“已向用户汇报”记录只负责通知，不另建执行状态机。拆分现有完成回执前要有迁移及崩溃测试。
- 将固定角色职责与 provider-specific 工具发现指令分开。通用职责由 Bot 产品定义，Caelis
  在自己的受信任装配层落实；不能通过用户可编辑的 Bot description 伪造系统权限。
- 配置增加版本及 provider 专属字段，凭据仅存私有引用。绑定按产品 Bot、provider、稳定
  后端归属隔离；连接进程的 instance ID 用来隔离过期事件，不作为持续身份。首版只做本地 Host，
  不在缺少远端稳定归属/文件传输协议时开放任意远程 URL。
- 首片将旧 `conversation.json`、`execution.json` 的原位置明确为 Codex 专属兼容命名空间，
  新 provider 使用独立子目录；不搬移活跃任务记录。将来统一迁移必须保留原任务/回执和失败回滚；
  切换后端不复用原生 ID、不自动搬运历史或发送旧草稿。角色与本产品 Bot ID 保持不变。
  有活动任务、审批或未知回执时拒绝切换；首版可保存后重启切换，避免引入热切换竞态。
- 关闭窗口/隐藏角色不停止工作。明确退出按当前产品语义停止本产品拥有的工作并断开连接；
  不调用 Caelis `/host/shutdown`，不停止共享 Host 或其他应用的任务。

## Windows：原生落点

沿用[平台基线](platform-baseline.md)的逻辑坐标、非激活窗口、输入优先级及验收要求。
以下文件/模块为未来实施落点，不在当前提交中添加空壳实现。

| 责任 | 实施落点 | 必须验证的行为 |
| --- | --- | --- |
| 窗口、托盘、焦点、命中测试、原生弹层 | `internal/desktop/runtime_windows.go`、`native_windows.go`；收窄 unsupported build tag | pet 不抢焦点、透明区域穿透、浮层层级与外部点击、关闭窗口不退出 |
| 快捷键、通知、文件选择、URL/文件打开、剪贴板 | 从现有 darwin 装配回调提炼小型 OS 服务；Windows 实现由装配层注入 | 热键冲突回滚、直接输入、IME 候选、中文/长路径、通知唤回，不改通用 UI 流程 |
| 屏幕和 ActiveWindow 上下文 | desktop driver + 带版本的 placement 存储 | Per-Monitor DPI、负坐标、拔屏恢复；未知窗口信息保持 unknown，不猜测窗口与任务关联 |
| 本地 Bot 工具通信 | 已提取 `internal/localipc`，供 `bot/tools.go` 服务端与 stdio forwarder 共用；待加 Windows 实现 | macOS Unix socket；Windows 本机 named pipe、显式用户 DACL、随机能力凭据、限长/超时/回收；拒绝远程访问 |
| 后端进程所有权 | Codex `process_windows.go` / `owned_tools_windows.go`，必要时提炼 `internal/processhost` | 原生可执行文件发现、受管进程组/Job Object 清理；只杀自己启动和拥有的进程，连接共享 Host 不取得 kill 权 |
| 私有存储与替换 | 扩展 `internal/localstate` 的平台实现，并收敛各自的 JSON writer | Windows ACL、可靠替换/失败恢复、锁竞争、Unicode/reparse points；不能用 chmod 或交叉编译代替验证 |
| 资源、材质和分发 | 前端共享资源、Windows surface material、独立打包/更新/CI 路径 | WebView2 中的 GLB/透明合成与输入；使用当地材质或清晰的回退，Liquid Glass 不是通用 renderer 能力；审查现有图标来源及分发许可 |

Windows 的进程组可采用 [Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)，
但要验证嵌套及逃逸情况。Named pipe 必须设置访问控制，不能依赖
[默认安全描述符](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-security-and-access-rights)。
DPI 转换参考 [Per-Monitor 指引](https://learn.microsoft.com/en-us/windows/win32/hidpi/high-dpi-desktop-application-development-on-windows)。
这些是实施选择，不是当前产品已通过的原生测试。原生 Windows 与 WSL 作为不同环境处理，首版不混用。

## 可独立提交的切片与验收

按下面顺序推进；每一行可以继续拆成独立 commit，不做跨两个仓库的大规模同时重写。

| 切片 | 仓库与内容 | 合并/开放门槛 |
| --- | --- | --- |
| C0 后端装配与契约 | Bot：装配、显式能力和兼容命名空间首片已完成；第二后端前补稳定 Host 绑定及切换事务 | 保留旧绑定不重建；若搬移另做事务迁移/回滚；忙碌/未知状态禁止切换、能力不支持不派发；现有 Codex 契约及真实工作流回归 |
| C1 Caelis 基础 adapter | Bot：固定 Host schema，连接/认证、Bot 绑定、聊天/恢复/取消及模型选项；先不作为完整连接选项发布 | 临时 Store 的真实 Host 联调；过期 instance、丢 SSE、未知回执不重发、错误 revision/审批目标拒绝；笔记与历史恢复 |
| C2 Caelis 委派能力 | Caelis：Control-owned Bot 工作授权、受管工作区、工具及桌面能力连接；公开版本化契约 | 普通 Bot 原边界不隐式扩权；两项独立工作可并行、主对话可用、原生审批/取消/重启恢复，凭据与任务归属隔离 |
| C3 完整产品接入 | Bot：Caelis TaskProvider、有限完成通知、附件/产物闭环、设置按能力呈现；固定支持的已发布 Caelis 合同 | 与 Codex 共用场景验收：创建→执行→审批→继续→结果→一次汇报；真实 Caelis provider 联调、退出只停止自有工作；此前不宣称完整接入 |
| W0 共享 OS 服务收敛 | Bot：在真实调用点提炼 IPC、进程/文件服务和原生能力描述，保留 macOS 实现 | macOS 原生回归 + 共享契约测试 + 现有 portability 检查；不把 Windows 标为可用 |
| W1 Windows 宿主 | macOS 发行后：原生窗口/托盘/输入、WebView2、存储/IPC/进程与位置迁移 | Windows runner 执行测试/原生编译，加实际设备验证穿透、焦点、IME、DPI/热插拔、后台任务与退出清理 |
| W2 Windows 后端及发行 | 两个 adapter 在原生 Windows 联调、安装/卸载/更新和制品签名 | Codex 与 Caelis 分别完整验收；缺失组合明确禁用；达到平台验收再更新支持矩阵 |

当前 CI 只有 macOS runner；`script/check-portability.mjs` 在该宿主运行共享测试并交叉编译
darwin/windows 测试二进制，未执行 Windows 测试。后续适配器一致性测试应放在拟建
`internal/backend/contracttest`，由两个 adapter 的测试驱动提供相同场景，协议特有测试留在各自包内。
原生窗口、进程权限、安装和真实模型调用的证据分别记录，不能用 fixture 或交叉编译顶替。

原始审查之后已实施公共边界首片，详见能力契约与 preparation-status。仍未修改 Caelis 仓库、未增加后端选项、未启动 Windows 实施。
