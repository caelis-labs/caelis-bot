# Runtime 首次连接与设置：本地实现交付

2026-10-13 至 14。工作树 `research/runtime-onboarding`，起点 `origin/main` 为 `393b29f5406df739682280c1c87652bbe773c52b`。此前的 [交互草稿](interaction-draft.md) 是评审时的研究快照，其“未正式实现”说明不再描述本交付。当前交付只在此 worktree 和 `Caelis Bot Research` 开发包中；未推送、开 PR、合并或发布。

## 已实现

- 首次先选“默认”或“使用 ChatGPT”，两项同等呈现、不显示推荐标记。内部选择值仍分别为 `caelis` / `codex`，没有改动默认选择或 ChatGPT 路线；后者继续使用 Codex。缺 Runtime 时自动开始官方准备，安装中保持“正在进行准备工作”，Caelis 归档有实际已收字节、总量、百分比、五秒滚动速率；没有可靠总量时不伪造百分比。下载无绝对五分钟限时，有连接和无数据保护。安装完成仍须通过账号或至少一个可聊天模型的配置检查。首次选择、配置和进入对话均在当前 Bot 进程完成，不要求重启。
- 默认 CLI、认证与 Store 跟用户终端共用，Codex 默认 `~/.codex`，Caelis 默认 `~/.caelis`。不内置 Runtime、不默认建立第二份 Store。官方脚本安装后重新发现路径、版本和握手；已识别 Homebrew formula/cask 与 npm 全局来源沿原渠道更新。来源不明或收据不一致时拒绝静默并装。共享 Host 更新沿现有 idle、升级和重连生命周期，保留不确定操作的对账。
- Caelis 连接列表取 Host 目录与功能字段；Magpie 放在“三方聚合网关”区域，默认检测本机网关，也可手输地址、手输模型或访问 `usemagpie.ai` 指引。无目录的手输退路和失败后“修改后重试”已提供；密钥草稿在失败返回时清除，凭据由 Host 保存。首次与设置调用同一 `ConnectionWizard` 和 Host 状态机。取消、失败、不确定结果分别保留对应动作。
- 协作配置只从本机“默认执行方式”的设置行进入一跳专页，使用 ChatGPT 的 Worker 不显示该入口；首次不出现。页面按 Host 返回的成员、系统 Agent、模型能力、effort、Fast 和 binding set 修订读写；已知角色用用户语言解释，原 ID 保留在辅助行。方案对用户称“协作方案”；未强制给不支持的模型配置字段。使用中模型断开显示用途与共享影响，按主模型/协作成员/Bot 对话用途给出先换模型、解除绑定或显式停止使用的路径，不再只有一个禁用的“移除”。
- 测试专用 `Research` 包有独立 bundle ID、进程单实例键、数据目录、HOME、PATH 与 CodexHome。`runtimeenv` 的真实账号 HOME 恢复在该 build channel 被隔离规则替换。该 channel 不能当正式安装包。

## 本机原生验收与证据

| 场景 | 结果与截图 |
| --- | --- |
| 空白首次选择、准备、失败恢复 | [选择](screenshots/implementation/01-blank-connection-choice.png)、[准备](screenshots/implementation/02-native-preparing.png)、[安装不确定状态](screenshots/implementation/03-native-install-uncertain.png)、[失败后重做](screenshots/implementation/05-native-redo-preparing.png)、[重做结果](screenshots/implementation/06-native-redo-result.png)。独立 HOME、无复制真实认证。 |
| 官方 Caelis 安装与进度 | `fresh-progress` profile 在隔离 HOME 运行官方脚本。 [实际下载进度](screenshots/implementation/08-measured-progress-live.png) 是 16.8 / 30.7 MB、149.3 KB/s；[安装后待连接](screenshots/implementation/09-install-complete-model-chooser.png)未把 CLI 安装当成就绪。 |
| Magpie 与聊天 | 独立 Caelis Store 经本机 Magpie 连接 `xiaomi/mimo-v2.6-flash`，[已连接](screenshots/implementation/10-magpie-connected-native.png)。`no-restart` profile 首次配置后不重启，提交助手称呼并得到实际模型回复：[聊天截图](screenshots/implementation/18-no-restart-live-chat.png)。这验证此机器本机网关，不代表其他提供方或真实 Codex 账号认证。 |
| 设置视觉与断开 | [设置排版](screenshots/implementation/19-settings-layout-after.png)、[使用中模型断开路径](screenshots/implementation/20-used-model-removal-after.png)。协作[页面](screenshots/implementation/21-team-layout-native-after.png)与 [breeze 模型绑定读回](screenshots/implementation/22-team-binding-readback.png)来自隔离 Host；另存及应用 binding set 的前次原生证据是 [保存](screenshots/implementation/14-team-breeze-saved.png)、[应用](screenshots/implementation/15-team-set-applied.png)。 |
| Magpie 手输 | 修改为无目录地址后出现手输模型退路，[原始截图](screenshots/implementation/23-magpie-manual-fallback.png)、[修正后截图](screenshots/implementation/24-magpie-fallback-final.png)。手输 `127.0.0.1:1/v1` 与测试字符串在 Host 得到失败，未提交到真实服务。独立 Worker 原生确认“修改后重试”可回到可编辑表单，地址与模型保留、测试密钥清除；其[截图](../../../caelis-bot-runtime-onboarding-review/research/visual-review/screenshots/34-magpie-edit-retry.jpg)取自较早的 `d743ad57` 包。 |

