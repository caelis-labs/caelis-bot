# Cua Computer Use 基础集成

2026-09-28，基础集成候选，尚未发布。此页是当前实现状态；
[原始 POC](bot-control-poc-validation.md)与[依赖调研](computer-use-dependency-audit.md)保留历史证据。
角色控制、公共动作资产、虚拟手柄继续独立交付。

## 产品与协议

按 Runtime 的 `NativeTools` 能力标签确定 Computer Use 的所有者，Worker 保持 Runtime 原生范围。

- Codex 声明 `computer-use` 原生能力标签，直接使用 Runtime 自己的 Computer Use；不注入任何
  `bot_desktop_*`，不扩充其自动批准清单，不启动 Cua 子进程。实际工具可用性、权限与拒绝仍由 Codex 决定。
- Caelis 当前没有此原生标签，由常驻 Bot 自身持有 `bot_desktop_observe` / `bot_desktop_perform`。
- 标签表示能力所有权，不是即时就绪状态；原生工具不可用或权限被拒绝，不触发 Cua 后备路线。

后文的短引用、输入与生命周期契约描述 Bot 提供的 Cua 路径；Codex 原生路径遵循其自己的工具契约。
先列出可见窗口，再选择窗口读取组件名称、角色、值、边界和可用动作；默认不截图。
窗口内容属于不可信数据，不能授予新的操作权限。仅在用户任务需要时操作目标应用。

`perform` 支持 `click`、向指定文本控件 `type`、向指定可编辑控件 `press_key`、向指定内容区域 `scroll`。
模型只使用宿主发出的短引用，不传 PID、原生 token、脚本或任意坐标。
整批至多 8 步，先校验全部参数，执行一次界面变更后返回新观察及未执行步骤。
模型用新目标重新规划；`dispatched` 本身不是效果证明。输入为插入，不隐式清空原文。

窗口引用最长 5 分钟，观察最长 60 秒；重观察使旧观察失效。派发前复核窗口身份和位置。
Cua 原生层继续校验 element token。macOS 只保留选中 AXWindow 的子树，避免将整个应用的
其他窗口/系统菜单混入当前目标。无窗口子树、降级、失效或不支持的操作不能执行。
按完整 content-v1 回执限制内容：文本块加上 Go JSON 编码后的 structuredContent，
为 outcome、receipt_id 等字段预留空间，总计不超过 Caelis 的 32 KiB；另保留 512 KiB 的 Go/MCP 帧上限。
统一覆盖列窗、单窗观察和动作后带 remaining 的结果，计入 HTML 与 Unicode 分隔符转义。
裁剪后的窗口/目标引用同时从宿主目录移除。`remainingCount` 保留全部未执行步骤数；
超限时只移除完整步骤并标记 `remainingTruncated`，不截短待输入文字而改变操作。

文本框只列出实际可用动作：没有 AXPress 时不宣称可点击，可以直接 `type`。
Cua 0.30.2 的 typed TypeText/PressKey/Scroll input 缺少 element token，因此这三项使用它的公开
`callTool` 接口，由宿主固定工具名、窗口和原生目标；模型不能透传任意 Cua 工具。
不从控件坐标猜测点击或滚动位置。
组合键固定使用 Cua 的窗口/控件限定后台 `hotkey`：本机实测 0.30.2 前台 AX modifier
路线把 Cmd+A 送成普通 a，而后台 hotkey 能正确全选并完成替换。宿主不在失败后自动
改用另一条路线；不支持后台快捷键的应用会拒绝。普通单键仍使用明确控件的 press_key。

截图通过 observe 的 `screenshot:true` 按需请求，须当前模型明确支持图片。
每次最多一张、256 KiB；无法提供图片时保留可用元数据并说明 imageUnavailable。
初始化引导和隐私设置均提供辅助功能、录屏及通知入口。权限由用户授予，宿主不修改 TCC。
界面文案说明执行桌面任务时向所连接 AI 服务发送选中窗口的文字、控件和按需图片。

## 宿主、打包与停止

```text
非原生 Runtime 的 Bot 工具调用（当前为 Caelis application callback）
  → Go desktopcontrol.Controller
  → 应用包内 Node --jitless + host.mjs（私有管道）
  → 固定 Cua 0.30.2 原生 SDK
  → 目标窗口的系统辅助功能和输入
  → 新状态 / 可选 image 内容块
```

