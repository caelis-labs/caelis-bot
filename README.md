# Caelis Bot

面向普通用户、由 Agent 驱动的轻量桌面入口：**状态栏常驻 + 可缩放桌面宠物 + 按需展开的轻量交互**。

用户通过一个克制的入口表达需求、补充附件、处理审批，并在需要时连接工具。
Session、thread、workspace、协议参数与执行编排属于内部实现，不成为产品导航。

发展目标是具有持续身份、情境化 3D 表达和可信桌面交互的个人助手。当前已接入角色与真实
工作流；已开始接入轻量桌面上下文、六套本地待机、鼠标/工作响应和独立纸飞机的原型。
这些路径仍在开发者自用阶段，尚未完成整体表现力验收。
早期以开发者持续自用和有辨识度的真实演示为目标，复杂桌面理解后置。
短期与长期方向见[产品发展路线](docs/roadmap.md)，具体[交互基线](docs/desktop-behavior.md)。

## 当前状态

P1 桌面宿主已实现：状态栏常驻、透明桌宠、拖动、等比缩放和偏好恢复，
点击角色在其下方打开轻量输入胶囊，屏幕边缘自动避让；点击外部收起。左侧“+”
提供附件及实际可用的插件技能引用；角色右键只提供对话、隐藏，状态栏另有显示、设置、检查更新与退出。
单击角色切换空闲输入框，双击直接打开聊天。65%–160% 连续大小滑杆移到设置窗口。状态栏采用 Caelis 首页图标；位置与任意比例会保存。面板和桌宠可独立收起；
明确退出才结束应用。已完成单屏 Retina 核心交互验证，具体证据与尚未覆盖的桌面
场景见[原生验收记录](docs/native-acceptance.md)。

P2 已接通真实 Codex 对话、流式输出、工具进度、附件、审批、中断、文件结果和历史恢复。
发送接受后收起空闲输入，气泡显示最新回复并可原位审批/停止/确认；可选聊天接近 IM。
一个长期 Bot，无新会话入口；支持应用常驻的定时激活及有限角色反馈，见 [Bot 设计](docs/bot-design.md)。
默认不显示 transcript 或技术会话 ID。附件会复制到本机工作目录：图片走原生图片输入，
普通文件由后端工具读取副本。审批依据实际请求与选项，未知结果不自动重发。
已验证核心工作流；尚未覆盖的登录、复杂 MCP 表单和发行门槛见[后端验收](docs/backend-acceptance.md)。
首个后端为 Codex App Server；未来 Caelis Bot Mode 通过独立适配器接入。
角色路线为 Blender → GLB → Three.js。当前人设采用用户提供的 Caelis 小女孩：
奶油白上衣、鼠尾草绿花裙、花夹与斜挎包；复用已精修脸眼、头发和既有动作骨架。
待机、工作、提醒、点头和庆祝五段动作及跑步/挥手继续可用；火柴人为加载失败回退。
[当前成品合同与来源记录](docs/character-assets.md)保留制作来源和剩余精修项。
制作工程和历史角色试作留在组织私有资产仓库，生产包只带当前角色、通用纸飞机与火柴人，不内嵌人设图或 Blender 源。
聊天中的用户/助手消息均采用气泡，复制按钮只在悬停或键盘聚焦时显示。

## 早期预发布安装与启动

早期包使用 **本地 ad-hoc 签名，没有 Developer ID 签名，也没有 Apple 公证**。
这两项暂不作为 pre-release 门槛，之后再补；第一次打开可能需要你手动信任。
当前实机验证范围是 Apple Silicon / macOS 27，Intel 与更早系统尚未完成原生验收。
程序不携带 Codex、Node、Go 或 Blender；普通用户需要本机可连接的 Codex。

1. 从本项目可信发布来源取得 `.zip` 和同名 `.zip.sha256`，在下载目录核对校验值：
   `shasum -a 256 -c Caelis-Bot-0.0.1-preview-macos-arm64.zip.sha256`（文件名随版本变化）。
2. 解压，将 **Caelis Bot.app** 拖到“应用程序”，双击打开。它常驻状态栏，默认不显示 Dock
   图标或主窗口；点击桌宠输入，右键桌宠或点击状态栏图标打开独立配置。