独立 Worker 曾因本机锁屏遇到 `cgWindowNotFound`，解锁后已用 CUA 精确绑定 `dev.caelis.bot.research` 的目标设置窗。旧表中提及的“修改后重试”、隔离启动及 Team 路由随后完成了原生复验；[独立复验报告](../../../caelis-bot-runtime-onboarding-review/research/visual-review/independent-acceptance-retry.md)记录了待修问题，本文件下面的增补记录修复后的直接结果。没有操作微信、系统设置或正式 Bot。

### 2026-10-13 至 14 原生收尾增补

下列截图来自隔离 `Research` 进程；[首次双入口](screenshots/implementation/26-first-run-two-methods.png)使用全新 `copy-first-run` HOME/Store，未启动安装或登录。两个卡片同等样式，没有“推荐”徽标或强调边框。点击值仍是原生 `caelis` / `codex`；本次文案验收未重新下载 Codex 或输入 ChatGPT 凭据。

[设置执行方式](screenshots/implementation/23-execution-copy-settings.png)将本机 Worker 的两项显示为“默认 / 使用 ChatGPT”，按钮不换行；[协作配置](screenshots/implementation/24-collaboration-copy.png)保留一跳专页、成员/系统 Agent 分组及“协作方案”入口，全局设置侧栏无 Team。[Magpie 连接页](screenshots/implementation/27-magpie-display-name.png)把已知网关显示为 `Magpie`，本机默认地址、Host 模型目录、无须重复输入 Provider key 的说明仍保留。[单模型断开即时回读](screenshots/implementation/25-sole-model-removed-immediate.png)使用仅有 `xiaomi/mimo-v2.6-flash` 的隔离 Store：确认共享影响并显式停止使用后，当前页的 Bot 对话与工作模型同时清空，无需离开设置再进入。该断开截图取自 `3727a98d` 包；之后只改了 Magpie 显示名和按钮/首屏样式，断开逻辑未变。

同一隔离应用切到英文后，[执行方式设置](screenshots/implementation/28-english-execution-settings.png)显示 `Default / Use ChatGPT`，[协作页](screenshots/implementation/29-english-collaboration.png)显示 `Collaboration settings / Collaboration plan`；验收后语言恢复为跟随系统。真实模型和 Provider ID、版本路径及官方安装来源仍按实际值展示。

在“已显式停用并断开唯一模型”的隔离 Store 上重启 Research，[聊天连接提示](screenshots/implementation/30-known-review-error-localized.png)从内部固定 Guardian 错误转为“审批模型不可用……不会跳过审批”。此处只对四条已知固定审查错误做精确双语显示映射，未知 Host 错误保持原样；审批配置、权限和原生错误事实未改。首次正常安装后尚无模型的页面仍按待连接状态呈现，而不是把安装当成就绪。

### 2026-10-14 独立报告后两项修复

[独立最终报告](../../../caelis-bot-runtime-onboarding-review/research/visual-review/final-6fb64da4/independent-final.md)在 `6fb64da4` 包上确认旧 Finding 1–5 关闭，另发现首次称呼被真实模型拒绝、唯一模型断开后协作仍声称可工作。本轮功能提交为 `12764d784f5a990297a78221f0d2bab34f17d87a`；最终包还包含本报告与截图提交，准确构建源码以 `Info.plist:CaelisSourceCommit` 为准。

