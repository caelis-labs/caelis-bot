# Bot 桌面聊天 A2UI POC

Bot 常驻 Session 继续使用一份 `bot-core`。复杂比较、成组记录、可试填草稿会引导模型按需读取随包的 [桌面聊天参考](../internal/botskills/skills/bot-core/references/a2ui-chat.md)。简单问答保留普通 Markdown。助手在 Markdown 中提供判断所需的事实与结论，可在段落之间插入独立的 `<a2ui>` 块。聊天接收时先显示普通正文，块闭合后才编译。最终非法或截断的块按字面原文展示，代码示例依旧是代码。解析只挂在桌面聊天的助手正文，复制、引用、存储和历史都使用原文。

## 官方来源和生成方式

- 官方仓库为 `a2ui-project/a2ui`。npm 的 `repository.directory` 分别指向 `typescript/agents/a2ui_agent`、`typescript/renderers/react`、`typescript/core/web_core`。在官方发布版本变更 `7a776e7a476b61215ec68c6511f3b8f1921d6e24` 读取的三个 manifest 分别为 `@a2ui/agent@0.2.1`、`@a2ui/react@0.16.1`、`@a2ui/web_core@0.16.1`，与 npm 发布包一致。Express 编译器、React host registry、Web Core Basic Catalog 的三份 npm 源映射内容和该提交的对应 TypeScript 逐字节一致。发布元数据没有 `gitHead` 和 registry attestation。实际执行和生成以锁文件的 tarball URL 与 SHA-512 完整性校验为准。三包及官方仓库标为 Apache-2.0，随参考文件保留发布包的 [许可证与附带声明](../internal/botskills/skills/bot-core/assets/A2UI-LICENSE)。[版本提交](https://github.com/a2ui-project/a2ui/commit/7a776e7a476b61215ec68c6511f3b8f1921d6e24) · [官方许可证](https://github.com/a2ui-project/a2ui/blob/main/LICENSE)
- v1.0 是编译后的 A2UI wire 版本。Express 是官方提案和已发布 Agent SDK 中的推理格式。初版原生探针用 `connect-src 'none'` 验证 Express 编译加显示，真实 Wails 窗口验证了模板、绑定、表单和切换。Vite 构建会报告 SDK 中的 Node 模块外部化以及大 chunk；实测路径能离线运行，POC 使用 Express 单一路径。[v1.0 协议](https://a2ui.org/specification/v1.0-a2ui/) · [Express 提案](https://github.com/a2ui-project/a2ui/blob/main/specification/proposals/express/a2ui_express.md)
- [Catalog 声明](../frontend/src/chat-a2ui-catalog.ts) 用锁定 `basicCatalog.derive` 选取组件和函数，继承对应的 API 和 schema。编译、渲染和文档生成共享这份声明。参考文件由 [生成脚本](../script/generate-chat-a2ui-reference.mjs) 运行官方 `SkillGenerator` 与 `ExpressFormat` 得出，指定 `v1.0` 和 `createSurface`。脚本移除生成器仍列出的事件、强制动作、surface 寻址三条规则及事件示例，保留其余官方语法和派生签名。Bot 维护前置说明和三组同时用于测试的示例，前置说明限定字符串和消息子集。运行 `source script/env.sh; node script/generate-chat-a2ui-reference.mjs` 更新，`make check` 校验生成结果。Caelis 用现有 `Skill` / `Read` 读本地引用，Codex 走现有技能与文件读取。[官方 Catalog 文档](https://a2ui.org/concepts/catalogs/)

## v1.0 能力和 Bot 范围

锁定版 `@a2ui/web_core/catalogs/basic/v1` 导出官方 18 个组件的实现及 schema。React `A2uiSurface` 渲染该 SDK 的组件模型，Basic 实现大多为 Web Components；Chat 为 `Text` 提供小型 React 实现，用转义文本显示解析后的值，保留早期历史用的 `title`。下表的 SDK 列表示已在锁定的本地包核对导出、schema 或实现，实测列单独记录。完整参数见 [官方 Basic Catalog](https://a2ui.org/specification/v1.0-basic-catalog-implementation-guide/)。

| 能力 | 官方 v1.0 与锁定 SDK | Bot 取舍与证据 |
| --- | --- | --- |
| 文本 | `Text` 的 `body`、`caption` 可绑定，官方 Web 实现可渲染 Markdown | 启用纯文本、绑定和纯函数结果。旧 `title` 兼容分支经夹具验证。普通回复仍走 Markdown |
| 媒体和图标 | `Image`、`Icon`、`Video`、`AudioPlayer`；官方包含媒体 URL 校验和图标映射 | 排除四项，保持离线；原生夹具验证 `Image` 原样降级 |
| 基本布局 | `Row`、`Column` 的布局轴、对齐、权重；`Card` 单子项；`Divider` 分隔 | 启用。原生夹具验证布局与分组，真实 Codex 验证按组显示三列记录 |
| 集合与模板 | `List` 横向或纵向；`List`、`Row`、`Column` 支持显式子项或动态模板。Basic Guide 另列 `@index`，SDK 作为协议内建函数提供 | 启用数据数组、`_template`、相对子项路径。夹具验证 `List`、两条绑定记录与价格。真实 Codex 验证每组各三条。横向列表和 `@index` 未做原生专项测试 |
| 选项和展开 | `Tabs` 有本地的已选项，`Modal` 有呈现内容和打开/关闭行为 | 启用 `Tabs`，真实两后端点击后显示对应选项，`Modal` 留在 Catalog 外 |
| 输入 | `TextField` 支持 `shortText`、`longText`、`number`、`obscured`；`CheckBox` 写回布尔；`ChoicePicker` 支持单选、多选、chips 与过滤；`Slider` 写回数值并支持步长；`DateTimeInput` 使用带时区的 RFC 3339 | 启用前四类。真实 Codex 验证邮箱即时校验、滑块和复选框，夹具另外验证单选和跨块隔离。排除日期输入。其余输入变体、组合过滤及无障碍遍历未做专项验收 |
| 绑定和本地状态 | `@path` 支持绝对路径与列表相对上下文；`@call` 可在解析属性时调用函数；输入经本地数据模型双向绑定 | 初值随 `createSurface`，只在当前块的视图存活期间有效。夹具验证邮箱更正后绑定文本同步，独立块状态不串，历史重新挂载恢复初值 |
| 校验与函数 | `@checks`；Basic 注册 14 个函数：校验 `required`、`regex`、`length`、`numeric`、`email`；格式化 `formatString`、`formatNumber`、`formatCurrency`、`formatDate`、`pluralize`；逻辑 `and`、`or`、`not`；副作用 `openUrl` | 启用 `required`、`length`、`numeric`、`email`、`formatNumber`。原生验证 `email` 与 `formatNumber`；文本输入始终产出字符串，数量范围用 `Slider`，实测淘汰了 TextField 配数值校验的方案。夹具验证 `openUrl` 降级 |
| 按钮、事件、函数回传 | `Button` 可触发带上下文的 action 或调用。协议定义 action、错误和双向 RPC，分别有 `callAgentFunction` / `agentFunctionResponse`、`callRendererFunction` / `rendererFunctionResponse`。锁定 MessageProcessor 有 action handler 及可选 outbound listener | 排除 `Button` 和事件声明；不挂出站监听器。夹具验证动作和独立调用降级，本地编辑未触发聊天发送。需向助手提供结果时走原有聊天输入 |
| surface 生命周期 | `createSurface` 可内联数据和组件，协议还定义 `updateComponents`、`updateDataModel`、`deleteSurface`、跨 Catalog、扩展和 `sendDataModel` 回传，SDK 有对应消息 schema 和 processor | 每个闭合块一个 `createSurface`、一个派生 Catalog 和独立 processor；更新指令、跨 surface、元数据、回传均在边界外。夹具验证更新拒绝、独立重放和局部重置 |

协议只定义回传封装。若另行立项，需先确定可信的会话/消息/surface/操作身份和生命周期，明确哪些字段可在用户显式操作后进入原有授权边界，并验证重放、重复触发、来源和字段最小化。此次只验证本地边界，范围限于 Skill 和聊天展示。每块限 8,192 个 UTF-16 代码单元、40 个组件，带路径和集合限制。没有协议执行通道、渠道或 Core 改造。[动作和函数](https://a2ui.org/concepts/actions-and-functions/) · [v1.0 消息与 surface](https://a2ui.org/specification/v1.0-a2ui/)

## 真实模型与原生面板

用同样的五条中文 [合成输入](../script/a2ui-chat-live.mjs) 对两后端各跑一轮。每条单独的测试 profile 和 Session。用户输入没有提到卡片、A2UI 或 Skill。前三条分别是陶艺比较、补货记录和报名演练。对照是 `19+23` 只要结果，以及一句话解释闰年。正式触发定义为真实助手原文被生产 Markdown 扩展识别的块；有效触发还须通过锁定编译器和 Bot 范围校验。两者都按回复计。截图是另一层实际原生证据。

| 后端 | 比较 | 补货记录 | 报名演练 | 简单对照触发 | 三个预定场景 |
| --- | --- | --- | --- | ---: | --- |
| Codex CLI 0.172.0 · gpt-6.2-sol · low | 三家 Tabs，推荐符合安静和预算顺序 | 绑定模板呈现 6 条记录，分组切换，金额保留两位小数 | 初始邮箱格式提示，修正后消失，份数 1–5 和备注可本地改 | 0/2 | 正式触发 3/3，有效 3/3 |
| Caelis Core v0.86.1 · `magpie@local/magpie/codex/gpt-6-luna` | 三家 Tabs，实机能切；推荐多花 5 元换工具，不符合用户给的次优先级 | 选用普通 Markdown，给出 6 项合计和 3 项待点 | 生成了闭合块，含 Express 不支持的 `+` 拼接，原生面板保留正文并显示块原文 | 0/2 | 正式触发 2/3，有效 1/3 |

这些是每后端三条任务与两个对照的小样本。对照只评价是否触发；有的简单回复仍带进度话术。两后端也都曾在正式回复前向用户提到 `bot-core`。有值域和条件含义的草稿仍需要模型正确选取绑定、校验和文案。Caelis 三个预定场景都通过 `Skill` 及 `Read` 加载参考，两个对照只读核心 Skill。Codex 的三个复杂样本执行了本地引用读取，两个对照没有引用读取。二者的最终助手原文和 Bot SQLite 对照一致。

原生 Wails 中点击 Caelis 三家比较、Codex 两组仓库记录、编辑并校验 Codex 报名草稿，也回放了 Caelis 的真实非法块。已保存截图和可访问性操作证据。原生 WKWebView 夹具驱动同一生产聊天代码，另覆盖分块/结束、块顺序、字面代码、历史重放和旧标题、非法/截断、超长源文换行、用户侧边界、动态模板、复选与单选、表单状态隔离、零发送和禁止更新。Windows、所有官方控件组合、键盘/读屏矩阵、更长自然流量及任何协议回传未验收。

摸底轮单独保留：修订读取指导前，Codex 为 3/3 正式、3/3 有效，Caelis 为 1/3 正式、1/3 有效，两边对照均 0/2。Caelis 一次尝试用 Markdown HTML 折叠，两次先向资源工具读取本地引用，其中一条随后通过文件工具读到。摸底的 Codex 草稿将字符串输入接到 `numeric`，夹具验证合法数字也会报错，随后改用了官方 `Slider`。最早本地 composer claim 被拒的探针没有送出模型请求。修订轮 Caelis 前两个已接受的操作由原运行日志和 SQLite 只读对照，保留原回执。Caelis 空闲投影仍可携带最近 Turn 标识，测量驱动更新后从尚未建立的 profile 继续。

## 实际 macOS 应用及 ZIP

基线 `d3bd238b2ccf80f7e523d930d5b3bae1dab890ae`，POC 源码 `677c424ddbcabbb9cb4f05891a4a84758900200a`。同一 macOS 27.2.1 arm64、Xcode 27.0、Go 1.26.11、Node 24.21.0 与 Vite 8.3.8。两边使用各自锁文件，应用名称、构建通道与参数一致（`BOT_BUILD_CHANNEL=development`、`BOT_DEVELOPMENT_IDENTITY=-`，清除发布 tag 与 Sparkle 发布公钥变量）。Core 为 v0.86.1，Sparkle 为 2.10.0，包内最终 Core、Frameworks、Resources 的 SHA-256 对照一致。

| 项目 | 基线字节 | POC 字节 | 增量字节 |
| --- | ---: | ---: | ---: |
| JS 原始 | 1,807,622 | 2,646,542 | 838,920 |
| CSS 原始 | 210,605 | 211,447 | 842 |
| JS 逐文件 gzip | 511,109 | 734,653 | 223,544 |
| CSS 逐文件 gzip | 35,721 | 36,038 | 317 |
| `.app` 逻辑字节 | 219,943,719 | 220,813,767 | **870,048** |
| 实际 macOS 应用 ZIP | 85,913,835 | 86,157,354 | **243,519** |

逻辑字节为包中 99 个普通文件的长度之和，不遍历 9 个符号链接（链接目标字节共 213，两边一致）。应用使用 ad-hoc 签名。ZIP 由 `ditto -c -k --sequesterRsrc --keepParent` 生成，条目使用 Deflate 或 Store。两份均经 `unzip -tq`、`ditto` 解包、文件哈希/链接对照和 `codesign --verify --deep --strict`。它们是可在本地交付评估的实际应用归档，公开分发需要另行 Developer ID 签名与公证。gzip 列仅为前端构建产物的诊断统计。

## 复现

在 POC 源码提交的 checkout 执行。使用 Bash 和当前工具链锁。若需要重测同一版本，请在基线和 POC 分别按锁安装，不并发重写同一 checkout 的 `frontend/dist`。

```bash
make setup
make check
CODEX_HOME=/absolute/path/to/isolated-codex-home make smoke

root="$PWD/.cache/a2ui-reproduce"
mkdir -p "$root/evidence" "$root/packages"
git worktree add --detach "$root/baseline" d3bd238b
(cd "$root/baseline" && make setup)
for label in baseline poc; do
  checkout="$PWD"
  if [[ "$label" == baseline ]]; then checkout="$root/baseline"; fi
  pkg="$root/packages/$label"
  (
    cd "$checkout"
    env -u BOT_RELEASE_TAG -u BOT_SPARKLE_PUBLIC_KEY \
      BOT_BUILD_CHANNEL=development BOT_DEVELOPMENT_IDENTITY=- \
      BOT_BUILD_OUTPUT_ROOT="$pkg" make build
  )
  ditto -c -k --sequesterRsrc --keepParent "$pkg/Caelis Bot Dev.app" "$pkg.zip"
  unzip -tq "$pkg.zip"
  ditto -x -k "$pkg.zip" "$root/packages/extracted-$label"
  codesign --verify --deep --strict "$root/packages/extracted-$label/Caelis Bot Dev.app"
done
source script/env.sh
node script/a2ui-chat-package-report.mjs \
  "$root/baseline" "$root/packages/baseline" "$PWD" "$root/packages/poc"
```

依次运行三个夹具场景。同名 `.png.json` 的 `expected.ok` 应为 `true`。

```bash
for scene in comparison collection draft; do
  BOT_PREVIEW_FIXTURE=a2ui BOT_PREVIEW_A2UI_CASE="$scene" \
    BOT_PREVIEW_AUTO_CAPTURE=1 BOT_PREVIEW_CAPTURE="$root/evidence/panel-$scene.png" \
    bash script/build_and_run.sh --chat-preview
done
```

真实样本通过已有 Bot 本地 transport 向两个真实后端提交。准备已配置模型和 Agent Team 的**隔离** Caelis Store 配置，以及已登录的**隔离** `CODEX_HOME`。测试驱动每个样本建立独立 Bot profile，Caelis 还为每个样本建立独立 Store。取原回执或已有历史确认后再继续；`BOT_A2UI_RESUME=1` 跳过已有 profile。

```bash
source script/env.sh
binary="$root/packages/poc/Caelis Bot Dev.app/Contents/MacOS/caelis-bot"

node script/a2ui-chat-live.mjs caelis "$binary" "$root/real-caelis" \
  /absolute/path/to/isolated-caelis/config.json 'magpie@local/magpie/codex/gpt-6-luna'

CODEX_HOME=/absolute/path/to/isolated-codex-home \
  node script/a2ui-chat-live.mjs codex "$binary" "$root/real-codex" "$(command -v codex)" gpt-6.2-sol

CAELIS_BOT_DATA_DIR="$root/real-caelis/compare/bot" \
  BOT_BUILD_OUTPUT_ROOT="$root/packages/poc" bash script/build_and_run.sh --restart
```

在桌宠打开聊天。分别检查 `compare`、`records`、`draft` 的隔离 profile；Codex 回放同时设置对应隔离 `CODEX_HOME`。测试完成后移除临时认证副本。基线构建、当前 ZIP 和本次测量证据保存在 `.cache/a2ui-phase2/`。原生夹具 PNG 及断言、真实模型回执/合成回复、无障碍观察、逐项包体统计与测试日志在 `.cache/a2ui-phase2/evidence/` 及 `final-{caelis,codex}/`。
