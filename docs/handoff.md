# 继续开发

## 2026-09-28 空闲 Dream 与延迟上下文交接

- Bot 主对话空闲 15 分钟后执行一次 `caelis-dream`，写指定 HANDOFF 并安静展示一句 recap；
  只在下一次用户消息/截图输入时切换内部 Session，Provider 原生 Compact 保持原状。
- 每个 Session 首次输入携带 MEMORY 和非空 HANDOFF；原生接受记录持久保存后消费交接，
  未知/拒绝不删除，文件已编辑则保留。用户返回只中断维护 Turn，旧聊天与任务继续保留。
- 两种后端均接入；技能保持 Bot 范围与渐进加载，普通工作增加简短进度汇报要求。
- `make check`、`make smoke`、`make build`、受影响包 race 通过；安装版 Codex/Caelis + 本机
  合成 provider 验证原生技能读取、HANDOFF 写入、延迟切换、注入/消费、worker 隔离与既有功能。
- 未做新增 GUI 视觉验收或真实模型长期摘要/缓存收益测量。改动尚未发布。
  [实现边界与时序图](design/context-lifecycle-v1.md)。

## 2026-09-27 截图交互完善（v0.4.0 候选）

- 使用图标工具栏、调色板、连续粗细滑杆；备注与 Ask Bot 组成同一发送行。选区完成默认聚焦备注，
  Enter 发送，F1/Esc 退出，Space 从画布返回备注。IME 确认和文字标注提交不发送，选中文字时 Cmd+C 复制文字。
- 全局完整屏幕开关默认开启；聊天展示可跨重启查看的截图卡片，点击查看/复制；展示文件参与已有 30 天附件清理。
- 原始截图回执和模型输入仍归 Runtime；普通 composer 草稿不被消耗。Bot skill 补充全局上下文和历史预览语义。
- 本轮验证记录见准备状态；保持真实模型、物理输入法和多屏硬件验收边界。

## 2026-09-27 屏幕快捷输入（本地，未发布）

- Issue #26 首版：F1 选区/标注/复制/保存、F3 贴图、整屏上下文 + 选区 Ask Bot；操作与数据边界见
  [screen input v1](design/screen-input-v1.md)。普通聊天草稿隔离，拒绝保留，未知回执不重发。
- Codex 按当前常驻模型的 inputModalities 门控；Caelis 通过可选 application-model-capabilities-v1
  读取当前应用模型的明确图片能力，展示和发送前均复核；老 Host 仅禁用 Ask Bot。截图功能要求 macOS 14+；Windows 只保留降级契约。
- Bot skill 已新增 Screen input 参考：按需读取、参考截图理解意图、通过既有 Notebook 学习明确反馈，
  不硬编码 Reddit 翻译，不将截图文本当作执行授权。
- 独立验收入口：`script/build_and_run.sh --capture-preview`；真实模型问答、TCC 授权和多屏硬件验收仍单列。

## 2026-09-27 工作可观察性与消息气泡（本地，未发布）

- 气泡与聊天等待区显示当前工具活动：读取/浏览/搜索/编辑文件、网络搜索、网页读取、命令、协作等。
  信息来自两种 Runtime 的原生事件；并行完成、稀疏更新、当前回合和终态均有回归覆盖。
  子任务内部步骤仍通过原有任务/终端入口观察；未知工具保留通用提示，不根据助手文字猜测。
- 气泡复用安全 Markdown，取消最新助手正文的 1000 字截断。悬浮展开、移开收起；原生可用屏幕
  限制最大高度，全文在内部滚动，链接/复制不会误触打开聊天。审批仍需显式点击。
- `./script/build_and_run.sh --bubble-preview` 是独立合成 WebKit/AppKit 验收入口，不加载个人数据、
  不调用模型、不重启日常 Bot。使用生产前端构建和共享布局函数，可切换活动/审批/顶部场景并保存截图。
- Bot skill 无需更新：本轮只改变已有原生事实和消息的呈现，没有新增工具、模型工作流程或恢复权限。
  具体验证与尚未覆盖的实机范围见准备记录。

## 2026-09-27 任务卡片与通用终端管理（未发布）