- 常驻 Bot 指令把 Caelis Bot 明确为产品名称，同时要求接受用户指定的称呼；`bot-core` Skill 补上跨 Runtime/上下文的称呼与 `MEMORY.md` 指引。首次消息改为用户主动提出“称呼”并请求记住，仍是普通用户消息、原回执机制不变，接受后不在 native 设置中另存身份。向导双语标签解释“称呼”与应用名称的关系。
- 协作入口只在当前默认 Worker `Ready` 时出现。模型断开后，同页保留“管理连接/重新检查”的可执行恢复路径，不再显示“默认可工作”；若协作专页仍保持打开且 Host 回读已无可用模型，则显示连接入口而非默认就绪说明。Codex Worker 与内部 Runtime ID/存储/绑定行为未改。
- 在全新 `nickname-live-12764` 隔离 HOME/Store 中，只复制了另一隔离 profile 经官方安装的 **CLI 可执行文件**，没有复制 Store、认证或模型凭据；本轮因此验证真实本机 Magpie 连接与模型行为，不重验安装下载。用户输入 `Review Bot` 后，真实 `xiaomi/mimo-v2.6-flash` [首次回复接受称呼](screenshots/final-fixes/01-nickname-accepted.png)，并在此 profile 的 `Notebook/MEMORY.md` 写入该偏好。重启同一 Research 包后，模型对明确的 `What should I call you` [仍回答 Review Bot](screenshots/final-fixes/02-nickname-after-restart.png)。截图中的单独 `?` 是 CUA `typeText` 输入中文时只提交了标点的操作伪像；回忆验证使用后续完整英文提问。
- 在**同一个**隔离 profile 中，重新连接唯一真实模型，明确把默认 Worker 选为“默认”；[断开前](screenshots/final-fixes/03-worker-ready-before-disconnect.png)协作入口正常出现。随后通过共享影响说明勾选“停止使用并断开此模型”，[断开后](screenshots/final-fixes/04-worker-needs-model-after-disconnect.png)默认 Worker 选择保留，但工作模型不可用、协作入口消失，并出现“管理连接/重新检查”。未改动其他隔离 profile 的模型连接。
- 键盘 Tab 的 AX 焦点仍只报告 WebView 的 HTML 内容，未得到稳定控件焦点证据；窗口原生“缩放窗口”动作变为更宽布局，未证明约 800×560 窄窗。这两项留给独立 Worker 窄范围复验，不把尝试当通过。未更改系统外观或设置。

## 检查与未覆盖边界

- 最终代码 `make check` 通过，含 131 项脚本测试、Go 测试、双语目录及前端构建；沙箱第一次无法监听测试用本机端口，允许 loopback 后同一门禁通过。`make smoke` 通过 Codex stdio initialize 和资源检查，未建立会话或发送模型请求，此结果不是 Codex live auth。
- `BOT_BUILD_CHANNEL=research make build` 通过，验证完整 Desktop World 嵌套资源与本地 Apple Development 签名；最终交付包重新构建后，`Info.plist:CaelisSourceCommit` 与本分支 HEAD 一致。在可读取本机证书链的环境中，`codesign --verify --deep --strict` 通过；此签名不等于公证发行。
- 没有在真实用户 HOME 执行 brew/npm upgrade、修改 CLI/Store、复制生产认证或进行 Codex 真实账号登录。Homebrew/npm 分支有源码与测试覆盖，真实本机渠道升级、权限提示和回滚仍需独立环境验收。Caelis 官方 `install.sh` 的候选上游改动保存在 [patch](caelis-dev-install.patch)，未改 `caelis.dev`；Bot 当前通过官方脚本完成安装，并在脚本前安全暂存、校验及计量同一官方发布归档。
- Host 对手输错误返回较泛的拒绝原因；UI 使用地址/认证/模型中性说明并可修改后重试。若要分别指出不可达、认证失败与能力不兼容，Host 需要稳定的错误码；Bot 未猜测或绕过 Host 认证。此轮称呼行为改变已同步 `bot-core` Skill 和常驻引导；Bot-scoped 元数据仍按渐进加载，未改 Worker 指令。
- 本轮 `make check` 首次仅在不相关的 Telegram 慢速输入计时测试失败，单测重跑及完整 `make check` 再跑均通过；`make smoke` 的 Codex stdio 与资产检查通过。上述真实模型与设置结果是实现者原生复验，仍需独立 Worker 用全新 profile 再签收两项修复。

