# Windows 适配重建方案

状态：2026-10-16 完成工作区准备与基线调查；2026-10-17 **P1–P3 主要实现与原生组件验证已完成，P4 完整产品链路验收进行中**，见第 9 节。
以当前 [产品合同](product.md)、[架构](architecture.md)和实际 macOS 实现为准；
[界面设计](ui-design.md)用于视觉参考，过时的说明见[缺陷清单](windows-adaptation-issues.md)。
本方案接替旧 [Windows handoff](windows-11-handoff.md)，不把旧 preview 的验收结果算作新版本通过。

## 1. 工作区与版本基线

| 项目 | 已完成的准备 |
| --- | --- |
| 旧适配备份 | 本地分支 `codex/windows-preview-backup-2026-10-16`，完整保留 `96b1fb55ea2766afd8c64c8d9a062a56ca598587` |
| 原分支 | `codex/windows-preview` 保留，含 `d944db3`、`96b1fb5` 两个本地 Windows 提交 |
| 新主线 | fetch 后的 `origin/main` = `95882f1cab67605403d03f8446a128cda8d8eee8`；本地 `main` 已同步 |
| 新工作分支 | `codex/windows-main-realignment` 从上述主线直接建立，没有合并旧 Windows 代码 |
| 产品版本 | `package.json` 为 `0.14.0`；当前主线还包含 v0.14.0 tag 之后的 Apple ticket 传输重试修复 |
| 原基线差距 | 旧 Windows 基于 `9e98155` / v0.10.0；主线已前进 42 个提交 |
| 忽略文件 | 原 `.cache/` 验收日志、更新密钥和 `dist/releases/` 安装包仍在本机，不属于 Git 分支备份；依赖和前端生成目录可以重新构建 |
| 发布资料 | 本机文档目录下的 `Caelis Release Materials` 保留；本轮未生成、轮换或上传密钥 |

切换前工作区无未提交文件，因此无需额外快照提交。工作区准备完成时，以上分支均仅在本地，未推送、未发布。
后续每个小改动合入前重新 fetch，核对主线新增行为，禁止把备份整批 cherry-pick 或覆盖回来。
备份分支只作参照，不继续双线维护两套产品逻辑。

### 已改变的合同

| 范围 | 新主线要求 | 旧实现的处理 |
| --- | --- | --- |
| 桌宠点击 | 单击开关聊天；双击唤回聊天及已打开的设置；保留草稿 | 不恢复单独的旧输入面板 |
| 聊天/设置 | 同一个上下文入口、保留视图状态与切换尺寸；最新连接与模型页 | 复用当前前端，不恢复旧 AI 模型/AI 账号双页 |
| Caelis | App 内置 v0.72.2、统一公共 Control Session、共享 Store/Host；热切换成功后才提交选择 | 重做装配；旧 v0.67.2/Application 接口代码与演示不再构成证据 |
| Computer Use | Desktop World rc.9 内置插件，Skill + 单一 `exec` MCP，经原生 Runtime 审批 | 不恢复已退役的 Go host、`bot_desktop_*` 或免审名单 |
| 记忆与上下文 | `bot-core`、Notebook/MEMORY.md、模型主动 `bot_dream` 交接 | 不恢复 `bot_memory`、旧 Skill 名或空闲/升级自动 Dream |
| 插件/渠道 | 插件凭据、OAuth、生命周期；Telegram 与微信已存在的消息能力 | 补齐新功能的 Windows 依赖；不能只迁移旧 Telegram 接口 |
| 更新发行 | macOS 当前只有正式安装产品与本地 Dev，原 Sparkle 发布链保持 | 单独添加 Windows preview 发布，不复活主线已撤销的 Mac preview/candidate 流程 |

## 2. 目标、承诺与范围

目标是当前 macOS 已实现的**全部核心功能和用户操作结果对齐**：入口、引导、聊天、设置、
Runtime、附件、审批、任务、插件、消息渠道、截图、Computer Use、启动、更新和恢复。
Windows 的托盘、系统菜单、文件选择器和快捷键遵循 Windows 原生习惯；不仿造 AppKit 材质或 TCC 页面。
关闭界面、隐藏宠物、关闭任务终端均不得被解释为停止工作。

- 首要验收：Windows 11 x64、普通用户的交互式桌面，不要求管理员或开发者模式。
- 兼容目标：Windows 10 22H2 x64；Server 2022/2025 x64 Desktop Experience 已登录桌面尽力支持。
  拟以 Win10 22H2 为新 preview 安装下限，最终需先核对 Wails/WebView2、Caelis、DTW/VC runtime 并实测。
  旧 preview 的 1903 技术下限不能自动成为新依赖的支持承诺。
- Win10/Server 未验收前只标记“待验证”，不从本机 Win11 或 CI 推断；若依赖确有硬限制，在发行说明收窄承诺。
- ARM64 原生、Server Core、无交互服务、UAC 安全桌面、Linux GUI 不在本轮目标内。
- 桌宠情境行走、独立道具等非核心表达，以及 SSH 远程机器终端，可按此前决定后置。
  保留共享配置/原任务数据，Windows 不呈现无法执行的入口；不能影响 Mac 已有能力。
  缩放、基础动画/表情、拖动、气泡、任务 Dock、普通本地终端属于核心，不一起延期。
- 可选任务窗口缩略图允许失败时保留文字卡片；不能因此丢失任务或阻断终端。
  第三方账号登录、微信扫码、跨机器兼容和受保护窗口不能用 fixture 宣称完成。

## 3. 架构隔离与 macOS 保护

### 保持一套产品核心

沿用 `internal/desktop/assembly.go → internal/app → backend.Service`，只装配一次 Application。
保持当前 DTO、配置/收据格式、任务路由、聊天日志、插件目录与 React 页面。
共享层不增加 Win32/AppKit 依赖；renderer 不获得路径、PID 或原生请求 ID 的执行权。

