# Caelis Bot 首次连接与设置复用：交互草稿

> 调研稿，2026-10-13。基线 `main` / 构建源码均为 `393b29f5406df739682280c1c87652bbe773c52b`（#163 已合并）。独立 `research/runtime-onboarding` worktree；未改正式产品实现，未提交、推送、开 PR 或发布。下面的页面文案与状态是**提案**，后附现状实测和未验证项。

## 结论与取舍

首次使用把“连接 AI”放在第一段：**选连接方式 → 准备本机 Runtime → 登录或连接一个聊天模型 → 验证 → 开始对话**。助手称呼、附加功能、权限在连接过程中以可跳过的简短设置呈现，连接成功后可补全。Caelis 安装完成只表示程序可用，至少一个可用聊天模型通过配置检查后才能出现“开始对话”。

当前两张页面把“安装状态”“模型连接”“高级设置”“检测结果”散列在长页面里；第二张仍显示“准备 Caelis”，却缺少下一步的主按钮层级。建议把它们合为**一个连接卡片**：顶部是目标，中央只显示当前阻塞动作，底部是上一步/稍后继续。完成的阶段折叠成一行“Caelis 已安装”，不再与待办的“连接一个模型”争抢视觉注意。

## 首次主线与页面文案

进度只呈现三个用户可理解的阶段：**1 连接 AI → 2 认识助手 → 3 开始对话**。安装、认证、模型检测是第 1 阶段内部状态，不各占一级导航。可选附加功能与权限在第 2 阶段作为次要入口；尚未授权不阻止文本对话，确需权限时再上下文请求。用户已填写的介绍保留在本地，连接就绪后才发送，不因返回或切换 Runtime 丢失。

| 页面/状态 | 主标题与简短说明 | 主按钮 | 次操作与规则 |
| --- | --- | --- | --- |
| 连接方式 | “先连接 AI，马上就能聊天”／“使用这台 Mac 上的 Codex 或 Caelis。Bot 会复用它的账号和模型。” | `使用 Codex`、`使用 Caelis` 两张选项卡 | 若只发现一种已就绪，预选并显示“已找到你现有的连接”；仍可更换。多份来源先显示实际将使用的路径与渠道，再确认选择；不自动覆盖或并装。 |
| Runtime 未安装 | “准备 Codex/Caelis”／“将官方程序安装到此 Mac 的用户环境，之后与命令行共用。” | `安装并继续` | 安装前显示来源与目标位置；`返回`。安装中主按钮变 `正在安装…`，禁重复点击；允许关闭窗口并在设置中接续。 |
| Runtime 已安装，待连接 | “连接一个可聊天的模型”／“Caelis 已安装。完成连接后就能开始。” | `连接模型` | 上方完成摘要 `Caelis 已安装 · v…`；已有可用模型则改为 `使用这个模型`，不要求重配。Codex 改为 `使用 ChatGPT 登录`，并列 `使用 API Key`。 |
| 选择提供方 | “怎样连接模型？” | 账号登录：`继续登录`；API 服务：`继续` | 默认优先呈现本机已发现的 Magpie、已有连接和常见提供方；`其他服务`可手输服务地址。OAuth 只在当前 Runtime 支持的提供方上出现。原始 provider ID 可放说明/技术详情，不作新手选项文案。 |
| 填写/授权 | “连接 Magpie/… ”／“地址和模型由 Caelis 保存并验证。” | `验证并连接` | `服务地址`、`模型`（自动列出，可输入）；按官方 `/connect` 能力显示账号授权或 API Key。密钥只提交给共享 Runtime，不进入 Bot 自建凭据库。`取消`中止当前连接流程并返回选项，已保存的其他连接不动。 |
| 验证中/成功 | “正在验证模型…” → “已经可以聊天”／“Bot 将使用：…” | `继续` → `开始对话` | 验证要区分“配置已保存/认证有效”和“实际聊天回复已验证”。若仅完成前者，文案写“连接已保存，启动后检查对话”，不可声称已收到回复。 |
| 认识助手 | “给助手一个称呼”／可选描述；功能与权限在下方一行“稍后设置” | `开始对话` | 若 AI 连接尚未就绪，主按钮回到 `继续连接 AI`；已有介绍只本地保存，不提前发送。 |

