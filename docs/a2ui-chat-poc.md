# 桌面聊天 A2UI POC

`chat-a2ui` 通过现有 Bot Skill 装配在常驻 Session 中按需加载。助手在独立可读的 Markdown 中插入 Express 块。桌面聊天仅为助手正文启用这项语法。每个闭合块由官方编译器生成一个 v1.0 surface，再由官方 React Renderer 显示。消息复制、引用和历史都使用原始文本。

组件目录包括 `Text`、`Card`、`Row`、`Column` 和 `Divider`，只接受字面文本和静态组件引用。每块限 8,192 个 UTF-16 代码单元与 40 个组件。接收时先显示周围正文，闭合后才编译；非法或最终仍未闭合的块按字面源文显示。Markdown 里的代码示例继续作为代码显示。

## 官方包与体积

用 Node 24.21.0、Vite 8.3.8，锁定 npm 发布包 `@a2ui/agent@0.2.1`、`@a2ui/react@0.16.1`、`@a2ui/web_core@0.16.1`。编译器入口解析时会报告 Node 的 `fs` / `path` 被 Vite 外部化。带 `connect-src 'none'` 的生产构建探针在 macOS WKWebView 显示了 Express 生成的文字，随后用生产聊天组件验证了完整布局。这个 POC 采用 Express。 [A2UI Agent SDK](https://www.npmjs.com/package/@a2ui/agent/v/0.2.1) · [React Renderer](https://www.npmjs.com/package/@a2ui/react/v/0.16.1) · [官方集成指南](https://a2ui.org/guides/agent-integration/)

每边从各自锁文件执行 `./script/npm.sh ci` 和 `./script/npm.sh run build`。统计 `frontend/dist/assets` 下所有 JS/CSS 字节数，gzip 列是逐文件用 Node `gzipSync` 压缩后求和。基线为 `d3bd238b`。

| JS/CSS | 原始字节 | gzip 字节 |
| --- | ---: | ---: |
| 基线 | 2,018,227 | 546,830 |
| POC | 2,744,231 | 740,461 |
| 增量 | 726,004 | 193,631 |
| Express + Renderer 按需 chunk（含 CSS） | 723,616 | 192,686 |

## 本机复现

```sh
make setup
make check
make smoke
BOT_DEVELOPMENT_IDENTITY=- make build

mkdir -p .cache/a2ui-poc/evidence
BOT_PREVIEW_FIXTURE=a2ui BOT_PREVIEW_LANG=en \
  BOT_PREVIEW_WIDTH=600 BOT_PREVIEW_HEIGHT=785 BOT_PREVIEW_AUTO_CAPTURE=1 \
  BOT_PREVIEW_CAPTURE="$PWD/.cache/a2ui-poc/evidence/wkwebview-fixture.png" \
  bash script/build_and_run.sh --chat-preview
```

夹具通过合成快照驱动生产聊天组件。截图同名 `.json` 中的 `expected.ok` 应为 `true`。核对进行中的正文、闭合后插入的卡片、正文顺序、非法和截断源文、字面代码、用户消息边界，以及完成消息重新挂载的结果。

分别准备独立的 Bot 数据目录、登录过的测试 `CODEX_HOME`，以及配置好模型和 Agent Team 的独立 Caelis Store。用 `CAELIS_BOT_DATA_DIR` 与 `CODEX_HOME` 启动 `bash script/build_and_run.sh --verify`，从隔离 Bot 设置选中对应 Runtime。Caelis 的测试数据目录应在 `runtime.json` 中设置 `caelisStore` 为隔离 Store 的绝对路径。使用下列两份合成输入，各发一次。遇到未知回执时先查原操作。

- Caelis · 请先按名称加载 `chat-a2ui` Skill。用中文普通 Markdown 交代虚构读书会的笔记和提纲已完成、最后一页待校对，今晚只处理最后一页。在正文中放一张带标题的只读摘要卡。资料已给齐，只需回复。
- Codex · Load the `chat-a2ui` skill by name. Write a brief English reply about a fictional garden. Basil and mint are ready. Rosemary needs water this evening. Include the conclusion and next step in ordinary Markdown with one read-only summary card in the middle. All facts are supplied.

在实际聊天面板核对卡片和独立正文；重启同一测试实例，打开同一聊天查看历史。可从隔离实例的 `chat.sqlite` 只读获取助手正文，并与该 Runtime 的最终助手事件逐字比较。合成夹具、真实模型生成、实际 Wails 窗口观察分别记录。`.cache/a2ui-poc/evidence/` 保存本机夹具截图、构建日志和本次试验的合成主题回复，不含账号凭据。