优先在现有消费者旁增加小型 `_windows.go` 适配器，而非复制整个 Mac runtime 或新造第二个产品核心：

| 边界 | 计划的 Windows 实现 | macOS 约束 |
| --- | --- | --- |
| `desktop` 原生宿主 | Wails/WebView2、Win32 窗口/托盘、焦点、DPI、截图/剪贴板/文件对话框/通知 | `_darwin.go/.m` 保持行为；抽取共享展示描述时先证明输出等价 |
| `localipc` | 当前用户 Named Pipe、对端身份验证、取消/超时、原实例身份 | 保留 Unix socket 行为与错误语义 |
| `localstate` / `secretstore` / Runtime binding | 私有 ACL、原子落盘、重解析点/路径边界检查、持久化失败保留原收据 | 保留 POSIX 模式/所有者检查，不全局放松安全校验 |
| `backend/codex` / `runtimeenv` | EXE 发现、原生 argv、无闪窗子进程、自有进程 Job/出生标识、原实例恢复 | 不改变 Mac 进程组、socket、共享 daemon 所有权 |
| `caelisruntime` | Windows 内置 CLI 路径和完整性检查；公共 service 生命周期 | 不改 Mac bundle 优先级、签名、公证或共享 Store 语义 |
| `plugins` | Bot 主进程存活锁、MCP 子进程回收、OAuth 跨进程锁、普通用户 Skill 投影 | 保留现有版本/连接 generation 撤销与审批策略 |
| `taskterminal` | 独立 console helper、Windows Terminal/Console Host、原 Worker attach 与收据 | 不改变 Mac LaunchServices/关闭取消语义 |
| 更新/自启动 | Windows 独立 updater、Run 注册、Setup/ZIP 布局 | Mac Sparkle、SMAppService、既有产品 ID 和 feed 不变 |

新主线主动把插件、Telegram、微信凭据统一为 `secretstore.FileStore`；Windows 应在这个合同下补 ACL，
**不能把旧分支的 Credential Manager 改动整批覆盖回来**。若需读取旧 Windows 试用资料，使用单次、
可核对的迁移适配器；持久化并读回成功前不删除旧项。无需新造第二套认证或复制 Caelis Store。

共享修改分两步：先引入小接口并保持 Mac 默认实现等价，再接 Windows 实现。
避免一次性搬迁整个 `internal/desktop`；只有实际出现第二个消费者时才抽取公共行为。
同一 PR 不混入协议升级、UI 重设计和 OS 改写。不得降级 Wails 或回退主线修复来迁就旧代码。

### 回归门槛

1. Windows 专属变更保持独立 build tags；共享行为、前端、依赖、协议和发布工具仍触发现有 Mac/Windows/Linux checks。
2. 保留 `product` fail-closed 检查；不通过大面积 skip 或把 Windows job 改成可忽略来制造通过结果。
3. 每项共享修复带原失败场景和平台差异测试。纯 POSIX 权限断言改为验证实际 OS 权限，而不是简单删除。
4. 涉及原生 Mac 装配、窗口、依赖/资源或发布的改动，必须在 Mac 跑 `make check`、`make smoke`、`make build`，
   经 `script/build_and_run.sh` 检查实际窗口；发布仍走原 Developer ID、公证、staple、Gatekeeper。
5. 本机无法证明 macOS UI 未回归。需要 Mac 实机或维护者验收；相应步骤未完成就保留未验收状态。
   平台专属 Windows 功能不等 Mac 新版一起发布；共享行为回归不能以赶 Windows release 为由跳过。

## 4. 功能与交互对齐清单

下表“复用”指当前主线的共享代码，不代表 Windows 已可用。所有新原生路径都要重新验收。

