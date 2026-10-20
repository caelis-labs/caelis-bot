# Headless / Web 本地交付验收（2026-10-20）

## 交付结论与依据

基于远端 main / Bot v0.17.1 `2d94f101638a46321209c1ad5ebe2784d68f7d4c`；交付前再次 `git ls-remote` 得到相同 SHA。分支 `feat/headless-web-20261020`，独立树 `/private/tmp/caelis-bot-headless-web-20261020`。主树仍为 `95882f1` 且干净，未作为施工基线；并行恢复 worktree 未修改。没有 push、PR、合并或发版。

实际产品服务运行在 OrbStack Docker Linux，浏览器入口 **http://127.0.0.1:19317**，新浏览器会话免配对。此测试 profile 没有模型凭据和 Runtime，能进入首次连接、聊天、全部可见设置与空任务列表。可用本机脚本：

```sh
/private/tmp/caelisbot-local-20261020/start.sh
/private/tmp/caelisbot-local-20261020/stop.sh
```

`start.sh` 前台运行 relay（Ctrl-C 只停止浏览器入口）；`stop.sh` 停止入口及专用 Bot。脚本仅管理本次专用 relay 和 `caelisbot-headless-web-20261020-delivery` 容器，数据卷保留。实际聊天流式、问题审批和任务投影使用**明确标记的 Linux fixture**验收，不等同真实模型或 Worker 执行。部署说明见 [headless-web.md](headless-web.md)。

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