**两张现有页面的具体替换：**

```text
连接 AI · Caelis                                    第 1 / 3 步
Caelis 已安装  ✓   v0.71.0              [查看来源]

还差一个模型，就能聊天
从这台 Mac 的现有连接中选择，或添加新的服务。

  [本机 Magpie · 可用]   [其他已有模型…]
  [连接新模型]

状态：等待模型连接                      [返回] [稍后继续]
```

安装时同一位置显示“正在安装 Caelis… / 下载中、校验中、正在安装、重新发现”，只保留一个进度与失败恢复区；成功后卡片原位转成“还差一个模型”，不跳到一张信息布局不同的空白页。安装结果不写“配置就绪”。当已有模型可用时，原位显示模型名称、认证状态、`验证并继续`；验证通过才显示 `继续`。

原生截图审视要点：[安装中](screenshots/07-caelis-installing.png)和[已安装无模型](screenshots/08-caelis-installed-no-model.png)在同一大画布中把主动作、状态、空态分散得很远；“正在下载并安装…”与“未安装”同时出现，缺少清楚的过程关系。安装后“连接新模型”在右侧、目标与引导在左侧，视线要往返；“高级连接设置”在未完成主线时占据同一级。可用模型页里 `使用` 被禁用但无就近说明；[设置详情](screenshots/16-in-use-remove-disabled.png)的 `移除` 也只有禁用状态。建议每个状态仅一个显著主按钮，状态文字紧挨动作，禁用操作给可读原因和可执行的下一步；进度与错误用 `role=status/alert` 宣告，键盘焦点回到新状态标题或主动作。当前 provider 选项暴露 `magpie` 等内部 ID 与英文解释，应改为面向用户的中文名称，技术 ID 放辅助信息。

## 设置沿用同一连接组件

`设置 → 连接与模型` 顶部只保留“当前用于聊天的连接”摘要：Runtime、来源、账号/服务、Bot 对话模型、验证时间及状态。操作是 `更换`、`重新验证`、`管理`。下面“其他连接”可 `添加连接`。首次引导与设置调用**同一个 ConnectionFlow 组件与后端状态机**，以 `context=onboarding/settings` 决定返回位置和最后按钮；提供方目录、地址/模型发现、OAuth 轮询、错误和取消都相同。

详情页提供以下可见动作：

- `更换 Bot 对话模型`：先验证目标，再原子切换；旧模型继续可用到切换成功。
- `修改连接`：可修改地址、模型或重新输入 API Key；先用 Runtime 的现有连接/配置 API 生成新修订，验证后生效。若当前接口不支持原地编辑，第一版明确显示“添加新连接并替换”，不可暗中删除重建。
- `重新认证`：仅在提供方支持时展示，复用 Runtime OAuth/API Key 流程；已连接模型在新认证确认前保持原状。
- `断开此模型`：未被使用时确认后删除；**正在供 Bot 对话或 Caelis 主模型使用时仍有入口**，解释依赖并给 `先换成其他模型` 或 `停止使用 Caelis 并断开`。单模型场景可以在 UI 明确停用 Bot 对话、切换 Runtime/进入待连接状态，然后请求 Runtime 删除；不能只是灰掉“移除”，也不能让用户猜 CLI 命令。共享 Runtime 的删除会影响 CLI 和其他客户端，确认框点明具体模型、用途和影响范围。

单模型断开需要新增 Bot 侧的有序状态转换：停止新对话入场并标记“需重新连接”→ 清除 Bot 对该模型的选择（或切至已经验证的替代模型）→ 调用 Caelis `hostDeleteModel` → 读回 Host 修订与模型列表 → 恢复/停留在连接页。已有进行中工作不可静默中断；失败时保留旧选择或显示不确定状态。Caelis TUI 的删除适配器已有删除后清空所选 Session 模型的分支，证明底层允许这一方向；Bot 当前额外的使用中保护在 `internal/app/runtime_configuration.go:31-54`，不能只把前端按钮解禁。