| 核心领域 | 必须落地的 Windows 路径 | 验收重点 |
| --- | --- | --- |
| 常驻壳与身份 | 托盘、单实例、按安装渠道分离数据、隐藏/关闭/退出语义 | 第二次启动只唤回；关闭最后一个面板仍工作；退出有明确回收边界 |
| 桌宠 | 透明点穿、单/双/右键、拖动、65%–160% 缩放、位置恢复 | 单击不误触双击；拖动不触发聊天；跨屏负坐标、混合 DPI、不抢输入；休眠/解锁恢复 |
| 气泡与任务 Dock | 主线 Markdown、流式正文、悬停展开、滚动、任务排序/固定/清理 | 无矩形白底/多余阴影；阅读不抢焦点；卡片事实不由动画驱动；聊天打开仍有任务入口 |
| 上下文聊天/设置 | 复用新主题、页面和 retained-view 行为；原生窗口切换、标题栏、键盘 | 草稿/附件/阅读位置不丢；最小化/关闭/重开；IME、中文字体、缩放、Tab/Escape/焦点回还 |
| 首次引导 | Runtime 准备/连接 → 附加功能 → 可跳过权限 → 身份/开始聊天 | Caelis 自动准备内置服务；Codex 不捆绑；失败可重试；介绍只提交一次；旧安装不重复引导 |
| 连接与模型 | 当前统一页、连接授权、模型多选、默认工作模型、执行权限、可选 Team/ACP | 原生结果先于成功提示；取消/失败/未知不重复提交；默认工作工具不改旧任务 |
| Runtime 切换/恢复 | Codex handshake；Caelis 统一 Control；共享 Host 状态、热切换与失败回滚 | 切换不中断外壳；未知操作沿原 ID；已停止服务只按公开合同恢复，不终止共享 Host |
| 对话/审批 | 本地 IM 历史、流式/steer/stop、问题、真实审批选项、重连/离线 | 新输入不等 Worker 历史；恢复无重放；自动审查进度不制造用户审批；局部错误不退出 App |
| 附件与制品 | 拖入、粘贴、选择文件、图片缩略图/原图、文件预览/打开/复制/另存/定位 | UNC/盘符/Unicode/空格、临时 Artifact 绑定、对话框等待、换页回填；路径文字不是权限 |
| 记忆与交接 | Notebook/MEMORY.md、bot-core 元数据与按需加载、bot_dream、旧数据兼容 | CRLF/LF、长/中文路径；旧记忆不丢；不恢复 bot_memory；两 Runtime 的 Worker 不继承 Bot 私有 Skill |
| 本地工作任务 | 创建/续接/停止/读结果/卡片、运行中审批和完成反馈 | Worker 与 Bot/观察终端生命周期分开；中断/恢复都用原任务和回执 |
| 任务终端 | 系统终端、显式 Console Host/Windows Terminal、已支持的参数模板 | 同一 Worker attach、Unicode/stdin/out、专属实例复用；普通关闭保留取消语义；更新可强制关闭观察客户端 |
| 手动截屏/贴图 | F1 选区/当前窗口、标注/文本/箭头/脱敏/撤销、保存/复制/Ask Bot；F3 贴图/重编辑 | 实际像素、DPI/多屏、最终导出与缩略图都脱敏；模型图片能力发送前复核；开关不影响普通聊天图片 |
| Computer Use | rc.9 插件完整包、唯一 exec、Skill、父进程锁和 generation 撤销 | 从 Bot UI 通过 Caelis/Codex 真实模型操作明确测试窗口并读回；退出/停用后旧桥不可继续操作 |
| 插件连接 | 当前目录、安装/启停/更新、MCP 状态、API key、GitHub Device Flow、Notion OAuth | 普通用户安装；OAuth 取消/刷新竞争；秘密不入前端/日志；Brave 缺 Node 22+ 明确提示；不静默安装依赖 |
| 消息渠道 | Telegram 及当前微信已交付的文本/附件/文件与编号审批能力 | 原身份/原请求去重、配对、跨入口镜像、预算、断线队列；不扩展微信 POC 的账号/服务能力承诺 |
| 提醒/关怀/通知 | 共用调度；Windows presence/锁屏/休眠/idle、Toast、点击恢复聊天 | 锁屏/未知不派发；隐藏宠物仍调度；通知点击/冷启动无重复任务和默认授权 |
| 设置/维护 | 所有当前设置目的页、角色/服装/头像导入、快捷键、主题/语言、存储/诊断 | 开关与 OS 事实分开；无 macOS 专属授权按钮；清理到回收站、凭据/原文件/未知收据受保护 |
| 自启动/更新/发行 | 当前用户开机登录启动、后台更新、Setup + ZIP | 自动启动不抢焦点；两个版本真实升级、任务/数据/凭据保留；失败回滚/恢复有原收据 |

右键菜单对照当前 `native_darwin.m`：宠物菜单为聊天与当前可见性切换；托盘承接截图/贴图、
设置、更新和退出等应用操作。Windows 使用相同动作所有权与顺序层次，不照搬 AppKit 菜单实现。
设置入口包括通用、外观、连接与模型、消息渠道、插件、远程机器、附加功能、隐私与权限、更新与帮助；
受限平台通过明确能力状态隐藏/禁用相应动作，不在组件内散布未经测试的 OS 字符串判断。

## 5. 分阶段实施与完成条件

| 阶段 | 工作与主要文件边界 | 必须交付的证据 |
| --- | --- | --- |
| P0：已完成 | 分支备份、主线同步、源代码调查、基线检查、缺陷台账与本方案 | 备份/主线 SHA；准备阶段未启动旧代码迁移 |
| P1：原生基础与可行性门 | 优先修复 B01–B06；`localstate/localipc/secretstore/runtimeenv`、Codex 进程与 Skill 投影；验证内置 Caelis/DTW Windows 依赖与更新占用方案 | 普通用户真实 ACL/pipe/进程归属测试；共享层无 Wails/cgo；两个 Runtime 无模型握手；阻塞项给出可复现报告 |
| P2：最小完整外壳 | `runtime_windows.go` 及小型窗口/托盘适配器，调用当前 assembly；新引导、上下文窗口、桌宠与菜单、图标字体/DPI | 安装身份与单实例；实际可见/可拖动；单双右键与关闭行为录像/截图；不复活旧输入窗 |
| P3：核心工作闭环 | 接当前 Control、热切换、历史/审批/制品、Notebook/Skill、任务与 console helper、渠道/插件凭据 | Caelis 和 Codex 分别真实聊天、文件任务、审批/停止/恢复；同一 Worker 终端关闭/重开；共享失败回归转绿 |
| P4：截图与 Computer Use | 手动截图驱动/编辑/贴图、rc.9 包完整性、MCP 生命周期、原生权限与焦点状态 | 原生截图最终保存/取消、脱敏读回；两 Runtime 真实 GUI 编辑并保存文件；停用/退出/重启旧桥拒绝；不可用路径明确说明 |
| P5：系统集成与完整 UX | 通知、presence/关怀、自启动、全部设置/快捷键/角色资源、插件 OAuth/消息渠道走查 | 通知实际点击/冷启动、自启动重登录、IME/无障碍/多屏；每条矩阵标记通过或具体限制 |
| P6：交付与更新 | 独立 Windows 构建/NSIS/ZIP、Ed25519 feed、更新 helper、CI 路由与发布 | 同源两制品；两版本升级与故障演练；实际安装/卸载/保留数据；Mac checks 和受影响实机路径通过 |

每阶段拆成可独立评审的小 PR，不等整个 Windows 版本写完再回主线。P1 的可行性探针先消除依赖风险，
P2 后持续做真实 UI 走查；不能到 P6 才发现内置 Host 文件无法替换。
截图、Computer Use、任务终端、更新是核心交付条件，不能用按钮占位或关闭这些入口替代完成。
时间安排按阶段出口估算，依赖尚未验证前不承诺发布日期。

### 旧分支的选择性复用