- PR #28 复查修复：启动目录/脚本、终端偏好和自定义进程创建失败，在确认未提交启动后
  不再永久锁住卡片；修复配置后可再次点击。已提交但结果未知仍保留防重复保护。
  Ghostty 在 PID 返回前即持有公共实例句柄，取消后迟到的 LaunchServices 回调仍能补齐身份；
  待定启动不能误报已关闭，后续沿用原实例重连或正常关闭，已撤销脚本不能执行。
  Go 回归覆盖启动前失败修复、未知结果不重试、PID 零取消后的复用/关闭；原生夹具在自有
  进程上注入迟到回调验证句柄生命周期，并检查未安装应用的未提交状态，不冒充真实 Ghostty
  时序或视觉验收。本轮 `make check`、`make smoke`、`make build` 和 taskterminal/desktop race
  均通过，日志 `.cache/pr28-p2-{check,smoke,build,race,native}.log`；目录修复的回归在修复前
  失败，见 `.cache/pr28-p2-before.log`。Bot task skill 已同步重试与取消边界。
- 脚边透明三点展开透明堆叠卡片；没有数量、Runtime 标签、滚动条、右键菜单或批量清理按钮。
  卡片按 visibleFrame 自适应横向/纵向、向上/向下展开；数量增加时压缩间距，悬停浮到前方。
  支持独立可配置快捷键、悬停锁/关闭、上滑关闭、拖动排序，以及桌面拖出打开。
- 清单没有展示数量上限；无手动排序时活跃任务优先，新执行自动显示并前置，普通刷新保留手动移除。
  未锁定完成项保留 30 分钟，锁定项常驻。清单操作不停止 Worker 或删除历史。
  默认执行并发从 3 调整为 6，保留用户已保存的设置。
- 管理对象是 Bot 启动的专属终端应用实例及其整组窗口。保存系统返回的 PID/出生标识，
  不按标题、TTY 或窗口数量认领实例。标准文档打开、置前、正常 Quit 为基本路径；
  hide/show、快照与定位是可选能力。原精确 AX 窗口控制已移除，不依赖辅助功能授权。
  Ghostty 原生创建仅为可选启动增强；未知执行或权限拒绝不改走另一条启动路径。
- TUI 已退出或旧打开请求已撤销时，优先向原实例发送标准文档事件重连。终端决定窗口/标签页，
  不向旧 shell 输入命令，不自动退出旧窗口。原客户端已退出、原实例恰在复用时自行结束时，
  仅在旧脚本撤销成功且原应用确认退出后，同次点击允许一次新建。闲置自动清理尚未实现。
- 打开事件回执与脚本执行回执分离：取消或应用退出结束等待；文档处理成功不算执行成功。
  回复后三秒内未执行则原子撤销旧脚本，恢复可重试状态。原生确认仍在等待时不叠加请求，
  迟到脚本不能重复执行。正常关闭等待标准 Quit 回执及实际退出；取消保留终端和卡片。
- 本地等待/失败提醒复用现有消息气泡，不新建提示面板，不写聊天记录或触发模型。
  审批与恢复关注优先；旧提示清理不能覆盖新提示。可选快照不阻塞终端控制。
- 权限页与可跳过的首次引导按权限类型说明用途，状态来自当前进程。启用先请求原生授权，
  明确需要系统设置后才跳转，不代替用户确认、不乐观改状态。屏幕权限申请只请求内容权限，
  丢弃元数据；正常预览只在已有权限时采集单张本地图片，不录屏、不上传。多窗口歧义保留占位。
  显式修复只允许经确认重置当前 bundle 的单一权限类别，不能选择性移除旧版本授权。
- 本机用户显式选择稳定 Apple Development 身份，配置指纹文件被 Git 忽略；CI 仍 ad-hoc，
  发行仍需 Developer ID、公证与 Gatekeeper。详见[开发签名](development-signing.md)。
- Go 回归覆盖取消重试、并发点击、连续十次同实例复用、未知状态保留、关闭确认和过期/锁定。
  AppKit 夹具覆盖密集堆叠、边缘布局、悬停命中、排序和提示；原生双窗口实例验证归属隔离与切换。
  收尾审查还复现并修复观察者替换与清单刷新的 data race，删除已无调用者的旧启动参数路径。