当前源码中首次与设置已共用 `ConnectionWizard`，但首次还套一层 `RuntimeSettings`，设置由 `RuntimeWorkspace` 另行展示；建议抽出共同的 `ConnectionSummary + Flow + Repair`，减少两处状态文案漂移。高级内容（多模型、team、judge、ToolSearch、手动程序/Store 路径）在 `高级` 中，默认不要求新手分配角色。对话默认选首个经验证的可用聊天模型；后台工作跟随 Runtime 合理默认值，之后再改。

## 异常、取消与更新

| 情形 | 页面给出的下一步 |
| --- | --- |
| 网络下载失败 | 保留选中的 Runtime、已有 Store 和表单；写“无法取得官方安装包”，提供 `重试下载`、`查看网络/代理说明`、`选择本机已有程序`。本次隔离原生 Codex 下载确实失败，但从相同隔离 HOME 的终端执行先前取得的官方脚本成功；需诊断 App HTTP 代理/证书环境，不能笼统说用户必须去终端。 |
| 程序已在 PATH/标准路径 | 展示实际可执行文件、版本、安装渠道；`使用现有程序` 与 `重新检测`。多来源需用户选其一；不静默并装。握手/版本失败时给原安装渠道的修复动作。 |
| Caelis 已装但无模型 / 模型不支持聊天 | `连接模型`；模型列表为空则 `手动输入模型名`。只有 Runtime 返回可用且聊天能力满足后继续，不能把 CLI 已安装等同就绪。 |
| 登录等待或取消 | 账号授权显示“等待浏览器完成登录”和 `取消登录`，取消后回账号选择；认证失败保留非密钥字段，给 `重试` / `换一种方式`。永不代输入秘密。 |
| 模型验证失败 | 按错误给 `修改地址`、`重新输入密钥`、`选择其他模型`、`重试验证`；不丢弃旧连接，不自动重放不确定提交。 |
| 更新被占用 | 显示“另一项更新正在进行”，提供 `查看进度/稍后重试`；不得并行运行包管理器或重复安装。 |
| 更新失败或回滚 | 复查实际 CLI 版本与 Host 握手。若旧版仍可用，显示“仍在使用旧版”；若握手失败给原渠道重试或选择已有安装。Caelis Host 启停/回滚由 Caelis 官方生命周期处理，Bot 不杀共享进程。 |
| 用户取消当前连接 | 对未提交表单清空密钥草稿、关闭；对进行中的 OAuth/连接 flow 先调用 Runtime cancel 并等待确认；结果不明则显示“状态待确认，重新检查”，不宣称已取消。 |

**来源识别和更新建议。** 默认生产安装调用 Codex、Caelis 官方脚本，落在**用户**的 `~/.local/bin`，沿用用户默认 `~/.codex` / `~/.caelis`。现有 brew cask/formula、npm 全局安装需按可执行文件真实路径、symlink 的包根、`brew list --versions`/`brew --prefix`、`npm prefix -g`/`npm root -g` 及 Node 可用性做只读归属判定；冲突或不确定时显示来源供选择。Bot 负责调用相同安装渠道的**非交互更新**，其后复查版本、路径、标准握手；brew 只更新匹配 formula/cask，npm 只更新对应全局包，不能回退成官方脚本另装一份。权限不足、Node 不可用、包管理器要求交互、渠道已移除等应指向准确的渠道/命令和修复步骤。未知来源只允许“选择已有程序/查看官方说明”，不自动替换。当前 Codex Bot 对非标准路径直接拒绝更新；Caelis Bot 调 `caelis update`，Caelis CLI 本身已有 raw/npm 渠道识别，但 Bot 对 `skipped` 与实际版本的解释仍需补齐。不得仅因最新 release 版本不同就假设已安装二进制不兼容，真正以握手和消费的协议语义判断。