| 旧工作 | 迁移方式 |
| --- | --- |
| Named Pipe、ACL、Job Object、无闪窗启动、EXE argv | 复查新消费者合同后逐个适配；带旧反例测试，不直接覆盖当前共享文件 |
| 窗口透明/显示修复、DPI、字体/图标、截图像素与编辑器 | 借用平台经验，接当前 surface/设置/主题/截图 DTO；重新验证 Wails beta.28 |
| console helper、精确客户端关闭、NSIS/ZIP/updater | 保留设计与反例，按新内置 Runtime/插件布局改造；旧包合格证不能沿用 |
| Caelis Application 协议、旧 desktopcontrol、旧 Skill、旧 onboarding/聊天布局 | 不迁移；以当前主线重做 Windows 适配 |
| 旧任务缩略图与未验收 UI | 保留失败记录；不宣称已完成，不作为核心闭环的阻塞条件 |

## 6. 优先解决的风险与外部依赖

1. **内置 Caelis Windows 交付与服务升级**：已加入公开 v0.72.2 windows-amd64 制品 pin，校验归档、
   EXE 摘要、release commit/版本、许可并独立暂存。隔离 Store 的真实启动/发现/统一 Control 握手通过。
   内置版本随 Bot 更新，显式外部 CLI 继续走公共 update；共享 Store 不复制。完整跨版本 Client/Host
   升级与重新连接仍是发行门，不能由一次握手推断通过。
2. **运行中的内置 EXE 与更新**：实测 Core 实际从哪个路径启动 Host、是否已复制到自有版本目录。
   若 Host 映射着待更新安装目录中的 EXE，优先解决安装布局和公开生命周期的交接。
   优先使用 Core 已公开支持的驻留/升级位置；必要时采用可校验的并存版本布局与延迟清理，
   保持启动入口稳定并验证回滚。必要时允许强制更新，但必须通过 Core 的公开生命周期核实目标、
   保留 Store/原身份/原收据，并验证旧 Client 自动连接新 Host；禁止按进程名全局结束未知进程。
   这个方案必须在实现 updater 前定案，不新造 Bot 的服务权威。
3. **新版 DTW**：rc.9 Windows payload 与私有 worker 均需要完整校验，VC runtime、WebView2、
   Win10 API 下限需实测；Windows owner lock 已完成跨进程持有、死亡释放与重建测试。
   上游 rc.9 已验收不等于 Bot 接入验收。仍遵守上游已知 DPI 几何、模态对话框等待和可能已生效输入
   的限制；使用原收据和 UI 读回，不盲目重试。不能直接拿旧 rc.3 演示说明 rc.9 可用。
4. **普通用户安全与可用性**：主线 POSIX mode、symlink、插件 lock 不能直接套 Windows。
   用原生 ACL/文件锁及受管理的 Skill 投影解决；不要求提升权限、关闭防护或开启开发者模式。
5. **验收资源**：macOS 实机、Win10/Server、多显示器、高 DPI、真实账号授权和公开两版本更新属于独立证据。
   缺设备/账号时记录具体未验收项，不能冒充技术不可能，也不阻塞无关的 Mac 发版。

目前已确认的是本地代码缺陷与缺少原生适配；尚未证明存在无法解决的核心技术障碍。
以上前三项需要 P1 探针给出结论，不能提前承诺“完全没有阻塞”。

## 7. Windows 发布、自启动和自动更新

沿用用户决策：同仓库、共享主线和产品版本，**按平台独立验收、独立选择发布时间**。
Windows 可以跳过某些 Mac 版本，不维护长期平行产品分支。每个已发包必须明确 tag、完整 SHA、
平台/架构/渠道及实际文件摘要；相同版本不替换字节，修复包用新版本。

主线当前 Mac 发布没有 installable preview；Windows preview 是新增的独立渠道。
建议使用不可变的 `vX.Y.Z-windows.preview.N` 标签及独立 workflow（例如 `v0.14.0-windows.preview.1`，
仅为命名示例，不代表现在发布），指向已合入主线的确切提交；嵌入同一全版本与上游基础版本。
实现时补 Windows 自己的 version validator，不能修改现有 Mac tag 校验让预览进入 Sparkle stable。
正式 Windows 发布再选择普通产品 tag；Mac 不等待 Windows 凑齐制品。

- 首批制品：**unsigned NSIS Setup.exe + 免安装 ZIP**，含相同 App、内置 Runtime/插件/许可清单。
  MSIX 留作开发 fixture，不进入发布清单。安装默认当前用户；数据放 AppData，不随卸载误删。
- 自启动默认关闭，注册当前安装的稳定入口与后台启动参数；不改系统 StartupApproved，
  登录启动不弹设置/聊天。ZIP 移动路径后需重新注册，不自动追踪或覆盖别的安装。
- 自动更新默认后台检查并下载，用户确认后在可安全交接时重启安装；Dev 不消费发行 feed。
  Setup 走安装维护，ZIP 在原解压位置更新（如采用版本布局则只原子切换入口）；数据/凭据不在覆盖范围。
- 更新准备暂停新请求并保留原回执，可**强制关闭已证明归属本安装的任务终端观察客户端**，
  更新后重新 attach 原任务。不能要求用户手动关终端才允许更新；关闭观察客户端不触发 Worker 取消。
  活跃/未知任务按实际生命周期安全交接，不能把“终端存在”当作“任务执行中”。
- 优先对齐 Mac 的后台检查/下载、用户确认和空闲交接体验；确有文件占用等困难时允许强制更新。
  强制路径必须明确展示任务的实际结果或未知状态，保留原请求及身份，使用公共 Host 生命周期；
  **旧 Client 自动重连新 Host 是必须通过的验收条件**。覆盖断线中、有未确认请求、Host instance/端口变化、
  旧 Client 再次启动与协议不兼容失败保留。不能把重新打开一个新 Session 当成重连成功。
- 复用本机已备份的 Windows Ed25519 密钥；它用于 update manifest/feed，**不是 Authenticode**，
  不承诺绕过 SmartScreen/组织策略。早期不购买签名服务；受管设备阻止 unsigned 时提供明确说明。