`resources/computer-use/package-lock.json` 固定 npm 制品及 integrity。
构建下载校验过 SHA-256 的官方 Node 24.21.0，随包安装 Cua 原生组件；运行时不依赖
Homebrew、用户 Node、全局 MCP 或另起监听服务。模型密钥与 Bot bridge 凭据不传给子进程。
所有嵌套原生代码以应用同一身份签名；`--jitless` 避免增加 JIT 或关闭 library validation 的 entitlement。
构建会实际加载签名后的 native binding。正式签名脚本也覆盖这套嵌套组件，正式公证仍须独立验收。

CI 的 PR 构建显式选择 Dev 身份；npm 缓存同时跟踪产品与 Cua lockfile。
`verify-computer-use.sh` 在构建、签名后及只读挂载 DMG 时验证随包许可证、版本、
宿主架构、每个原生二进制的签名/同团队身份/发行时间戳以及外部动态库路径，
用包内 Node `--jitless` 加载真实 SDK，不创建驱动或申请系统权限。
动态库检查区分构建时的 `LC_ID_DYLIB` 与实际加载命令，并检查 `LC_RPATH`；
真实 Mach-O 夹具覆盖自身标识、外部依赖和包含空格的搜索路径，避免误拒绝上游原生模块。
`CaelisComputerUseVersion` 标记防止缺失 payload 被误判为旧包；只有明确允许历史恢复且
标记与 payload 都不存在时，才兼容不含 Cua 的旧 immutable tag。发布凭据仍只在隔离签名 job 使用。

Cua 原生包最低 macOS 13；所捆绑 Node 二进制的 LC_BUILD_VERSION 为 13.5，
因此本实现只在 **macOS 13.5+** 注册此能力。App 本身原有系统支持范围不因此缩小。
Windows 共用宿主/协议边界并保留上游发行路径，尚未打包或原生验证。

工具调用最长 8 秒；传输失败、超时、取消时先立即终止当前子进程再关闭输入管道，
不沿用普通空闲退出的一秒清理宽限期，避免仍在等待的只读校验完成后继续派发输入；不自动重放。
只有新 observe 可以重新创建驱动，perform 不能在失去观察的情况下启动驱动。
用户点击“停止工作”先取消本地桌面调用，再中断 Runtime；结束 Turn 与退出 App 也取消调用。
新 Turn 重新开放观察，旧原生引用在驱动回收后不复用。已发出的单个系统输入不能撤回，
所以停止/传输中断的结果必须按 unknown 处理，而不是假称操作未发生。

## 独立开发身份

普通本地/PR 构建默认 `Caelis Bot Dev.app` / `dev.caelis.bot.dev`。
正式 tag 构建或显式 `BOT_BUILD_CHANNEL=release` 使用 `Caelis Bot.app` / `dev.caelis.bot`。
两者默认数据目录分别为 `Application Support/Caelis Bot Dev` 和 `Application Support/Caelis Bot`，
单实例标识、应用标题、菜单标识、权限与日志分别归属各自身份。
开发启动脚本只退出自身 bundle 路径的进程；不退出安装版，不迁移生产数据或权限。
`CAELIS_BOT_DATA_DIR` 仍可指定独立验收 profile。开发 DMG 名也包含 `Caelis-Bot-Dev`。

## 实际验证

最终能力装配版本使用干净 Dev profile，通过真实聊天连接 Codex App Server，
模型调用原生 `cua_repl` 完成专用测试窗口的勾选、Cmd+A 替换文本、区域滚动与读回。
独立 AX 核对：Only incomplete 为 1，Visible tasks 为 2，文本精确为
`native-codex-e2e-20260928`，Scroll position 为 scrolled，滚动条约为 0.267。
会话中 0 个图片块、0 次 `bot_desktop_*` 调用，进程检查中 0 个包内 Cua helper。
Runtime 的逐次应用访问确认通过 Bot 原有确认卡传递，不增设自动批准。
首次引导已实机检查：辅助功能、屏幕录制、通知三项均可见，已授予的 Dev 权限正确显示。
另一个干净 profile 验证原生确认期间点击“停止工作”：先明确取消尚未答复的 MCP
elicitation，再请求 Runtime 中断；确认卡消失，下一轮正常返回 `stop-recovery-ok`。
旧实现只请求中断，会留下仍等待答复的原生工具并超时；回归已确认修复前失败、修复后通过。
发送取消不等于伪造结束事件，仍由 Runtime 的终态决定可否开始下一轮。