- 收尾 `make check`、`make smoke`、tasks/taskterminal/desktop race、开发签名 `make build` 均通过。
  日志 `.cache/review-final-{check,smoke,race,build}.log`；观察者竞态的修复前/后日志为
  `.cache/review-observer-{before,after}.log`，修复前 race 失败，修复后连续五次通过。
  本机真实终端使用合成脚本及 OS 状态，没有加载 Bot 数据、读取用户终端内容或调用模型。
  iTerm2 取消打开后同实例重试通过（18:03–18:04，`.cache/terminal-reuse-consent-e2e.log`）。
  Terminal/iTerm2 两轮控制与客户端退出复用通过（18:06–18:07，`.cache/terminal-reuse-e2e.log` 对应段）；
  该混合日志最终 FAIL 为 Ghostty 自行退出撞上复用，补齐公共退出边界后 Ghostty 单独重跑通过
  （18:10，`.cache/terminal-reuse-ghostty-e2e.log`）。未将失败的混合运行称为全套通过。
  Terminal 关闭取消/确认由用户手动操作通过（17:14–17:15，`.cache/client-recovery-consent-e2e.log`）。
- 不承诺任意终端单窗口启动、Dock 隐藏、手动最小化后全部窗口恢复、跨 Spaces/多屏及物理触控板方向。
  当前实例路径的实际快照与视觉动画仍有人工验收边界；历史精确窗口路径的成功截图不替代它。
  自定义 argv 无可证明 GUI 归属时仅支持打开；Windows 只保留接口，没有启用桌面适配。
- Bot 英文 task skill 已同步清单维护、实例复用、取消恢复和平台权限边界。
  细节见[任务委派](task-delegation.md)、[状态机](design/task-terminal-window-state-machine.md)
  和[实例复用与清理边界](design/terminal-instance-retention.md)。

当前主线是 Caelis Bot 的产品功能和 macOS 正式版质量，先读 `product.md`、`architecture.md`、
`roadmap.md`。产品已具备真实 Codex 工作闭环，不从早期工具链试验重新开始。

2026-09-25 审批与工作任务修复（PR #25 已合并，尚未发布）：