- Windows 的渠道路径、产物名、concurrency、发布收据与环境独立；不得写 Mac appcast、legacy latest.json
  或推动 GitHub Latest 抢占 Mac 下载入口。预览不成为 stable 自动升级目标。
- 构建 job 不接触私钥；main-only `windows-release` 环境中的后续签名/发布 job 消费准确构建摘要。
  更新公钥可提交，私钥通过 secret 注入；沿用现有 R2 最小前缀所有权和 immutable asset 校验思路。
  实现时扩展 publication 的 Windows 适配，不移植旧 Mac candidate gate，也不新建重型人工 token 流程。
- 正式走查包含 clean install、覆盖升级、卸载、非 ASCII 路径、ZIP 只读/移动/磁盘不足、下载中断、
  坏签名/摘要、旧版本回退拒绝、文件占用、更新崩溃恢复、重复启动，以及凭据/历史/任务保留。

签名资料仍位于本机文档的独立备份目录；已有 README 解释用途。本轮不把密钥复制进仓库，
不改 GitHub secrets，不触发 workflow 或上传制品。

## 8. 重建前的基线调查（2026-10-16）

本节记录重建开始前的检查，不是第 9 节的最新结果。基线测试在新主线提交上执行，Windows 11 x64 build 22631、Go 1.26.8、Node 24.21.0；
`GOWORK=off`。没有更改系统权限、真实账号、用户 Runtime Store，也未启动 GUI/真实模型任务。

| 本轮执行 | 结果与边界 |
| --- | --- |
| `npm ci --ignore-scripts --no-audit --no-fund` | 成功，按当前 lockfile 更新本地依赖；没有改锁文件 |
| `npm run build` | TypeScript + Vite 通过；保留已有大 chunk 提示，不是原生 UI 验收 |
| 选定前端/发布/交互 Node contracts | 77/77 通过；文件列表和命令见缺陷清单 |
| `CGO_ENABLED=0 go test -timeout 90s ./...` | 失败，14 个包报失败；包含实际 Windows 缺陷、故意 unsupported 的能力及 POSIX/symlink 测试假设 |
| 针对失败的 6 项合同重跑、无真实凭据的本机探针 | 确认 B01–B06；详见缺陷清单，不把级联失败全部当独立 BUG |
| Mac check/smoke/build、Windows GUI、公开发行升级 | 本轮未执行；不会从前端构建或 unsupported bootstrap 推断通过 |

日志在忽略目录 `.cache/windows-realignment-20261016/`：`frontend.log`、`go-test.log`、
`focused-failures.log`、`native-probe.log`；探针只操作自建 synthetic 数据。持久化结论保存在本方案和缺陷清单中。

后续每项功能的记录统一分为：实现、fixture/contract、真实 Runtime、原生交互、精确发行制品五层。
验收必须填写 source SHA、包摘要、OS/Runtime/插件版本、测试入口和结果，禁止只写“测试通过”。
真实 Computer Use 用专用窗口编辑并保存指定文件，再由独立读回核对内容；通过 Bot UI 发起，
使用原生审批与原插件路径，不能用直接运行 dtw 或 shell 写文件替代演示。

## 9. 当前实施与验收状态（2026-10-17）

代码基于 `95882f1cab67605403d03f8446a128cda8d8eee8` / 0.14.0；本轮实现集中在
`codex/windows-main-realignment`，随 Windows 适配 PR 审查。
当前有可启动的 Windows 开发包和两种本地 preview 制品，**尚未达到公开发布或完整 UI/UX 验收标准**。

| 范围 | 已实现和已验证 | 剩余边界 |
| --- | --- | --- |
| 平台基础 | 私有 ACL、Named Pipe peer SID、PE 校验、Unicode 路径、普通用户 Skill 联接、Codex Job/PID/出生时间、原始回执冷恢复；全量 Go 检查通过 | Mac 原生、Win10/Server 实机未验证 |
| 原生外壳 | 当前 product assembly、保留的聊天/设置/预览窗口、透明轮廓、托盘、多尺寸 ICO、快捷键；补拖动/指针上下文；用户确认跑步/松手、单双击和右键菜单正常 | 缩放、隐藏恢复、悬停表情及多屏 DPI 视觉验收未完成；当前自动化不暴露非激活桌宠窗口 |
| 手动截图 | GDI 获取、选区/标注、贴图、复制/保存/问 Bot 与原回执；首图绘制就绪再显示，默认全屏及提示，确认后紧凑悬浮工具栏；复制/保存/贴图/发送的完成退出与取消保存保留选区实测通过 | 瞬时闪帧、多屏混合 DPI、全部标注工具与深浅主题仍需原生走查；一次识图不代表完整编辑器验收 |
| 任务终端 | typed argv、真实 console stdio、一次性收据、原进程出生时间；更新仅关闭归属确认的观察客户端；独立 Host/其他观察者保留 | 真实 Bot Worker TUI、GPU 终端缩略图、Win10 terminal 走查未完成；身份不明时不用标题猜窗口 |
| 任务卡片 | 开启/切换、锁定/移出/清理，缩略图身份与过期控制；拖动/键盘排序、拖出打开，取消不触发打开；脚边透明三点、悬停展开，冻结排序但继续更新状态 | Mac 堆叠卡片的布局/动画仍需视觉走查和调整 |
| Caelis | 内置 v0.72.2，公开服务发现/握手；真实 v0.72.1→v0.72.2 忙时等待、任务完成、换 Host；同一旧 TUI 进程自动重连已通过 | 此矩阵使用 synthetic 本地模型，不代表 Bot App 安装升级全过程已验收 |
| DTW | rc.9 Windows 732 文件 pin 校验；真实 Caelis Host + Bot MCP 桥的 Skill ready、唯一 exec、禁用撤销通过；修复 CRLF Skill 漏读；真实模型经 Bot UI 操作记事本、另存为并读回通过 | 尚不代表所有桌面应用、多屏 DPI 与复杂模态窗口均已验收 |
| 通知/自启动 | 原生 WinRT Toast/独立 AUMID/固定激活协议；实际 OS 历史新增两项并精确撤销一项通过；HKCU Run 精确所有权合同及 Bot 开关/注册状态实测通过，“不支持”误报已修复 | Toast 点击与冷启动、真实登录/重启未验收 |
| 自动更新 | 后台检查/下载、明确安装按钮、入站冻结、自有终端关闭、成功 shutdown 的 commit 确认、精确 helper 与结果回执；坏签名/占用/未提交退出/ZIP 回滚测试通过；完整 Application 带活跃对话与 Worker 的关闭/重开/换 Host 测试通过 | GUI 安装 App→App 更新、多实例/低磁盘/下载中断完整走查未完成；NSIS 安装失败可能需要手动重装 |
| 制品/CI | unsigned NSIS + ZIP，本地精确制品的安装/卸载/覆盖、Unicode 路径、外来文件保留通过；独立 Windows preview build/publish workflow，签名与构建分离；MSIX 仅开发 fixture | CI 未远端运行，尚未设置环境 Secret、未签署发行清单、未发布；MSIX 新主线未重新打包 |