## 独立 Worker 复验入口

源码与包：本 worktree `git rev-parse HEAD`；`dist/Caelis Bot Research.app` 的 `Info.plist:CaelisSourceCommit` 应与 HEAD 一致。包仅供本机隔离验收，不是公证 release。

```sh
BOT_BUILD_CHANNEL=research make build
BOT_RESEARCH_PROFILE=manual-error bash research/runtime-onboarding/launch-isolated.sh --restart
```

`copy-first-run` 是空白 HOME/Store，仅用于双入口视觉；点击任一项会触发真实准备流程。`manual-error` 是无认证、无模型的独立 profile，CLI 只复制了已在另一隔离 profile 中由官方脚本安装的可执行文件。可用“默认”→ Magpie → 手输地址 `http://127.0.0.1:1/v1`、任意非秘密模型 ID、测试密钥 → 失败 →“修改后重试”复验错误恢复。`no-restart` 已有真实本机 Magpie 模型配置，默认 Worker 现选“默认”；用于确认协作入口和专页，运行命令只需把 profile 改为 `no-restart`。`nickname-live-12764` 现为完成“连接→断开”事务的无模型 Store，保留首次回复与记忆，可复验空白设置。`final-acceptance` 也是断开后的无模型 Store。`fresh-progress` 保留实际下载安装现场；不要重跑未知安装事务。所有 profile 数据都在 `.cache/runtime-onboarding-research/profiles/<profile>/`，未纳入 Git。只通过 [隔离启动脚本](launch-isolated.sh)启动，不直接从 Finder 打开 Research 包。

独立复验请使用**新的** `BOT_RESEARCH_PROFILE` 名称，确认该 profile 目录此前不存在，按“默认 → 官方准备 → Magpie 本机目录 → 可用模型 → 称呼”完成；本机网关需可达 `127.0.0.1:3425`，无需真实账号或生产认证。观察首次回复和隔离 `Notebook/MEMORY.md`，重启后再明确提问称呼；设置中先将默认 Worker 选为“默认”，确认协作入口，随后断开唯一模型并核对入口和恢复按钮。不要把复用旧 profile 的聊天记录当成新首轮证据。

### 独立复验状态（2026-10-14）

[独立 reviewer 原始受阻记录](independent-review-blocked.md)核对了 `efd223cb0508223f5aceecda4a5e4ac7e3b0946b` 源码、Research 包身份与主程序 SHA，并以此前不存在的 `review-efd223cb-1` profile 启动隔离 native host。首次精确绑定 `cua.getApp('dev.caelis.bot.research')` 时，CUA 返回 `Computer Use was not approved to use Caelis Bot Research`。自动审批没有给出更详细原因；reviewer 没有换名称、路径、窗口 ID 或工具重试。该 profile 已被启动创建，下次独立复验必须换一个全新名称。由于没有窗口访问权限，**昵称真实回复、唯一模型断开后的协作状态、窄窗和键盘路径均未完成独立原生验收**；本报告上面的截图仅是实现者证据。

同次 reviewer 在原生网络权限下对本机 Magpie `/v1/models` 的无凭据只读请求得到 HTTP 200。受限 shell 中的连接拒绝不能证明网关停机；是否在下一次复验时仍可达，需要届时重新检查。没有读取模型目录或密钥，也没有发送模型请求。

等待窗口审批期间，重新检查了首次称呼的一次性提交、`bot-core` 指引、默认 Worker 的 `ready` 门控和协作页空状态。隔离 `GOCACHE` 下，`internal/bot` 首次介绍/不确定回执/拒绝恢复定向测试、`internal/botskills` 包测试、`TestWorkerTeamProfileFollowsWorkerNotConversationRuntime` 均通过；`npm run check:i18n` 的双语目录与 4 项脚本测试通过。默认 Go 缓存路径在受限 shell 中读取被拒，改用本 worktree `.cache/go-build-targeted` 后测试通过；这是测试环境缓存权限，不是产品失败。此前完整 `make check`、`make smoke` 与 Research `make build` 的结果见上节。没有因 CUA 拒绝改动产品逻辑或声明独立验收通过。

### 2026-10-14 常驻壳与进程内切换

