# Headless / Web 本地交付验收（2026-10-20）

## 交付结论与依据

基于远端 main / Bot v0.17.1 `2d94f101638a46321209c1ad5ebe2784d68f7d4c`；交付前再次 `git ls-remote` 得到相同 SHA。分支 `feat/headless-web-20261020`，独立树 `/private/tmp/caelis-bot-headless-web-20261020`。主树仍为 `95882f1` 且干净，未作为施工基线；并行恢复 worktree 未修改。没有 push、PR、合并或发版。

## 随包 CLI 与首启修订（本节为当前交付）

体验服务现运行于 **Rocky Linux 10 arm64**，入口仍为 **http://127.0.0.1:19317**，本机无需配对。使用独立 `/tmp/caelisbot-bundled-20261020` HOME/profile，未配置真实模型密钥。随包 Caelis v0.72.2 实际启动 Core；真实 Codex v0.153.4 仅作为该测试 HOME 的外部安装，不进入发行包，未登录。

```sh
/private/tmp/caelisbot-local-20261020/start.sh
/private/tmp/caelisbot-local-20261020/stop.sh
```

启动脚本管理本次专用 Rocky 服务、宿主 loopback SSH 转发和 relay；返回后入口继续运行。停止脚本仅停止这些进程及此独立 Caelis Store，保留 profile。已实际测试停止/再启动。正式 App 主进程 PID 28018 未重启；隔离 Onboarding Dev App 对照后已停止。

### 根因与生产路由证据

- [main.tsx](../frontend/src/main.tsx) 的 `surface=settings` → [Settings.tsx](../frontend/src/Settings.tsx) 的 `section=setup` → [BotSetup.tsx](../frontend/src/BotSetup.tsx) 的 `stage=runtime` → [RuntimeSettings.tsx](../frontend/src/RuntimeSettings.tsx) 的 `RuntimePreparation onboarding`。因此截图中的缺失页仍是生产恢复路径，不能以外观判为废弃页。
- 正式安装 App 只读检查为 Bot 0.17.0，`Contents/Helpers/caelis version --format json` 为 v0.72.2 / `fa598b00db7d6635086d3388f4d4c9c335642cfd`；源码施工/发布对照仍是 Bot v0.17.1 的远端 main，二者不能混称。只读对照正式 0.17.0 发布 commit `e65385fa12852381a08530541ea6665c1fb08b64` 与 0.17.1：上述四个首启路由文件、CLI 打包脚本与 caelisruntime 目录均无差异。
- [build.sh](../script/build.sh) 调用 [caelis-runtime.py](../script/caelis-runtime.py) 暂存固定版本；[bundled_path_other.go](../internal/caelisruntime/bundled_path_other.go) 原来仅识别 macOS `.app`。旧 headless build/Docker 只复制 Bot binary；Web 又把 onboarding 的 `ApplySetup(prepare)` 改为 `InspectSetup`。这是缺失页出现的三项实际原因。
- 现在 Linux amd64/arm64 归档携带 Helpers/Resources，延用同一 release pin、归档/二进制 SHA-256、固定成员提取和架构校验；[setup_preparation.go](../internal/app/setup_preparation.go) → `PrepareBundled` → Core `service start` 的原生命周期没有改动。
- App 的 Codex [安装器](../internal/runtimeinstall/codex.go)仅支持 macOS；[登录](../internal/backend/codex/setup.go)走标准 App Server，授权 URL 由原生 Host 打开。Web 保留缺失/检测、API Key、主机侧 device-auth 说明，不伪装远端 localhost 回调或本地文件选择器。`codex login --help` 实测支持 `--device-auth`；未开始真实登录。

### 本轮真实验收与边界

| 验收 | 结果 |
|---|---|
| Rocky 空 HOME、无系统 Caelis | 原生 Linux 构建后真实 HTTP `prepare` 返回 bundled=true / v0.72.2 / state=models / serviceState=running；模型数量 0，没有假认证 |
| 外部 CLI | 默认发现仍选随包；显式路径选外部副本；操作前后 SHA-256 不变，无覆盖安装 |
| Codex 缺失 / 已安装 | 真实 Linux 0.153.4：missing → 标准握手后 login；Web 重新检测能在两状态间恢复；未调用模型 |
| 原生与 Web 首启 | 两者真实进入同一服务列表、DeepSeek API Key/模型/参数表单；Codex 缺失、已安装未登录逐页截图；Web 明示路径属于服务端 |
| 宿主 loopback | relay → 宿主 19318 SSH → guest 127.0.0.1:19321；上游 `--require-pairing`，本机 proof 仅由 relay 持有。实测 200 / 伪造 XFF 403 / 跨站 403 / 原始转发端口 401 |
| Docker | 镜像已带固定 CLI、bubblewrap 和 CA；CLI 版本真实读取正确。默认 Docker 限制内部 namespace/proc，Core 保持拒绝启动。用户选择 Rocky，**Docker 完整验收未通过**；未部署放宽 systempaths 的持续容器，未关闭 Core 沙箱 |
| 发行架构 | arm64 为真实 Rocky 构建与运行；amd64 为交叉构建、归档及 ELF/SHA 校验，不宣称 amd64 实机 |