兼容下限采用 **Windows 10 22H2 x64（build 19045）**，安装和运行入口一致；Server 2022+
仅承诺有交互式桌面/WebView2 的候选支持。当前只在 Windows 11 build 22631 验证；不宣称 Win10、
Server Core、Windows ARM64 或 Linux GUI 已支持。SSH 远程终端与情境行为按授权后置。

### 关键原生证据

- `p10-ui-acceptance.json`：preview.10 实际自启动开关与 Run 命令、F1 默认全屏边框/提示、
  单击/拖动/右键重新选区、Ctrl+C 完成退出并 F3 读回、Ctrl+T 独立贴图、保存取消/成功和发送退出。
  仅发送 430×64 测试文字选区，模型正确读回；保存与发送字节一致（6704 bytes，SHA256
  `f6545c88da3c0c26899d430d720e6503f5b1a1959b820fe038e6da4c9757d84d`）。
  preview.11 补双击选区内/外边界及剪贴板提示，增量原生证据见 `p11-ui-acceptance.json`。
  没有注销/重启桌面，也未逐帧录制进入过渡，不用静态截图宣称零闪帧。
- `p7-ui-acceptance.json`：preview.7 新配置走完模型页（展开/折叠添加连接、继续连接）、
  首次介绍及真实 Caelis 回复；没有弹出 Windows Terminal。原生 F1 打开后仅显示选区提示，
  拖选完成后工具栏以紧凑图标出现在选区下方。原生另存为最终保存、再打开并取消均通过。
  393×75 PNG，13298 bytes，SHA256
  `8b5d4faa784adcee094ef4e9d453d2288d9ef94da9a6181a25f031e5e5ccf1fd`。
  显式取消“同时发送整屏”，提交记录只有一张 selection 图片，字节与保存文件一致；Caelis
  正确读回自建测试文字。未提交其他窗口或整屏内容。瞬时重连闪帧未做逐帧录像。
- `p6-live-demo.json`：用户退出 Snipaste 后，preview.4 通过首次引导启用内置 Caelis v0.72.2，
  复用本机已认证的模型。任务从 Bot 聊天提交，经原生 Guardian 和 DTW rc.9 执行；19 个工具调用
  只包含 Skill、Read、ToolSearch 与 Desktop World exec，没有 shell/文件写入替代界面操作。
  记事本恢复了已有标签；Bot 新建文档、输入两行并通过原生 Save as 保存，未编辑原有文档内容。
  Bot 界面读回后，独立读取 UTF-8 文件确认两行完全匹配，73 bytes，SHA256
  `6b00f88f26642c00b4256aae77e6e6a875c3f61f15edd517f5f1db24a182ffa3`。
  结果文件为 `.cache/windows-realignment-20261016/dtw-rc9-live-20261017.txt`。
  首次设置的模型布局、首条回复连接提示闪帧及后台 Terminal 分别记录为 B17–B19。

- `caelis-tui-matrix.log`：旧 CLI v0.72.1 的两个原 TUI PID **11088、16932**，均在升级前后
  观察到 `BOT_BEFORE_COMPLETE` / `BOT_AFTER_COMPLETE`；未注入新输入、未手动 resume、未换 Session。
  Bot 原 Store/principal/Session 保持，callback 前后各执行一次。Windows 任务终端已改为 Core 受管
  `--interactive --session … --store-dir …`，避免固定旧 Host 端口（B12）。Mac attach 路径未改动。
- 旧 CLI source `1d1f3dbe85fd15ce7d6c3844b8410545ab54d6ff`，archive SHA256
  `82a355d94515e9d4403f8af1ff3a2a1e611aa6d3af0ddf91a69943810491995f`；
  EXE SHA256 `4f9e11cfae2024a40bbeea4070edf9ec101746afe2e611769cf106867e7fd16c`。
  新 CLI 和 DTW 完整 pin 见 `resources/caelis-runtime/release.json`、`resources/desktop-world/release.json`。
- `p4-caelis-dtw.log`：真实 v0.72.2 Host、已安装 rc.9、当前 Bot bridge 的连接/Skill/MCP 撤销通过。
  使用独立 Store 与本机 synthetic HTTP 模型，没有真实账号、桌面操作或外部模型请求。
  初次失败分别是插件 CRLF 解析缺陷（已修复）与 fixture 未持有 resident owner lease（已纠正）。
- `p5-application-update.log`：完整 `app.Application` 使用真实 v0.72.1/0.72.2 和本机合成模型，
  走首次介绍、原生 `bot_delegate` 与一次性人工审批后，同时保持 Bot 对话和 Worker 请求未完成。
  `PrepareUpdate/Close` 冻结新输入但不取消原请求；重开同一应用数据后没有重复派发，原 Worker
  Session/Store 保留，任务完成后新 Host 接替。随后用新 Host 身份核对终端目标，并完成新的 Bot 输入。
  此测试显式选择手动原生审批，不模拟 Guardian，不操作 GUI 或安装器，也不调用外部模型服务。