最终 `make check`、`make smoke`、受影响 Go 包 race、原生开发构建与 Dev DMG 验证通过。
本机开发包为 `dist/releases/Caelis-Bot-Dev-0.4.0-dev-macos-arm64.dmg`，约 96 MiB，
Apple Development 签名、嵌套代码验证和 Finder 布局检查通过；不是公证发行包。
提交前复核还验证了独立副本的 CI ad-hoc 签名与真实 SDK 加载，保留 Dev App 的稳定开发签名。
工作流通过 actionlint；远端 PR CI 的结果单独记录，不用本地通过代替。

PR #37 的首轮 CI 在 Xcode 15.4 的 `lipo -verify_arch` 参数解析处失败；
已调整为输入文件在选项之前，重新验证实际 App/DMG 打包。
Review 回归使用产品 Go driver、host 与 Desktop facade，仅替换底层 SDK：
修复前可复现取消后输入，以及 18,000 字符观察超过 37,000 字节的超限回执；
修复后验证取消隔离、正常关闭、ASCII/Unicode/转义文本、长列窗、剩余步骤及可选图片预算。
这些是边界与传输测试，不等同于真实 Caelis 模型 GUI 验收。

在最初统一注入 Cua 的 Apple Development 签名 Dev Bot 下，用户分别授予开发版辅助功能和录屏权限；
系统设置同时显示生产版和 Dev 版两个独立条目。通过真正的聊天输入启动模型，使用包内驱动：

用户后续确认 Codex 已提供原生 Computer Use，因此最终装配不再向 Codex 注入 Cua。
下表 Cua 结果是驱动与 Bot 传输的既有证据，不冒充最终 Codex 原生路线验收。

| 路径 | 证据 |
| --- | --- |
| 元数据闭环 | 模型列窗、选目标、点击 Only incomplete，读回任务数 2；输入 `cua-dev-e2e-20260928` 一次并读回；滚动 Verification rows 后读回 scrolled。独立 AX 检查确认最终状态 |
| 截图依赖 | 上述闭环 8 次桌面工具调用，0 个图片块，无 shell/外部自动化替代产品工具 |
| 可选图像 | 随后单次只读请求返回实际 PNG image 内容块，1000 × 955；模型确认复选框与验证文本 |
| 失败恢复 | 第一轮发现文本框不支持 AXPress；模型报告 unknown 后停止，未重试。修复动作声明后重启专用夹具，再完成全链路 |
| 契约与生命周期 | JS 覆盖失效/移动目标、批量预校验、每次变更后读回、原生错误/读回失败、焦点、图片门控和 Unicode 帧上限；Go 覆盖真实管道取消/回收、仅显式观察重启、Turn 停止及模型图片能力 |
| Runtime 边界 | Cua 模型验证来自改为原生装配之前的 Codex 实验；Caelis #82 content-v1、原生 Host 与合成 provider/Worker 隔离证据见原始 POC，不冒充真实 Caelis 模型 GUI 验收 |

本机原始证据保存在忽略目录 `.cache/computer-use-*`，包含真实会话的内容不进入公共仓库。
测试窗口位于 `experiments/desktop-control/fixture.m`，不随产品打包。
英文 Bot skill 已同步实际工具、可选截图、停止/unknown 恢复、控件动作限制和不向 Worker 委派的边界。

## 后续独立门槛

- 实际工作应用覆盖、无 AX 的自绘画布、浏览器 DOM 精确路由、拖拽和多屏混合 DPI 仍需独立验收。
- Caelis 真实模型 GUI 全链路、Cua 组合键最终适配的模型闭环仍需独立验收；组合键已通过原生 SDK 探针。
- 持续的物理键鼠接管仲裁尚未实现；本轮落实用户停止与逐次检查点，不宣称后台无感控制。
- macOS Intel 与 Windows 原生 E2E、正式 Developer ID 公证及公开安装包验证尚未执行。
- 角色位移、共享空间坐标、手柄/HID、公共角色资产协议不阻塞本片，也未伪装为现有能力。

本次 PR 按调研/协议、Bot 集成、开发身份及 CI 打包组织提交；角色行动与后续联动继续独立交付。