本轮截图位于 `output/playwright/runtime-bundled-20261020/{native,web}`。两者亮色、逻辑窗口约 600×660；原生 Retina 截图为 1200×1320。`01` 首启、`02` 服务列表、`03` 实际模型表单、`04` Codex 缺失、`05` Codex 已安装未登录，另有 Web 主机路径弹窗与 Docker 失败恢复画面。没有使用 headless golden 冒充原生窗口。

清理了仅由开发 HTML 引用、已不在生产链路中的 `RuntimeWorkspace`、旧 `runtime-settings-preview.html` 及四个专属 preview 文件，删除 public-tree 中相应过时入口许可，修正文档。保留仍覆盖生产逻辑的 client fixture、模型/恢复测试以及 `RuntimePreparation` 的真实缺失分支；不把有效恢复测试当废弃代码删掉。

新增公共接缝仅为 CLI **自身发行目录**识别，优先级仍是显式路径 > 自身随包 > 系统发现，损坏包不静默换 CLI。其余改动属于打包、共享组件的 Web capability 分支和文档。未修改 task/schedule/receipt/recovery owner。bot-core 已有 surface/host 边界指导，本轮没有新增 Bot 工具或改变授权/任务语义，无需再更新 skill。

### 前一阶段验收（历史证据，非当前 Docker 结论）

下文保留 `a9382dcf` 的聊天、设置、附件和 fixture 行为证据。该阶段 Docker 没有携带 Runtime，不能用其 HTTP/UI 成功替代本轮 Core 沙箱验收。部署说明见 [headless-web.md](headless-web.md)。

## 逐页视觉对照

截图位于工作树 `.cache/web-acceptance/{before,after,native}`；全部为实际浏览器/原生窗口或明确命名的原生 fixture 截图，未用渲染图替代。目录未纳入 Git；[manifest.json](../.cache/web-acceptance/manifest.json) 保存 SHA-256。Web 使用亮色、1040×708 CSS viewport；原生设置实际约 1040×740（含标题栏）、Retina 2×。比较布局、密度和交互，不把原生材质/标题栏差异作为缺陷。

| 页面 | Web 截图 | 原生对照 | 验收结论 |
|---|---|---|---|
| 修改前 | `before/user-settings.png`、`settings-capabilities.png` | — | 保留用户截图及修改前占位状态 |
| 首次启动 / 无 Runtime 聊天 | `after/chat-runtime-missing.png` | `native/first-run.png`、`chat-runtime-missing.png` | 无配对；齿轮可退出首次引导进入设置；输入区留在 viewport 内 |
| 常规 | `after/general.png` | `native/general.png` | 共享语言与任务限额；保存 3→4 后离页及实际进程重启回读；Web 隐藏自启、终端及快捷键分组，桌面保留 |
| 外观 | `after/appearance.png` | `native/appearance.png` | 相同角色预览、主题与内容包 UI；Web 只隐藏桌宠尺寸 |
| 连接与模型 | `after/connections.png`、`codex-connection.png` | `native/connections.png`、`codex-connection.png` | 共享 Bot / 后台工作模型路由；无 Runtime 正常显示设置；Web 引导在执行主机安装/登录 |
| 连接弹窗 | `after/machine-dialog.png` | `native/machine-dialog.png` | 共享 SSH 连接表单、焦点/取消行为；未建立真实 SSH 连接 |
| 消息渠道 | `after/channels.png`、`telegram.png`、`weixin.png` | 同名原生截图 | 渠道与子页正常；没有输入真实凭据、扫码登录或发送消息 |
| 远程机器 | `after/machines.png` | `native/machines.png` | 共享列表及添加入口；Web 隐藏打开原生终端按钮 |
| 插件 | `after/plugins.png` | `native/plugins.png` | 共享目录/详情；Web 隐藏 DTW 与 Linux 当前不支持的 OAuth 项，桌面保留 |
| 关于与更新 | `after/updates.png` | `native/updates.png` | 共享品牌/设置行样式、发行页及诊断导出；主机侧更新 |
| 聊天 / 问题 / 审批 | `after/chat.png`、`question.png`、`approval.png` | `native/question-fixture.png`、`approval-fixture.png`、`approval-window.png` | 同一 History / QuestionPrompt / ApprovalCard；真实原生 WKWebView fixture 对照，非真实模型 |
| 文件 | `after/file-preview.png` | 复用 ArtifactLink / SettingsDialog | 实际上传文本文件到 Linux，发送后读取；预览/下载字节一致；修复隐藏 portal 与透明背景 |
| 任务 | `after/tasks.png`、`task-result.png`、`tasks-empty.png` | `native/task-fixture-controls.png` | Web 原任务 ID 只读列表/状态/记录/结果；不新建任务、不创建 PTY。原生浮层截图限制见下 |
| 窄屏 | `after/mobile-chat.png` | 不宣称手机原生验收 | 390×844 浏览器：设置可返回聊天，输入区完整可见 |