| 已确认来源 | Bot 可执行的更新 | 前置检查与失败出口 |
| --- | --- | --- |
| Codex 官方脚本 | 原官方安装脚本的非交互模式 | 确认可执行文件及 `CODEX_HOME` 属于同一用户；脚本可能检测到 brew/npm 冲突，冲突时暂停并显示来源选择，不静默并装。 |
| Codex Homebrew cask | `brew upgrade --cask codex` | 只有本机 cask 收据、路径一致才调用；Homebrew 锁/权限/需交互时保留现版本并显示该 cask 的明确修复步骤。Homebrew formula 只有收据确实存在且名称一致才用 `brew upgrade --formula <name>`，不可把 cask 当 formula。 |
| Codex 全局 npm | 用找到的 npm/Node 与**原 prefix**执行 `npm install -g @openai/codex@latest` | 检查 symlink 指向 `npm root -g/@openai/codex`、Node/npm 可执行与 prefix 可写；不可顺手改默认 prefix 或 sudo。 |
| Caelis 官方脚本 / 全局 npm | 调 `caelis update`，让 CLI 按自身 raw/npm 来源更新 | Caelis 源码会比对包目录与 `npm root -g`，非全局 npm 会 skip；Bot 应解释 skip、重新发现版本并检查共享 Host，避免把 exit 0 当作已升级。当前官方 README 没列 Homebrew。 |

本机实际 `node` 为 `/opt/homebrew/bin/node` v23.11.0，项目 `script/env.sh` 为开发构建优先选 `/opt/homebrew/opt/node@24/bin/node` v24.21.0；包管理更新应按已装 npm 的 Node 环境检查，不借用 Bot 的构建 Node 强行改用户安装。brew formula/cask 命令格式依据官方手册；Codex npm/cask 命令依据 OpenAI 官方资料，Caelis npm 包名和更新分支依据当前 Caelis README / updater 源码。这是**实现建议**，本轮没有在真实 HOME 执行升级。

## 调研证据与验收边界

| 证据层 | 结果 |
| --- | --- |
| 隔离身份与数据 | 临时 test-only `research` bundle ID `dev.caelis.bot.research`、名称 Caelis Bot Research、隔离 `HOME/ZDOTDIR/PATH/CODEX_HOME/CAELIS_BOT_DATA_DIR`。避免 `runtimeenv` 在 LaunchServices 启动时恢复真实 shell PATH。`Info.plist` 记录源码 SHA `393b29f…`。测试程序和 Store 均在本 worktree `.cache/runtime-onboarding-research/home`，没有改真实全局 CLI/Store。此 overlay 不进入正式产品。 |
| 原生空白流程 | [介绍](screenshots/01-introduction.png) → [可选功能](screenshots/02-extras.png) → [权限](screenshots/03-permissions.png) → [选 Runtime](screenshots/04-empty-runtime-choice.png)。没有授予系统权限。 |
| 官方安装与握手 | [Caelis 未安装](screenshots/06-caelis-missing.png) → [安装中](screenshots/07-caelis-installing.png) → [已安装无模型](screenshots/08-caelis-installed-no-model.png)。原生 UI 运行官方脚本，隔离 CLI v0.71.0，独立 Store 的 Host `running`；引导未显示开始按钮。Codex [未安装](screenshots/05-codex-missing.png)，原生下载失败；同一隔离 HOME 从官方脚本安装 v0.162.1，重新检测后出现 [ChatGPT/API Key 登录入口](screenshots/17-codex-auth-entry.png)，**未进行真实账号登录**。 |
| Magpie 闭环 | [添加连接选项](screenshots/10-caelis-api-options.png) → [Magpie 表单](screenshots/11-magpie-form.png) → [已添加](screenshots/12-magpie-added.png) → [模型就绪](screenshots/13-caelis-model-ready.png) → [实际聊天回复](screenshots/14-magpie-live-chat.png)。`GET /v1/models` 返回模型 ID，在隔离 Caelis 中手选 `xiaomi/mimo-v2.6-flash`，无手输密钥；Bot 重启后给合成介绍回复。这验证本机 Magpie 与共享 Caelis Host 的实际对话，不验证其他提供方或系统权限。 |
| 设置移除卡点 | [连接设置](screenshots/15-settings-connected.png)、[使用中模型无法移除](screenshots/16-in-use-remove-disabled.png)。AX 显示 “Caelis 主模型 · Bot 对话”且“移除” disabled；单模型用户缺少可见断开路径。原生表单自身有“取消”，与“已连接后断开”是两回事。当前设置可以“添加连接”和换对话模型；现有连接详情没有“修改”或“重新认证”动作，二者也是待实现的交互缺口。 |
| 源码位置 | `frontend/src/BotSetup.tsx`（连接在第 4 步）、`frontend/src/RuntimeSettings.tsx:67-73,99,108`（可“稍后设置”、模型/安装分区）、`frontend/src/settings/runtime/{ConnectionWizard,RuntimeWorkspace}.tsx`（共享连接向导，设置详情禁删使用中模型）、`internal/backend/caelis/setup.go:38-75`（无可用模型不 ready）、`internal/backend/codex/runtime.go:67-89`（非官方位置拒绝更新）、`internal/caelisruntime/runtime.go:132-197,259-286`（委托 Caelis CLI 更新且解析结果）。 |
| 本机只读探查 | 真实用户当前 Codex 为 `~/.local/bin/codex → ~/.codex/packages/standalone/current/bin/codex` v0.162.1，Caelis 为 `~/.local/bin/caelis` v0.71.0；`npm prefix -g` 是 `~/.local`，该 prefix 下 `@openai/codex` 未安装。未在真实 HOME 执行 brew/npm upgrade。Homebrew 查询改用隔离缓存仍需联网取 API，沙箱 DNS 拒绝，故本机 brew 包归属未确认。 |