用户明确要求 Bot APP 生命周期与 Runtime 解耦。现在首次选择仅暂存目标执行方式；准备、官方用户级 CLI 安装、Host 模型连接与握手通过可关闭的设置弹窗执行。设置和运行中连接恢复复用相同 `RuntimePreparation` / `ConnectionWizard`。连接未完成时聊天历史、草稿、设置、桌宠及非 AI 功能继续可用，发送动作提示先连接。旧的 `RestartForRuntime` 原生入口与前端调用已移除；连接达标后在当前 `Application` / `backend.Service` 中替换 Bot 适配器，完成握手才写入 `runtime.json`。失败或取消会恢复原绑定；任务的原 Runtime 路由、原始回执与待确认结果保留，切换前对活跃工作、审批和未知结果设围栏，不重放派发。只断开 Bot 自有客户端，不停止共享 Host。Bot 自有草稿附件可复制到新连接私有选择目录；两个连接已有未发送附件时明确拒绝覆盖，保留两边原件。

空白 `hot-switch-shell-1` 隔离 HOME 的直接原生检查：关闭首次连接弹窗后，常规设置仍能打开；返回聊天后可输入未发送草稿，发送键明确提示先连接。再从聊天进入“连接与模型”，点击“默认”后同一原生设置窗口显示“正在进行准备工作”，官方 Caelis 安装实际达到 `25 MB / 30.7 MB`、`81%`、`1.6 MB/秒`，随后自动进入选择模型服务，未把安装误判为可聊天。准备前、点击“默认”后、安装后 Research APP PID 均为 `33484`；隔离 Host PID 为 `33742`。关闭模型连接和外层准备弹窗后，聊天草稿 `isolated unsent draft` 仍在，设置也仍可用。此轮实际本机 Magpie `127.0.0.1:3425` 拒绝连接，因此这个新 profile 尚无可用聊天模型；本阶段不声称新包已完成真实模型对话、昵称或协作状态独立复验。CUA 截图/AX 记录保存在本任务会话结果，独立 reviewer 仍需按新构建精确复验。

为验证安装后 CLI 文件消失，只把 `hot-switch-shell-1` 的 `.local/bin/caelis` 暂时移到同一隔离目录的 `caelis.cli-absent-test`；未触碰真实 HOME、全局 CLI 或其他 profile。APP PID `33484`、已运行的隔离共享 Host PID `33742` 均保持。设置中的同一个连接弹窗识别文件缺失后重新准备，完成第二次真实官方归档下载并再次自动进入模型连接；前后 APP 与 Host PID 都未变。新装文件与暂存的原件逐字节一致（`cmp -s` 退出 0），仅在核对后移除了测试暂存副本。活跃 Bot 已握手后 CLI 消失但 Host 仍可用、Host 真正掉线后的无重启重连，需要有模型可用的新隔离 profile 继续原生验收；源码状态机对 CLI 安装与活跃会话分开判定，不能以空白 profile 的安装验证替代上述场景。

精确 `9cdd974b` Research 包还以全新无 CLI 的 `shell-decoupled-9cdd` profile 验证了首次弹窗可关闭、设置与聊天仍可打开、草稿可输入，以及点击“默认”后窗口与 APP PID `41283` 持续存在。点击“取消准备”后发现 UI 把主动取消显示成了网络中断。随后调整为使用 `CancelSetup` 确认结果：已接受的取消显示中性“已取消准备”，可重新检查或重试；未接受取消时保留原安装错误。此项必须用后续精确构建再复验，不把旧包检查当成修复验收。

回归覆盖：首次切换同一 Service、原任务 Runtime 路由不迁移、失败回滚、取消回滚、无用户 CLI 的壳/历史/设置、活跃会话与 CLI 文件状态分离、跨 Runtime 附件保留与冲突拒绝、主动取消不误报网络。最终 `make check` 退出 0，含前端 133 项脚本检查及 Go 全包；`make smoke` 退出 0，验证 Codex stdio initialize 与资源，未发送模型请求。原生 Research 的当前测试包在此段记录时仍是未提交源码构建；最终精确 `Info.plist:CaelisSourceCommit`、主程序 SHA256 与启动 profile 以本轮最终交付回执为准。该构建因本机没有可用 Apple Development 证书，使用 `BOT_DEVELOPMENT_IDENTITY=-` 进行 **仅隔离 Research 的 ad-hoc 签名**，不等于发行签名或公证。

本轮只改变原生生命周期、连接设置与 UI 恢复，不改变 Bot 可调用工具或模型侧工作步骤；`bot-core` Skill 无需再次改动。此前称呼的 Skill 更新继续保留。