- Caelis 实时审批事件驱动精确 head 核对；迟到读取不会覆盖新的审批，流式文字不会饿死核对。
- 主 Bot 已结束回复后，异步审批命令仍有持久化跟进：确认终态再发送一次完成通知；未知不重发。
- 迟到命令跟进按可选能力 `application-terminal-observation-v1` 启用：仅应用自有会话的终端只读观察，
  保留应用来源，并纠正终端生产者已退出但 Task 快照仍为 running 的观察结果。当前正式
  v0.62.0 不具备该能力，但仍可正常连接并使用其他功能。Caelis 修复独立提 PR，不单独发 release，
  不阻塞 Bot 交付；普通会话的自动续跑仍由 harness 决定，观察接口不隐式发起模型回合。
  Caelis 已提交 [PR #78](https://github.com/caelis-labs/caelis/pull/78)，Bot 修复已提交 [PR #25](https://github.com/caelis-labs/caelis-bot/pull/25)。
- `bot_task_start.workspace` 支持指定已有绝对目录，两 Runtime 都支持。默认仍分配私有目录。
- 新任务有空位自动 pin，满额保留已有项；稳定 ID 重试保留用户 unpin。英文 skill 已同步。
- 终端小球可绑定具体窗口实现收起/恢复，但现有启动回执没有窗口身份，本轮只完成评估。
  审批长命令气泡布局依然是 APPROVAL-01 待办。
- 证据与边界见[修复清单](bugfix-checklist.md)、[任务委派](task-delegation.md)。

2026-09-25 主动关怀审查修复，尚未发布：

- 关怀状态加载失败只停用关怀，保留原始文件并报告错误，个人空间、聊天和普通提醒继续启动。
- 未知提醒先按请求 ID 核对原生保留回执，确认写盘后再放行新关怀；恢复写盘失败仍保持未知。
- 新增启动故障与调度交错回归；英文关怀 Skill 补充存储故障处理，详见[主动关怀](proactive-care.md)。

2026-09-25 任务清单与终端偏好历史检查点（八项策略已被上方实现替代）：

- 运行数量默认 3、可配置，历史不设任务数量上限；Bot 可分页搜索并 pin/unpin，脚边最多 8 项。
  移除不取消任务或删除历史；新任务有空位自动固定。英文任务 Skill 已同步检索与召回方法。
- 设置自动保存，只列已安装的受支持终端，卸载后回退系统默认；高级自定义命令使用独立
  `{script}` 参数并由用户自行验证。发布界面没有测试、Reload 或 Save 按钮。
- 终端由用户点击打开，宿主异步等待执行回执、显示等待状态并提供取消入口，不阻塞 Bot。
  确认超时保留“不确定”语义；迟到确认被撤销。模型未增加打开/关闭终端工具。
- 全套检查、相关 race、原生 AppKit 和浏览器组件验证已完成。终端启动参数已有实测；
  最后一轮回执验收中 iTerm2 等待手动确认超时，不能宣称该轮确认成功。
  具体范围与后续实机边界见[验证状态](preparation-status.md)及[任务委派](task-delegation.md)。

2026-09-25 可编程主动关怀已从 POC 接入生产路径，尚未发布：

- `bot_care` 发现来源、试算 CEL、保存/删除规则；日期窗口、使用时长和前台应用切换是首批来源。
- 条件本地执行，持久化排队后沿用两种 Runtime 的后台激活、原生授权、静默结果和回执核对。
  已知解锁、空闲、冷却、有效期与总预算决定派发；未知回执和存储失败不自动重发。
- 来源注册和宿主 JSON 发布接口支持后续连接器、gh/脚本采集器；当前没有任意命令执行器或
  自动订阅。Skill 已新增按需加载的范围、限制和示例。详见[主动关怀](proactive-care.md)。
- 单元/原生 Runtime 合成验证及本机元数据检查见[验证状态](preparation-status.md)；
  物理锁屏/睡眠切换和真实模型关怀质量仍需实机复验。

2026-09-25 聊天与 Bot 核心 skill 已本地实现，尚未发布：

- CHAT-01：立即显示待发送气泡，以原生请求标识去重；拒绝/未知保留状态和草稿，不自动重发。
- CHAT-02：有订阅的子任务由原生回合事件维护终态，迟到活动通知不再重置忙碌。
- 聊天和快捷输入均按 canSend/canSteer 支持运行中补充；委派正文原样传递。
- Bot 英文核心 skill 使用 metadata → 正文 → 条件 references；旧固定角色/发现文案已迁入，
  Codex/Caelis 的应用 instructions 只提供局部技能目录。详见[加载契约](design/bot-core-skill-v1.md)。
- 任务气泡先设置目标窗口尺寸，再安装内容；活跃任务显示 loading，悬停顺序冻结但状态实时更新。
- 回归、真实 Runtime + 本机合成 provider、原生 AppKit fixture 和真实 React 组件检查的
  具体范围见[修复清单](bugfix-checklist.md)。真实模型长期自主加载/压缩恢复与完整桌面交互
  不能由合成测试替代。未接管或改变原生子代理的委派路线。

2026-09-24 原生任务气泡已接入：脚边收起、相同圆球、悬停原始 prompt、点击外部终端。
Codex 使用标准共享 Unix App Server，Bot 保持订阅并观察终端用户的新回合，不再长期轮询 Worker。
完整检查、smoke、构建、相关 race 与安装版隔离协议验收通过；原生浮动气泡的 hover、截断和
点击 Terminal GUI 仍需人工实机复验，不能把此前 POC 截图当成新构建的完整视觉证明。
Caelis 原生 Worker 也已接入相同入口，使用现有 Session 和用户凭据文件；主动关怀随后接入，见上方更新。详见[任务委派](task-delegation.md)及[验证状态](preparation-status.md)。

2026-09-24 设置与窗口体验已在本地整理：连接页去重、模型统一保存与草稿保留、
页面切换回到顶部；Dock 随聊天/设置的打开状态显示，关闭页面不退出应用。
当前成品由 `resources/character-pack.json` 固定，DMG 使用紧凑拖拽布局；打包额外依赖项目内固定版本 Python 工具环境。
完整检查、smoke、构建与 DMG 验证通过，已检查原生设置和 Finder 安装窗口；
物理快捷键与 Dock 点击、跨 Spaces 和完整 VoiceOver 仍待人工复验。详见 [验证状态](preparation-status.md)。
改动尚未发布；不要把本地 ad-hoc 开发镜像作为已公证版本分发。

2026-09-24 已本地修复后端通知误报：按方法和 item 类型消费 Codex 事件，非阻塞组件错误与
重试只写入可滚动清理的私有诊断日志；真正任务失败及关键生命周期不确定仍提示用户。
原生启动恢复用户 shell 导出环境，Bot/worker 及其 MCP/工具继承同一本机工具环境，
保留任务工作目录、原生权限与秘书私有能力边界；普通连接不重启共享 Caelis Host；显式更新通过原生生命周期启用新版。
检查、smoke、构建、相关 race 测试及真实 Context7 无模型启动验证通过；
新构建的原生窗口受正在运行的正式版单实例限制，未完成 GUI 验收。详见
[任务委派](task-delegation.md)与 [验证状态](preparation-status.md)。本轮尚未发布。

2026-09-24 更新切片：Sparkle 原生更新、忙碌时延后安装和公证后 R2 同步已本地接入。
R2 使用主仓库相同桶/域名下独立的 `caelis-bot/` 前缀，只保留最新正式版；同步失败
通过 `sync-r2.yml` 重试，不重新公证。Sparkle 公私钥和 R2 已配置为限定仓库可用的组织
变量/秘密；Sparkle 身份保留在本机钥匙串，新 R2 凭据于 2027-08-24 到期，见 [release.md](release.md)。
下一发行仍需验证公开签名更新及跨版本重启。当前旧版需手动
升级一次；本地 fixture、完整检查和实机/公证尚缺的验证范围见 [preparation-status.md](preparation-status.md)。

最新切片是[秘书与专业任务分离](task-delegation.md)：专业工作经 Bot 自有任务接口进入
独立受管目录，完成后有限唤醒秘书汇报；不依赖 Codex App 的私有 IPC。已有项目/worktree
授权与其他 App 任务的显式接管仍待后续实现，不能当成当前能力。

2026-09-23 当前切片是统一 Bot 产品层：`internal/bot` 持有身份、工具与提醒，
`internal/tasks` 持有产品目录、账本与汇报；Codex adapter 仅映射 native 执行/审批/回执。
`bot.json` 跨 Runtime 保留身份，计划和在途工作仍绑定原 Runtime。Notebook/Memory 的本地增量见文末。

Caelis 最低能力基线保持 v0.62.0；公开协议随 Core 模型能力扩展更新，精确提交与哈希见 `protocol/caelis/manifest.json`。
共享原生 Worker、steering、模型/Team 配置和连接向导通过公开 HTTP/SSE 接入。
Runtime 更新现在区分安装版本与服务版本，依次启用服务、验证协议、重新连接 Bot；
已有新版程序可直接启用。任务忙碌或状态未知时拒绝替换，具体共享客户端边界见
[接入契约](caelis-integration.md)。历史真实模型证据见[正式版联调报告](caelis-release-acceptance.md)，
新增路径和仍待验证范围以[实现与验证状态](preparation-status.md)为准。

工作模型与 Bot 模型解耦；Agent team 是 Runtime 的共享配置，与 TUI `/team` 一致。
Bot 不按任务发现或指定另一套 team。详见[任务委派](task-delegation.md)。

Runtime Setup、头像图标、显式切换、AppKit 点击/拖动与窗口唤回等已有功能保留。
2026-09-24 [v0.1.0 正式版](https://github.com/caelis-labs/caelis-bot/releases/tag/v0.1.0)已发布。
Developer ID 改动已恢复并提交；公开 DMG 及其 App 的签名、公证票据、Gatekeeper、
校验和与源码提交已在本机重新下载验证。原 stash 仅作备份，不能再次当作待实施改动。
CI 单次公证等待 60 分钟，打包作业 150 分钟；仍在处理时保留签名原件及提交 ID，
跳过发布，可使用 `resume_run_id` 继续。详见[发行流程](release.md)与 preparation-status。

[能力契约](backend-contract.md)定义产品装配和 provider 分支，
[平台计划](backend-platform-plan.md)保留 Windows 落点与历史审查。Windows 原生实现仍在
macOS 完整发行后进行；不能把共享核心交叉编译当作 Windows 桌面或 ACL 验收。

2026-09-23 新增本地内容包 v1：共享原生校验/注册表、外观设置、独立头像选择、
基础 GLB 渲染与离线制作工具。社区接入请从 [内容包开发指南](content-packs.md) 开始；
[扩展边界](asset-extensions-plan.md) 定义预置内容与第三方导入的职责及新能力接入规则。
内容包不改变 Runtime、对话身份或任务权限，不能携带执行代码。

## 工作范围

- 本仓库：原生生命周期、连接适配、聊天/审批/附件、桌面行为、成品渲染、发行。
- 组织私有资产仓库：建模、衣服/饰品、新角色、作者工具、原始来源与视觉制作验收。
- 当前默认成品、哈希与许可由 `resources/character-pack.json` 固定，详见 `character-assets.md`。
- 制作历史已从公开根移出。不要把源目录、旧 Git 对象、参考图或制作截图重新加入主库。

## 修改后验证

`make check` 检查成品边界、运行时动作与 Go 行为；`make smoke` 只握手本地 Codex 并验证成品；
`make build` 构建 ad-hoc 签名应用。需要观察原生窗口时用 `script/build_and_run.sh --verify`。
没有本地 Codex 时可以单独运行 `npm run smoke:assets`；CI 不安装或携带 Codex。
`make smoke-caelis` 使用显式提供的固定 Caelis 二进制、临时 Store 与合成模型，
验证新通用协议和 macOS 原生工具，不代表真实模型或原生 UI 已验收。
`make smoke-caelis-live` 使用用户已配置的隔离 Host 进行真实模型调用；MiMo v2.6 Flash
与 GPT-6 Luna 已在新协议通过；需显式提供隔离 store/model，Fast 与自管 Host 参数见实测报告，不在日常 CI 中执行。
测试通过不代表所有角度无穿模、双屏/Spaces/全屏都已验收。

继续优先处理正式版可用性、轻量桌面行为和真实演示；角色资产经私库更新 PR 独立迭代。
Developer ID 和公证已接入正式发行；Windows 原生适配仍未实现。保持历史记录和当轮验证的区别。

## 2026-09-23 Notebook / Memory 收敛

统一产品层检查点为 `b449aad`，Notebook 检查点为 `bca03ce`。Notebook 为普通 Markdown 目录，
应用只维护 INDEX 和当天目录，MEMORY 与日期笔记由用户直接编辑或 Bot 用文件工具维护。
`internal/botskills/skills/caelis-bot-memory/SKILL.md` 只注入秘书；worker/普通 Session 不继承。
名字必填、描述可选的表单仅首次初始化出现，转成可见用户消息，由 Bot 写 MEMORY；
投递前保留待发正文，接受后只留状态，结果不明不自动重发。

旧「记忆与笔记」UI、专用笔记 CRUD 与 Facts 写入口已移除，已有数据一次性复制为日期笔记，
原数据保留。嵌入 Memory v0.6.1 的线索工具支持 recall/remember/correct/forget。
路径、边界和验收见 [个人空间](personal-memory.md)。该历史检查点中 Developer ID 尚在独立 stash，现已恢复发行。
Caelis 新协议已完成真实模型 Notebook 读写、INDEX 更新和重启后回读；跨 Runtime 资料迁移未在本轮重验。

## Bilingual interface work

English and Simplified Chinese use shared catalogs, a host-owned language preference
and live surface/menu updates. Locale changes preserve drafts, approval identity and
connection progress. Coverage, external-content boundaries and release acceptance
are maintained in [Interface languages](i18n.md). The next formal version is v0.2.0.