构建与检查：`make check` 首次在沙箱中因本地 `listen` 权限被拒绝，改用同一隔离 HOME 允许测试 socket 后通过；`make smoke` 的 Codex stdio 初始化和资源检查通过，未建对话；`BOT_BUILD_CHANNEL=research make build` 通过并验证 ad-hoc 签名、Desktop World 资源与独立 bundle ID。运行中的研究 bundle 来自同一源码 SHA；临时 identity/LaunchServices overlay 已从 tracked 文件撤回，worktree 只留研究文档、截图和隔离启动脚本。构建产物在被忽略的 `dist/Caelis Bot Research.app`，隔离 HOME/Store 在被忽略的 `.cache/runtime-onboarding-research/`。

若需在这台机器上复看研究 bundle，只用 [隔离启动脚本](launch-isolated.sh)；它检查独立 bundle ID 与本机常见 brew CLI 路径，并显式传入隔离 HOME、PATH、CodexHome、Bot 数据目录。不要从 Finder 直接打开该临时 bundle，也不要在其他机器上把此脚本当通用沙箱。源码中的默认 `~/.codex`/`~/.caelis` 规则没有改动。

**验证缺口。** 未执行真实 Codex OAuth/API Key 登录；未模拟 Homebrew/cask/npm 的真实升级及回滚；Magpie 模型目录的 UI 自动建议尚未目视确认（此轮是读取 `/v1/models` 后手输模型名），但手输 fallback 已走通。截图是当前产品调研证据，不是新版交互的实现截图。新稿需评审后另派正式开发。

**最小改造路线。** 先统一 ConnectionFlow 的“准备/认证/验证/取消/接续”状态与中断恢复，再移动首次流程顺序并合并页面卡片；接着补设置里当前模型“换后断开”的事务路径；最后加 Runtime 来源识别及各渠道非交互更新，分别覆盖 brew formula/cask/npm 与脚本。每步用独立 fixture 契约测试、真实 CLI 握手、原生窗口截图验证。此轮只有研究文档和隔离 overlay，没有 Bot 行为改动，所以无 Bot skill 变更。

## 官方资料

- [Codex 官方安装脚本入口](https://chatgpt.com/codex/install.sh)；[OpenAI 开发者文档的 npm/brew Codex 命令](https://developers.openai.com/cookbook/examples/codex/using_goals_in_codex)。
- [Homebrew 命令手册](https://docs.brew.sh/Manpage)：区分 `brew upgrade --formula`、`--cask`，并验证非交互/依赖/锁行为。
- [npm 全局安装 prefix](https://docs.npmjs.com/cli/prefix.html)、[npm 全局更新说明](https://docs.npmjs.com/updating-packages-downloaded-from-the-registry/)。
- [Caelis 安装脚本](https://caelis.dev/install.sh)；本机 Caelis CLI 当前源码 `internal/updater/updater.go` 中 raw/npm 检测与对应更新分支，未发现官方 Caelis Homebrew 安装渠道。