- `p5-assembly-fixed.log`：移除 12 处过时的 Windows IPC 整项跳过，应用、Bot 和本地 IPC 测试通过。
  `p5-runtime-native-cli.log`：CLI 检测/更新/服务启动的真实 argv、凭据隔离与受限错误提示通过，
  原 shell fixture 换为可执行的跨平台 Go fixture。明确的外部实机/二进制 opt-in 仍单独保留。
- P2 截图贴图为 **587×128 PNG**，SHA256
  `fbdf2e840218ed2808040266c222a93d4496158bd56d4b08cbbdfda8c176c5de`。
  此次未发给模型；Snipaste 遮罩影响原图，不能称为完整截图视觉验收。
- `p3-notification-native.log`：真实 WinRT 通知历史和单项撤销；`p4-update-receipts.log`：原生更新进程交接、
  未提交关闭拒绝安装、读句柄与原子回执替换冲突恢复（B15）；
  `p4-nsis-4-acceptance.log` / `p4-zip-4-acceptance.log`：下面精确本地制品的生命周期。

### 桌宠交互事件核对（2026-10-17）

Mac 和 Windows 复用角色渲染、`DragMotion`、表情、活动状态与设置界面；指针事件、窗口移动、
焦点、菜单和显示生命周期分别由 AppKit/Win32 提供。前端相同不等于这些原生事件自动接通。
此次补齐 Windows `pet-context`，没有修改 Mac 原生事件处理或共享跑步算法。

| 事件 | Windows 路径与核对结果 | 证据边界 |
| --- | --- | --- |
| 左键单击 | 原生 caption 按下/释放；等待系统双击间隔；`pet-touch` + `Activate`；切换聊天 | 共享聊天切换测试通过；用户确认 preview.7 单击开关聊天正常 |
| 左键双击 | `WM_NCLBUTTONDBLCLK` 取消待发单击，再 `pet-touch` + `RecallWindows` | 用户确认 preview.7 双击唤回聊天且不误关闭 |
| 右键 | 清除单击定时器；宠物菜单仅聊天/当前可见性；菜单进入/退出发布 busy 状态 | 用户确认 preview.7 菜单内容和点外部收起正常 |
| 按下与拖动 | `WM_NCLBUTTONDOWN` 发布 pressing；`WM_MOVING` 加 125 ms 采样发布 dragging、鼠标/角色坐标 | 原生状态机、共享动画 8 Hz→30 fps、左右方向与负坐标合同通过；用户确认 preview.7 左右拖动跑步正常 |
| 拖动停止/松手/取消 | 静止采样衰减；结束清除 pressing/dragging；拖回原点也不当成点击；恢复气泡与任务入口 | 状态及动画清理测试通过；用户确认 preview.7 松手恢复正常；取消和辅助窗口恢复仍待原生目视 |
| 悬停/离开 | `WindowFromPoint` + `GA_ROOT` 识别实际指针命中的宠物；发布 hovering，沿用共享表情/注视 | 支持 WebView2 子 HWND；不把其他覆盖窗口当成悬停；原生目视待确认 |
| 隐藏/显示/销毁 | 取消待发点击和拖动；发布 visibility；隐藏不取消工作；销毁关闭原生定时器 | 生命周期与可见性合同通过；隐藏/重新显示后的原生目视待确认 |
| 缩放/位置恢复 | 沿用设置中的 65%–160%、原生缩放/DPI mask、持久化位置；无新增滚轮手势 | 合同通过；preview.9 实际设置滑块 65%→160%→100% 与原生持久化一致，靠近屏幕边界时保持窗体在屏内；混合 DPI 待实机 |
| 工作状态/显式动作/减少动态效果 | `pet-activity` / `pet-gesture` / visibility 使用共享渲染；减少动态效果清理跑步姿态 | 模型资源 smoke 与动画清理合同通过；全部视觉状态仍需走查 |

自动化工具未返回不激活的桌宠窗口作为可操作目标，因此没有通过模拟消息或伪造句柄把合同测试
记成真人输入。用户于 2026-10-17 回答“跑步和松手恢复都正常”及“三项都正常”，
对应上述 preview.7 跑步、松手、单击、双击和右键。其余未验证项保留，用户反馈与自动化证据分开记录。

### 已生成的本地制品

这些制品来自未提交的源树，`dirty=true / qualified=false / unsigned`，不可签署或上传公开 feed。
最新构建 **0.14.0-windows.preview.14** 包含模型页排列、截图紧凑工具栏、原生保存取消、后台 MCP
隐藏启动及桌宠指针/拖动状态修复。preview.7 完成首次引导与截图 UI 验收，preview.8/.9
补 WebView2 子窗口悬停识别及隐藏后气泡恢复；preview.10/.11 修复自启动文案、截图首次显示、
默认全屏与完成退出，增加常用快捷键和双击边界。分版本实机证据与未验证项见上文。
preview.12–.14 分离模型摘要与 Runtime 探测、复用模型参数、按表面拆包；截图改用快速 PNG、
合成器同步、禁用自身窗口过渡并预加载一个空 WebView；F3 直接打开无标题栏置顶贴图，按需右键操作。
本机 F1 到前端绘制确认采样从 717.5ms 降为 316.1ms，F3 44.6ms；详见问题记录 B27/B28。
这些数字不是逐帧视觉验收。此轮 UI 工具无法取得前台窗口进程 ID，模型页真实切换、贴图拖动/缩放/
菜单和截图无黑帧/无动画仍待实机验收。Windows 检查、smoke、构建及增量测试记录保留在
`.cache/windows-realignment-20261016/p12-*`、`p13-*`、`p14-*`，Mac 原生验收未重做。
旧 preview 制品保留原字节。下面 preview.4 的安装/升级通过不自动覆盖新候选制品。