正常 Web 聊天页仅保留有无障碍名称/tooltip 的任务与设置图标；设置内使用原返回聊天。没有重复品牌文字导航、无效功能说明或空分组。对照发现并修复了输入框裁切、设置引导无出口、文件弹窗挂到隐藏页面、任务弹窗缺少共享样式这四类实际问题。

## 行为与网络证据

| 检查 | 实际结果与证据边界 |
|---|---|
| Linux | Rocky Linux 10 arm64 原生 Go 1.26.8 编译并运行；Docker 内 CGO=0 binary 实际运行，无显示服务 |
| 真 loopback | Rocky 新 profile 真实监听 127.0.0.1：bootstrap 200 / HttpOnly cookie，无配对 |
| 容器与宿主 | `docker inspect` 仅 `127.0.0.1:19318→9317`；宿主 relay 19317。浏览器从空 cookie 成功打开 |
| 转发边界 | 实际 HTTP：relay 正常 200；伪造 X-Forwarded-For 403；直接容器 NAT 入口 401；伪造本机证明 401 |
| 私网认证 | VM 私网浏览器使用一次性配对；重启 fixture 后原 cookie/历史恢复。单测覆盖私网 peer、代理 peer、外部 Origin、DNS rebinding、CSRF、HTTPS Secure cookie、混合 origin 拒绝 |
| 流式与重连 | 16 次浏览器采样得到 8 个不同输出阶段；离线→上线→重载后原消息仅 1 条 |
| 写入丢包 | 让真实 Submit 请求完成后丢弃 HTTP 响应：提交 1 次，读取原 operations 回执 1 次，无新 ID 重放 |
| 多 tab | 第二 tab 默认只读，显式接管；旧 tab 输入区变 inert，服务端 epoch 拒绝旧写入；关 tab 不停 Bot |
| 问题 / 审批 | fixture 原绑定回答后卡片隐藏；原 approval choice 处理后收起；没有副作用操作或真实系统权限申请 |
| 任务 | fixture 的合成投影读取既有 Task/TaskConversation 形状；真实服务空列表正常。未验证真实 Worker 执行 |
| 文件 | 实际 Linux upload→原 DraftFiles→Submit；下载内容为 `CaelisBot Linux Web fixture result`；测试覆盖路径穿越、归属与清理 |
| 写入落盘 | 同 ID/同参数返回原结果；不同参数冲突；重启 pending 不再 dispatch；完成落盘失败仍保留 pending，不把内存结果升级为成功 |

## 门禁与边界

- `make check`、`make smoke`、`make build` 通过；Go race（websurface / headless / localproxy）通过。仓库没有 `make commit-check` 目标，没有虚报该门禁。
- 桌面 recovery mounted-component 回归通过：五类失败路径保留原恢复行为；原生隔离 Onboarding App 实际逐页操作，不接触正式 App/profile。临时 Dev/预览实例验收后停止。
- `script/task-dock-native-test.sh` 在现有 `animateLoading` 旧字段处编译失败。仅临时脚本副本移除过时引用后启动原生预览，记录控制窗口；**未取得可比较的原生任务浮层截图**，不将 Web fixture 或离屏 golden 冒充它。未改原脚本或任务公共业务。
- 未连接真实模型/账户、未实际投递 Telegram/微信通知、未执行真实 Worker、未安装登录 Tailscale、无 Windows WSL 实机。Tailscale 为可证明的网络契约测试；不能据此宣称原生 WSL 或 Linux GUI 验收。
- 首版不含 Web PTY、Linux/WSL DTW、桌宠窗口/系统快捷键、账户平台、多租户、Web 自动更新。Web 无效入口隐藏；服务端能力边界保留。
- 未动 schedule / task / receipt / recovery owner，也未接入未交付的 async/MCP 实验联调。Notebook 数据格式不迁移。