3. 若系统提示开发者无法验证/无法检查是否包含恶意软件，先取消弹窗，再进入
   **系统设置 → 隐私与安全性 → 仍要打开**，按系统提示确认。该操作只为此应用建立例外。
   参见 [Apple 官方说明](https://support.apple.com/zh-cn/102445)。

已确认下载来源、校验值一致，但仍受下载隔离标记阻止时，也可以在终端仅移除此应用的
quarantine 标记，再启动：

```sh
xattr -dr com.apple.quarantine "/Applications/Caelis Bot.app"
open "/Applications/Caelis Bot.app"
```

若放在 `~/Applications`，相应替换为 `"$HOME/Applications/Caelis Bot.app"`。
此命令绕过该应用的下载隔离检查，不赋予 Developer ID 身份，也不是公证。不要对未知
来源的包执行，不需要全局关闭 Gatekeeper 或 SIP。若校验不一致、签名损坏或系统明确
报告恶意软件，请重新获取包并反馈，不要用重新签名等方式掩盖损坏。

首次连接会优先握手已开放的标准本地 Codex 服务，不可用时自动发现 CLI；也可在
**设置… → 接入运行时 → Codex** 中手动选路径，检测通过后保存。没有 CLI 时按
[Codex 官方安装说明](https://developers.openai.com/codex/cli/)安装并登录。
仅安装/打开 Codex App 不保证它开放可连接入口；Bot 不会启动它。CLI 不要求某个特定
发行版本，按 [App Server 协议兼容性](docs/codex-compatibility.md)检查。

更新前从状态栏明确退出，替换 `.app` 后再打开；当前采用手动更新。菜单“检查更新…”查询公开发布版，显示结果后可打开发布页下载；
没有可下载版本或网络失败时会明确提示，不会自动替换应用。应用偏好、Bot 绑定、
定时任务、草稿及附件选择保存在 `~/Library/Application Support/Caelis Bot/`，替换应用
会保留它们；Codex 登录与原生对话由用户本机 Codex 管理。退出期间提醒暂停，睡眠期间
错过的提醒醒来后合并；系统通知需从“设置 → 桌宠与通知”主动开启。卸载应用不会自动删除这些本机数据。

聊天先显示最近消息，可点击“查看更早消息”继续读取。右键桌宠或点击状态栏后：

- **设置 → 附件存储**：查看副本占用；空闲且无后台工具时，可将 30 天前的副本移到系统废纸篓。
  原始文件、聊天记录和结果文件保留；后续若需要已清理副本，请重新附加原文件。
- **设置 → 诊断**：自行选择位置保存 JSON 报告，供问题反馈。只包含系统/组件信息、连接与工作
  状态、数量统计，不含聊天/草稿内容、文件路径、审批命令或凭据，也不会自动上传。

## 本地开发（macOS / Apple Silicon）

版本基线见 `toolchain.json` 和 `package-lock.json`。脚本优先使用 Homebrew 的
Node 24 独立目录，不修改用户 shell 配置。Go 命令使用 `GOWORK=off`，本仓库独立于
父目录的 Caelis 开发 workspace。

```sh
make setup     # 安装锁定的 JS 与 Go 依赖
make doctor    # 检查已安装工具
make check     # 类型、前端构建、Go 静态/生命周期检查、macOS/Windows 共享核心编译守卫
make check-portability # 仅共享核心/明确不支持的启动入口，不代表原生 GUI 可用
make smoke     # Codex initialize；成品哈希、GLB 验证、Three.js 解析与动画更新
make smoke-adapter # 新 Go adapter 初始化、读取鉴权状态、关闭自有子进程；无模型请求
make smoke-workflow # 显式调用真实模型/工具；仅使用独立临时目录和合成附件
make schema    # 使用开发基线生成 native types/schema，并比对已纳入仓库的消费子集
make run       # 构建并启动本地 .app
make package   # 构建 ad-hoc 预发布 ZIP 和 SHA-256；不公证、不上传
```

`make dev` 仅启动前端预览。原生构建与运行的唯一入口是
`script/build_and_run.sh`，Codex 项目 Run 按钮也使用它。

`make smoke` 不创建 Codex 会话、不发起模型调用。公共构建、测试与运行只消费版本化成品，
不需要 Blender、私有仓库或访问凭据。角色与服装在组织私库独立迭代，经 CI 验证后自动向
主仓库发起成品更新 PR；详见[成品合同与更新流程](docs/character-assets.md)。

## 文档

- [产品边界](docs/product.md)
- [架构与协议草案](docs/architecture.md)
- [跨平台实现基线与支持范围](docs/platform-baseline.md)
- [产品发展路线、竞争力假设与阶段验收](docs/roadmap.md)
- [最小桌面世界、待机/互动与独立道具设计](docs/desktop-behavior.md)
- [整体实现计划与验收](docs/implementation-plan.md)
- [准备状态与已验证范围](docs/preparation-status.md)
- [P1 原生验收与复现](docs/native-acceptance.md)
- [P2 后端验收与限制](docs/backend-acceptance.md)
- [工具与资产流程](docs/toolchain.md)
- [调研资料、生态布局与截图](docs/references.md)
- [继续开发 / Handoff](docs/handoff.md)
- [并行建模的首版动作与 GLB 交付约定](docs/character-actions.md)

## License

应用代码采用 [Apache-2.0](LICENSE)。内置角色、头像和品牌图标采用独立的
[Caelis Character Asset License](ASSET-LICENSE.md)，允许在本应用及其 fork 中使用、开发测试和
随应用分发未修改的成品；不开放制作工程，也不授予独立销售素材或用于其他产品的权限。
具体文件范围与版本见 [成品清单](resources/character-pack.json)。火柴人与通用纸飞机为 Apache-2.0。
第三方依赖保留各自许可。