| 0.14.0-windows.preview.14 本地制品（dist/releases，未发布/未做精确安装升级验收） | SHA256 |
| --- | --- |
| `Caelis-Bot-0.14.0-windows.preview.14-windows-amd64-preview-local-Setup.exe` | `ba70302e04acb9ae0e434f88d5345ac8a5fbe1035955cad7eb3e99c93ac925db` |
| `Caelis-Bot-0.14.0-windows.preview.14-windows-amd64-preview-local.zip` | `f6aaa1eb9674556f157c8dd697619ddc7e544e01627d2ec9cfe05cac5de09257` |

preview.14 EXE SHA256 `d3811bcb5b1418de40072f8a04effa02a4ed3f3cf9bd87ee7707f547eef62a64`，
目录 `dist/windows-realignment-preview-14`，复用独立 `p7-preview-profile`。
本轮没有绕过安装归属校验改写 Run 注册项；开机登录仍指向 preview.11 测试目录，用户可通过 Bot
开关重新注册最终测试目录。这里的候选切换不等于生产环境原目录自动更新。

| 0.14.0-windows.preview.11 本地制品（dist/releases，尚未做精确安装升级验收） | SHA256 |
| --- | --- |
| `Caelis-Bot-0.14.0-windows.preview.11-windows-amd64-preview-local-Setup.exe` | `152e43c54c21c1408fbef0abda7f176222708aa1933c843e06d763f6dcb80e4e` |
| `Caelis-Bot-0.14.0-windows.preview.11-windows-amd64-preview-local.zip` | `b18b9bac092d2af03296e3dc1422a9133b3ae986409e7b27004617dceca2935b` |

preview.11 EXE SHA256 `8bf13fbc9c3007110918fa14a1981bbc9f351a22d2a71eeab05eb42bbbea9416`。
本地构建目录为 `dist/windows-realignment-preview-11`；运行复用独立 `p7-preview-profile`，不迁移用户正式数据。
preview.9 原生角色预览/缩放/持久化已通过；preview.11 设置显示正确版本，原自启动开启意图
通过 Bot 开关迁移到当前候选目录，Run 命令核对一致。实际登录触发仍待单独验收。

| 0.14.0-windows.preview.4 本地制品（dist/releases） | SHA256 |
| --- | --- |
| `Caelis-Bot-0.14.0-windows.preview.4-windows-amd64-preview-local-Setup.exe` | `3133771407803da5137730897ed6b6f96b8348e1268ff8829591401651e44379` |
| `Caelis-Bot-0.14.0-windows.preview.4-windows-amd64-preview-local.zip` | `c8dee229d0b9ff230cbe99dbb4a6bad3eac20e81b35f778b33c920a81b9d5a37` |

EXE SHA256 `9d7e67c9b975a3d61124f5318c1854d00f3c6b7254f6621082f6061b0b1a10c4`。
Setup 在无既有安装/注册的当前用户目录实际升级和卸载，保留外来文件；测试后已移除安装 fixture。
ZIP 752 文件清单、中文解压路径、实际 PE 映射占用拒绝、清单内替换和数据保留通过。
两项使用临时测试签名密钥；未读取发行私钥，也未签署公开更新清单。

精确 ZIP 从 `.cache/windows-realignment-20261016/p4-zip-4-lifecycle/免安装 中文` 启动，
数据独立放在 `p4-preview-profile`。此前 Snipaste 全屏截图层阻挡操作；2026-10-17 用户退出
Snipaste 后已恢复，完成首次引导、真实模型回复与上述 Computer Use 演示。后续 preview.7
已补图片发送和截图另存为验收；Task Dock 与完整视觉走查仍不能用记事本演示替代。

### 检查入口与证据维护

Windows 使用 `script/check-windows.ps1`、`script/smoke-windows.ps1`、`script/build-windows.ps1`；
Mac `make check/smoke/build` 保留。最新 `p10-check.log` 全量 Go/前端/i18n/协议/资源/Node 合同通过，
包含 Windows 应用/插件/Skill、任务排序、截图工具栏边缘定位和桌宠拖动状态合同；
新增自启动状态、截图就绪代次、导出完成/取消/接收竞争及快捷键合同；`p11-focused.log`
复验 i18n 和增量 Node 合同，`p11-build.log` 记录当前可运行制品构建（含 TypeScript 检查）。
没有修改 Mac 原生源文件或共享动画算法；共享自启动前端保留 Mac 的 on/off/needsApproval 状态。
`p5-darwin-test-build.log` 另确认改造后的 CLI fixture 在 Darwin arm64 可编译，没有执行 Mac 实机测试。
`p10-smoke.log` 实际 Codex initialize 与 Three.js/GLB smoke 通过；`p10-darwin-build.log` 仅证明
Darwin arm64 CGO=0 交叉构建，不证明 AppKit、签名、权限或视觉效果。
发布 workflow 通过本地 actionlint，Windows PowerShell 脚本解析通过；远端 CI 未运行。
日志在 `.cache/windows-realignment-20261016/`，不会记录凭据或完整真实聊天。

Bot Skill 已更新 DTW rc.9 的单 exec、原收据和条件限制指导。其他改动属于原生宿主、窗口、安装和更新，
没有增加 Bot 工具或改变任务授权，因此不向 Bot Skill 塞入发布架构或开发操作。
后续应用更新测试只加强验证，沿用原生 `bot_delegate` 与审批合同，也无需新增 Bot Skill 指令。
本轮模型页、自启动呈现、截图选区/完成交互与桌宠输入修复不改变 Bot 工具、授权或图片接收合同，
已核对 `bot-core/references/screen-input.md`，无需新增 Bot Skill 指令。
详细 CI 和密钥接入见 [Windows preview 发布](windows-release.md)。