公共接缝仅：`desktop.ts` 选择 transport；现有共享组件按 surface 隐藏原生入口；`SettingsDialog` 可选 CSS scope（默认行为不变）；附件 owned staging 默认 false；`Application` 增加只读任务委托；根入口增加 serve/pair。新网络模块不拥有第二个 Core/Runtime 或 Worker。

## Web feedback follow-up (2026-10-20)

Based on local `6715109a`, without merging remote `main` or #204. Remote `main`
was observed at `e121d9e9d279db31b10166ac40d743698c20ef5d`; its changes are outside
this worktree. The production App and the task-recovery worktree were not changed.

Root causes verified against the running Rocky service:

- The later shared `.settings-page` rule in `surface.css` set `margin:0` with a
  1040px maximum. A wide browser therefore left all surplus space on the right.
  Web now centers the existing grid and aligns the breadcrumb with it; field
  widths still use the App controls. Narrow navigation remains a surface adapter.
- The initial Web setup redirect was only a one-time `SetupOverview` read. The
  gear, settings return and subsequent navigation could bypass it. A Web navigation
  guard now reads both setup and durable introduction state, canonicalizes URL
  history, fails closed, and ignores late reads. Setup mounts only `BotSetup`;
  chat/settings are not mounted until the guard allows them. Selecting a Runtime
  alone does not complete introduction. Normal in-guide Back stays local.
- `SetupCatalog(models)` returned three HTTP 503 responses because Rocky loopback
  had no Magpie listener. The browser recorded no JavaScript exception stack.
  `ConnectionWizard` mapped this ordinary read failure to `<Recovery>` without
  `onRetry`, so it displayed a window-restart card inside the form. It was not
  a proven React boundary crash. Failed/empty catalogs now stay in the form with
  a bounded read retry and preserved draft; null wire lists are valid empty lists.
- The copied diagnostics described the default, unselected **Codex conversation**:
  `hasRuntimeChoice=false`, onboarding and introduction required, offline, bound
  false, schema baseline 0.153.4 and zero logged events. Those were not diagnostics
  of the independent Caelis/Magpie setup request. Worker retirement counters are
  retained in the optional technical export, not copied into feedback prose.
  Shared recovery copies an allowlisted summary of issue, time, page, connection
  scope, classification and version. It never includes arbitrary errors, URLs,
  credentials, prompts or backend fields. Web fatal recovery says reload page.

Shared seams: `Settings` has an optional surface navigation callback; the existing
connection wizard and runtime client handle local read failures/empty catalogs;
`SurfaceBoundary` uses a shared feedback formatter. No changes to task, schedule,
Worker recovery, receipts, backend ownership, Core or Codex policies. No Bot-core
skill change: these are UI navigation, diagnostics and deployment fixes, with no
new tool or Bot workflow.

Evidence is in `output/playwright/web-feedback-20261020/` and a versioned copy in
Notebook. Browser setup/navigation and the successful model list use the live
Rocky service. Error/empty HTTP responses and completed-setup projection used for
settings screenshots are explicitly browser fixtures, not server-state edits.
The mounted shared-component recovery fixture separately checks finite reads,
form identity, retry without connection writes, and no private error leakage.
Native screenshots come from the real isolated Onboarding App with its own HOME
and profile: startup, actual Magpie models, a genuinely unreachable port, copy
confirmation and restored models. They are not browser golden images.

The final health probe also found that bootstrap treated every non-2xx response
as a pairing requirement. Only HTTP 401 now opens pairing; 429/503 and transport
failure show a local reconnect action. Browser fault injection verifies retry
without a pairing field. Authentication/session limits are unchanged. Disposable
probe sessions must log out through their own authenticated session; do not clear
`sessions.json` or evict another browser to make a test pass.

Final gates: `make check` (246 Node tests plus Go suites), `make smoke`,
`make build`, `make build-headless`; no `make commit-check` target exists.
The Rocky model gateway proof used one real `deepseek-flash` request: 35 input
and 2 output tokens, response `OK`. This is a gateway inference check, not a
claim of full live Bot/Worker/channel acceptance. Default Docker nested-sandbox
compatibility, real WSL and real Tailscale device acceptance remain outside this
follow-up's evidence.
