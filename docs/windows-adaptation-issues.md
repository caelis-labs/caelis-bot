# Windows 适配缺陷与验证台账

基线：`95882f1cab67605403d03f8446a128cda8d8eee8`，2026-10-16，Bot 0.14.0。
环境：Windows 11 x64 build 22631，普通用户；Go 1.26.8、Node 24.21.0、GOWORK=off、CGO_ENABLED=0。
本台账保留主线基线复现，并跟踪修复证据。这里的 P1/P2 是缺陷优先级，不是[实施方案](windows-adaptation-plan.md)中的阶段编号。
未验证平台或测试前置条件错误不能称为 macOS 回归。

## 修复进度（2026-10-17）

| 条目 | 当前状态与边界 |
| --- | --- |
| B01/B03 | 已补 Windows ACL；自有状态与 Core 外部凭据合同分开。迁移读写、权限反例、原生 Core 发现及 Bot 握手通过；全量 Go 回归通过；重解析点拒绝 fixture 用 NTFS 联接，不计为文件 symlink 特权覆盖。 |
| B02 | 已使用 Windows PE EXE 校验；Unicode/空格、真实 EXE 与伪造文件反例通过；真实 v0.72.2 的 Inspect/Manage/Probe 通过。 |
| B04 | 已采用普通用户 NTFS 目录联接，保留原清单与按需读取；投影/升级/碰撞/Worker 隔离合同通过。两 Runtime 完整模型驱动 Skill 加载仍待验收。 |
| B05/B06 | 已修复路径判定顺序和 CRLF 解析，原失败与新增换行测试通过；完整 UI 制品和路径边界仍按关闭条件验收。 |
| B07/B08 | 已修复解引用顺序，Wails pin 对齐 beta.28；未回退依赖。 |
| B09 | 已更新文档入口和 DTW 当前 pin；其他历史交互说明继续随外壳实现整理。 |

新发现的验证问题：OAuth relay fixture 使用 `/` 提取 Windows 文件名导致桥拒绝并等待；已改为
`filepath.Base`。另有一次旧桥在替换后提前关闭，独立重跑及连续 10 次未复现，保留观察，不推定为已解决。
连接安装 fixture 的 `/tmp/test-agent` 和终端 fixture 的 shell 文件改为本平台路径/EXE。
全量 Go 回归现已通过。真实 Runtime、原生 GUI 和精确发行制品仍需独立验收，未 blanket skip。

## 已确认的功能缺陷

### B01 · P1 · 文件凭据保存成功后无法读回或删除

- 位置：`internal/secretstore/file.go` 的 `prepare/checkDirectories/read`。
- 复现：新临时 FileStore，Save synthetic 值返回 nil；随后 Load/Delete 都返回 `unsafe_secret_directory`。
  `TestFileStoreMigratesOnceAndSeparatesConsumers` 独立重跑同样失败。
- 原因：在 Windows 上以 `Chmod(0700)` 建目录，再要求 `Mode().Perm()==0700`；文件还要求 `0600`。
  该判定不是 Windows ACL，不能完成自己的写后读合同。
- 影响：当前主线 Telegram、微信、插件连接均消费 FileStore；不能因保存按钮返回成功宣称凭据可用。
  当前代码也未提供对应的原生 ACL 保证；本轮没有据此声称发生过真实凭据泄漏。
- 处理方向：在原 FileStore 下实现 Windows 私有目录/文件保护与验证，保留 Mac POSIX 检查及迁移原子性。
- 关闭条件：普通用户写/读/删/重启恢复、消费者隔离、迁移失败保留旧项、重解析点与权限拒绝测试均通过。

### B02 · P1 · 手选 Windows EXE 被当成不可执行文件

- 位置：`internal/caelisruntime/runtime.go:Find`，以 `i.Mode()&0111 == 0` 拒绝文件。
- 复现：探针对**当前正在执行的 Windows Go EXE**调用 Find；mode 为 `0666`，得到
  `Caelis path is not an executable file`。该检查发生在任何 Caelis 握手之前。
- 影响：手选 Caelis 程序路径失败；不能将“PATH 能发现”作为显式路径选择通过的证据。
- 处理方向：平台执行文件验证与原生版本/协议探测，拒绝不允许的脚本包装或不可执行输入；
  保留 Mac 的可执行位校验，不用直接删除所有平台检查解决。
- 关闭条件：真实 `.exe`、损坏文件、中文/空格路径、显式覆盖与内置发现优先级均验证。

### B03 · P1 · Caelis 私有绑定状态在 Windows 被自身权限检查拒绝

- 位置：`internal/backend/caelis/state.go:privateDir/privateRead/privateWrite`。
- 复现：`TestCommandObservationIsOptional` 独立运行，返回“私有连接目录权限不安全”；
  全量 suite 中配置、callback、Worker 等多项测试也因此失败。
- 原因：POSIX `Perm()&0077` 检查直接用于 Windows。不能据此推断 Control 协议或上游 Caelis 故障。
- 处理方向：共享私有落盘接口、Windows ACL/owner/重解析点验证；不更换 Store/绑定来绕过问题。
- 关闭条件：原状态可靠写后读、未知请求恢复、权限损坏拒绝、断电/替换失败保存原身份；Mac 原测试通过。

### B04 · P1 · Codex Skill 投影需要普通用户没有的 symlink 权限

- 位置：`internal/backend/codex/plugins.go:projectCodexWorkspace` 中直接 `os.Symlink`。
- 复现：`TestBotProjectProjectionAndWorkerIsolation` 在本机失败，错误为
  `A required privilege is not held by the client`；同一路径是产品安装 Skill 的代码。
- 影响：Bot/插件 Skill 无法投影到受管理的项目入口，进而影响 Codex 连接与插件使用。
- 处理方向：Windows 使用 NTFS 目录联接，保留版本清单、原始相对引用和按需读取，
  保留来源/撤销/冲突检测与渐进加载。不能要求管理员/开发者模式或给普通 Worker 注入 Bot Skill。
- 关闭条件：无 symlink 权限的新用户安装、升级、停用、外部碰撞、Worker 隔离和两 Runtime 加载均通过。

### B05 · P1 · Windows 盘符链接被误认成 URL，附件不显示

- 位置：`internal/backend/codex/projection.go`，assistant 链接在 `filepath.IsAbs` 前检查 `url.Parse(...).Scheme`。
- 复现：`TestKnownLocalArtifactHasReadableTextAndOpaqueAction` 和
  `TestBoundTemporaryArtifactCanBeExplicitlyLinkedButPathAloneCannot` 独立重跑失败：`C:\...` 结果没有 Artifact。
- 原因：盘符 `C:` 被 URL parser 当作 scheme，已绑定本地文件被提前跳过。
- 处理方向：明确区分原生绝对路径与网络 URL，仍要求 Work 范围或已注册的临时文件原生绑定。
- 关闭条件：盘符、正反斜线、Unicode/空格、行号、允许的 UNC、URL 与恶意/未绑定路径反例；
  不能修成任意模型生成路径都能打开。

### B06 · P1 · CRLF 检出令 Bot Skill 元数据为空

- 位置：`internal/botskills/skills.go:metadata` 只识别 `---\n`；`.gitattributes` 只为 vendored plugins 设置 `-text`。
- 复现：本机 `core.autocrlf=true`，`git ls-files --eol internal/botskills/skills/bot-core/SKILL.md`
  返回 `i/lf w/crlf`；`TestBundleInstallsReferencesAndExposesOnlyMetadata` 报 `invalid bundled metadata`。
- 影响：Windows 本地构建 embed 了 CRLF 的 Skill，Codex metadata guide 得不到正确名称/描述。
- 处理方向：解析兼容 LF/CRLF，并对需逐字节校验的第一方资源明确定义检出规则；不全量 renormalize
  第三方哈希目录或修改用户全局 Git 配置。
- 关闭条件：两种换行、干净检出构建、包内哈希及 Skill 完整引用测试通过。

## 已确认的测试/文档问题

### B07 · P2 · 测试先解引用状态，掩盖前置加载错误

`internal/backend/caelis/command_followup_test.go:179` 在 `loadBinding` 返回后，先访问
`s.state.Views["main"].CommandCaughtUp`，然后才检查 err。Windows 的 B03 导致这里 nil panic，
提前终止该包后续用例。应先检查加载错误，再访问状态；该 panic 是测试代码问题，不等于产品进程已崩溃。

### B08 · P2 · Wails pin 与实际依赖不一致

`toolchain.json` 仍写 `v3.0.0-beta.23`，`go.mod` 已是 `v3.0.0-beta.28`。
`docs/release.md` 要求依赖变化时同步 pin。应统一声明并核对构建实际消费，不回退已合入依赖；
本轮未证明这种元数据不一致会单独造成 Mac 构建失败。

### B09 · P2 · 历史说明仍指向退役交互/协议

- `docs/windows-11-handoff.md` 仍以 v0.8.0、DTW rc.3、signed MSIX 为未来路径；已加入口说明转到新方案。
- `docs/caelis-integration.md` 的“使用入口”还说“不捆绑 Runtime”“保存后重启”，
  与本文件后面的内置 CLI 章节及当前 `architecture.md` 的热切换合同冲突。
- `docs/ui-design.md` 部分 onboarding 文案仍为 introduction 优先；当前 `product.md`/`setup-stage.ts`
  要求 Runtime 优先、介绍最后提交。
- README 仍描述旧单击输入/双击聊天并写 DTW rc.4；当前实现与 pin 已变。

方案和后续实现以当前产品合同/代码为准；这些文案单独修正，不借机改变已验收 Mac 行为。

## 主线基线缺口（实现进度见文件顶部及实施方案）

| 缺口 | 当前源码证据 | 归属 |
| --- | --- | --- |
| Windows 原生外壳 | `desktop/runtime_unsupported.go` 明确在 Windows 返回未实现 | 原生窗口/托盘阶段 |
| 私有 IPC、Codex 自有进程 | `localipc/ipc_unsupported.go`、`backend/codex/process_unsupported.go` | 基础能力；不能用空成功替代 |
| DTW 存活归属锁 | `plugins/owner_other.go` 返回 false；`launch.go` 的 guard 使用该结果 | Computer Use 前置；打包校验不覆盖 |
| OAuth 与跨进程刷新锁 | `plugins/secret_other.go` 的 `oauthNativeSupported=false`；`oauth_lock_windows.go` 返回 ErrUnavailable | 插件连接阶段，需与新 FileStore 一起完成 |
| 内置 Caelis Windows 制品 | `resources/caelis-runtime/release.json` 仅 darwin-arm64，`bundled.go` 仅识别 `.app/Contents` | P1 验证上游制品与服务执行路径 |
| 本地任务终端 | `taskterminal/script_windows.go` 明确 unsupported | Windows console/helper 与原任务 attach |
| 手动截图、通知、自启动、更新、Setup/ZIP | 当前主线没有旧分支的 Windows 原生实现 | 从备份选择性重建并重新验收 |
| SSH/情境行为 | 当前 Mac/Linux 原生路径未形成 Windows 产品闭环 | 已允许非核心后置，仍需明确能力状态 |

## 基线检查与证据边界

前端构建通过；以下 Node contracts 共 **77/77** 通过：

```powershell
node --test script/ci-scope.test.mjs script/release-version.test.mjs script/setup-stage.test.mjs script/runtime-settings.test.mjs script/chat-presentation.test.mjs script/chat-observation.test.mjs script/native-call.test.mjs script/attachment-menu.test.mjs
```

`go test -timeout 90s ./...` 在 CGO 关闭的本机基线上有 **14 个失败包**：app、backend、backend/caelis、
backend/codex、botskills、diagnosticlog、machines、notebook、plugins、remotework、screeninput、
secretstore、tasks、weixin。失败不能笼统统计为 14 个产品缺陷：包括 B01–B06、未实现接口、
POSIX mode 断言和不能创建测试 symlink 的前置失败。
已有 desktop/localipc/taskterminal 的通过项包括 unsupported 合同，不代表功能已能运行。

用于隔离根因的命令（同样设置 GOWORK=off、CGO_ENABLED=0）：

```powershell
go test -count=1 -timeout 45s ./internal/secretstore ./internal/botskills ./internal/backend/codex ./internal/backend/caelis -run '^(TestFileStoreMigratesOnceAndSeparatesConsumers|TestBundleInstallsReferencesAndExposesOnlyMetadata|TestBotProjectProjectionAndWorkerIsolation|TestKnownLocalArtifactHasReadableTextAndOpaqueAction|TestBoundTemporaryArtifactCanBeExplicitlyLinkedButPathAloneCannot|TestCommandObservationIsOptional)$'
```

实际运行的 EXE 探针和 FileStore 探针仅使用当前测试进程及 synthetic 字符串，未读取真实凭据。
日志/探针保存在 `.cache/windows-realignment-20261016/`，仅结论进入 Git 文档。
`make check/smoke/build` 的完整 Mac 路径、Mac UI、Windows 新版 GUI 与升级均未在本轮执行。

后续先补原生权限/IPC等前置，再重跑级联失败；对 POSIX 专属测试明确平台合同并增加原生对等用例。
不能删测试掩盖产品缺陷，也不能靠管理员运行掩盖普通用户的 Skill 安装问题。
旧分支的截图保存最终点击、通知冷启动、GPU 终端缩略图、Win10/Server 和公开升级验收仍是历史未完成项，
必须在新版本重新验证，不按“旧版曾经通过”关闭台账。

### B10 · P1 · Codex 的 Windows AF_UNIX 终端入口被 POSIX 前缀校验拒绝

- 复现：真实 Windows 自有 App Server 入口为 `unix://C:/.../runtime.sock`，
  `TerminalArguments` 只接受 `unix:///`。新增 `TestWindowsTerminalArgumentsPreserveNativeSocket`
  在修改前稳定返回 `invalid Codex terminal target`。
- 修复：Windows 按本机绝对盘符路径校验 socket，拒绝相对/网络/其他协议；输出统一正斜线。
  Mac 的原前缀判定保留，工具参数不经过 shell。任务身份与原入口保持不变。
- 关闭边界：参数反例与原生 launch fixture 已纳入，真实 Codex 任务终端验收仍待完成。

### 原生走查观察 · 截图快捷键冲突与终端缩略图

当前机器 F1 由其他截图工具占用，按 F1 唤起的是外部截图工具；Bot 的显式 `--capture` 单实例入口
成功打开自有截图编辑器。不能记录为 Bot F1 验收通过。保留原生注册失败状态，并需检查设置中的
冲突提示与替换快捷键可用性；不会抢占或关闭用户现有截图工具。

实际 conhost + 本次构建的 caelis-terminal.exe 已通过一次性回执、真实 console stdio、Unicode argv
和原始进程出生时间确认。当前宿主下 thumbnail 身份校验返回不可确认，保留为未验收，不能降级为
按标题查窗口来伪造缩略图支持。日志 `p2-terminal-native.log`。

## B11 — 原生“另存为”被统一 RPC 超时误报（已修复，实机复验待完成）

在 Windows 贴图窗口点击保存后，保持原生对话框超过 40 秒，底层仍等待用户选择，
但 renderer 已显示“操作未确认”。修复为只对明确的文件选择/保存路径等待原始结果；
截图 send/copy/pin 仍保留后台超时和禁止重放语义。新增定时器合同覆盖延迟保存与未知发送，7/7 通过。
同类桌面文件选择入口统一遵循已有 PickFiles 语义；没有更改 macOS 原生窗口实现。
本次自动化可以观察该对话框，但无法可靠输入或关闭它，最终停止了无 Runtime 连接的隔离开发实例。
没有完成实际“另存为”文件验收，也没有写入用户文档目录。原选区已由贴图流程正常落盘。

## B12 — Bot 任务终端固定旧 Host 端口，跨版本不能自动重连（Windows 已修复并通过原生双 Client 复验）

v0.72.1 → v0.72.2 原生 ConPTY 矩阵中，同一旧 CLI 的受管连接进程正常自动恢复；
使用 Bot 实际 `attach --control-url <旧端口>` 参数的第二个旧 TUI 能看到升级前回复，
升级后持续请求旧端口，最终报告 connection refused。没有重放任务或手动 /resume。
Windows 任务终端改用 `--interactive --session <原会话> --store-dir <同一 Store>`，
由 Core 的受管发现/身份校验接入新 Host；Bot 开终端前仍核对原任务、principal、Store 与实例。
macOS 的已验收 attach 参数暂未修改；该路径存在相同固定端口风险，需 Mac 原生复验后单独修复。
证据：`caelis-tui-matrix.log` 的失败轮；两个原进程 PID 分别为 9972（自动恢复）与 10940（固定端口失败）。

复验：`caelis-tui-matrix.log` 最终通过，旧 TUI PID 11088、16932 不退出、未重新输入即看到新 Host 的
后续消息；原任务完成后升级，Session/Store/principal 保持。此项不等同于整套 Bot 安装包升级验收。

## B13 — DTW Windows 包的 CRLF Skill 被遗漏（已修复，原生 Caelis 接入通过）

`plugins.Load` 原本只接受 LF frontmatter。rc.9 Windows 成品的 SKILL.md 使用 CRLF，实际结果为
`skills=0 servers=1`、`invalid Skill metadata`，尽管 732 文件的字节校验正常。
只在读取元数据时归一化换行，不修改上游成品及其校验摘要。LF/CRLF 保字节测试和真实 Caelis Host
接入测试已通过；Skill 为 ready、MCP 仅 exec，禁用后目录撤销。2026-10-17 真实 Bot→Caelis→DTW
记事本创建/保存/读回演示通过，见 `p6-live-demo.json`。

## B14 — Windows 检出换行与资源清单路径导致检查误报（已修复）

Git CRLF 转换使固定 OpenAPI/生成 wire 文件的哈希改变；恢复原始 LF 后与 pin 完全一致。
`.gitattributes` 对固定协议字节设 `-text`，生成前端合同设 LF，没有放宽摘要校验。
严格资源包清单使用 `/`，遍历却以 Windows `\` 比较，误报合法文件为 unlisted；统一相对路径
分隔符，保持越界/额外文件/重解析点拒绝。完整 `check-windows.ps1` 已通过。

## 原生走查阻塞 — Snipaste 全屏截图层（已解除）

2026-10-17 最新开发包首次设置可见，但点击目标被报告为 Snipaste 的 Snipper。
激活 Bot 后复试仍拒绝。未退出外部应用、未更改 F1/系统权限，也未越过目标窗口操作。
用户于 2026-10-17 退出 Snipaste 后恢复操作；Bot UI→Caelis→DTW 的真实模型记事本演示通过，
证据见方案的 `p6-live-demo.json`。截图编辑器另存为、通知点击及其余 UI 走查仍独立验收。

## B15 — 更新回执轮询会阻止 Windows 原子替换（已修复）

全量检查中，未提交成功 shutdown 的更新被正确拒绝，但 `result.json` 偶尔仍停在 waiting。
确定性复现：用普通 `os.Open` 持有旧回执，原 `MoveFileEx` 立即报 Access is denied；
Windows 普通读句柄不分享 DELETE，界面轮询也会短暂形成同样的冲突。
Windows 私有状态替换现在只针对 sharing/access 冲突做最多一秒的重试，不删除或截断原文件；
更新助手等待回执实际持久化并处理写入错误，写入 installing 失败时不开始覆盖安装。
真实句柄读写冲突测试、原生 helper 成功/占用/未提交退出测试及修复后全量检查通过。
证据：`p4-update-receipts.log`、`p4-check-fixed.log`。macOS 原子替换实现未修改。

## B16 — 过时 Windows 跳过隐藏了完整装配验证（已修复）

`internal/app`、`internal/bot` 的 12 处测试仍以“Windows Named Pipe 尚未实现”为由整体跳过，
尽管本轮原生适配已经完成。恢复执行后，唯一失败是把管道地址当 Unix socket 做 `os.Stat/0600`
断言；按实际传输区分验证，保留 Unix 检查，Windows 走真实 peer 身份、MCP 错误 token 拒绝和调用往返。
CLI 管理的两个 shell fixture 也改成原生 Go 子进程，覆盖 Windows argv/环境继承及凭据清理。
应用/插件/Skill/关怀的装配检查与上述原生合同均通过。现有 POSIX 权限特例和真实外部环境 opt-in
仍保留具体原因，不把缺设备或未配置真实模型的测试伪装成通过。

## B17 — 首次引导复用已有模型时控件挤向右侧（已修复，Windows 实机复验通过）

2026-10-17 preview.4 实机复现：模型连接页的模型行只有右侧一小块，标题与模型控件没有统一对齐。
`onboarding-models` 使用 `align-items:flex-end`，旧样式只给直接 `.setting-row` 子节点设置全宽；
当前 `ExistingSetupModels` 返回的是 `.setting-section`，因此宽度规则没有覆盖真实结构。
preview.5 已复验全宽表单和模型下拉。用户随后指出“添加连接”仍游离在“继续”之后；这是共享
组件的排列问题，且共享样式隐藏了默认折叠标记。改为同一组件内的模型行→可展开附加连接→继续，
补统一 Chevron，删除临时 Windows 宽度补丁。仅调整该复用模型路径的结构和间距，连接/授权不变；
preview.7 已通过折叠、展开、模型行对齐及继续连接的原生视觉检查。
共享布局还需 macOS 原生视觉复验。

## B18 — 首条聊天消息后短暂出现重连提示（已加入呈现缓冲，待复验）

用户在 preview.4 首次引导后观察到重连提示闪帧，随后正常获得模型回复。
真实首轮与 DTW 演示均完成，尚不能把闪帧归为实际连接失败；需核对首次连接/介绍交接的原生状态，
Windows 的短暂 connecting/无详细错误的 offline 提示现在等待 750 ms 才展示；真实登录、设置错误、
明确失败信息和结果未知仍立即显示。仅影响提示呈现，不修改原快照或发送/停止/审批权限。
Mac 的连接提示行为保持。原生状态为何短暂变化尚未复现定位，不把显示缓冲声称为修复真实断线。
preview.7 首次介绍和截图识图均正常完成，没有持续重连提示；瞬时闪帧仍缺逐帧录像证据。

## B19 — 后台插件启动意外打开 Windows Terminal（已修复，Windows 实机复验通过）

用户在首条消息后观察到 Windows Terminal 弹出；本机进程证据显示首次连接时 Bot MCP bridge
启动 `dtw.exe`，随后产生 console/Terminal（07:39:36–37）。`plugins.runStdio` 的子进程遗漏
Windows 后台启动选项，和已有 CLI 探测路径不一致。应只隐藏后台 MCP 进程；用户主动打开的任务
终端仍必须可见。已复用平台后台启动适配；真实经过插件校验/启动路径的 Windows 子进程测试
确认 stdio 往返成功且没有 console，非 Windows 平台帮助函数为空操作。

preview.5 / preview.7 冷启动、新配置首次引导、真实 Caelis 首条回复通过；没有新增 Windows Terminal/OpenConsole
窗口或进程。DTW 内部仍有后台 conhost，不能把“无可见终端”误报为完全不创建系统控制台宿主。

## B20 — 截图工具过早显示且占满屏幕顶部（已修复，Windows 实机复验通过）

2026-10-17 用户在 preview.5 原生 F1 走查指出：尚未选区就显示整屏通栏操作，应完成选区后才出现，
并跟随选区。当前 WindowsCapture 的两行 toolbar 参与整页布局，还挤压了画布尺寸。
改为画布占完整视口，选区完成后显示边缘避让的浮动操作卡片；调整选区时隐藏，键盘取消始终可用。
对照 Mac 原生工具栏的工具顺序，使用紧凑图标、提示与可访问名称；卡片优先出现在选区下方，
底部不足时移到上方，并避让屏幕边界。preview.7 的原生 F1、初始隐藏、拖选后定位、保存与
Ask Bot 均通过；393×75 测试选区保存和发送字节一致，关闭整屏附带后模型正确读回文字。
多屏混合 DPI、小屏原生显示与深色模式仍需分别验收；边缘定位已有合同测试。

## B21 — 取消截图另存为被当成错误（已修复，Windows 实机复验通过）

原生另存为等待超过 40 秒未再触发 RPC 未确认；点击取消却出现 `RuntimeError: cancelled by user`。
只把固定 Wails 版本的原生用户取消归为无保存结果；其他错误继续报告。
preview.7 实际保存 PNG 后再次打开另存为并取消，返回同一编辑器、无错误提示，随后发送截图成功。

## B22 — Windows 桌宠缺少原生交互上下文，拖动跑步丢失（已修复，用户实机确认）

用户发现拖动时没有 Mac 的跑步动作。Windows 只完成 HWND 位置变化，未实现 `contextDriver`，
也没有发送 `pet-context`；共享 DragMotion 需要原生 dragging 与连续指针采样，因此始终为零。
补当前桌宠/鼠标/按下/拖动/菜单/本应用输入的轻量原生观测，使用共享动画；桌面窗口语义和独立
情境行为仍按范围后置。检查事件结束、取消、隐藏、销毁清理及辅助窗口恢复，不改 Mac 原生实现。
`WM_MOVING` 与 125 ms 原生采样持续发布上下文，`WM_EXITSIZEMOVE`/取消/隐藏清理拖动状态；
单击等待系统双击间隔，拖回原点也不能误开聊天。悬停沿 HWND 父级链识别 WebView2 子窗口，
不把独立气泡、任务窗口或覆盖窗口算作宠物。原生状态机、不可变快照、共享动画 8 Hz 采样与
30 fps 渲染、方向、停留衰减和清理测试通过。用户于 2026-10-17 在 preview.7 确认：左右拖动跑步、
松手恢复站立、单击开关聊天、双击唤回且不误关闭、右键菜单内容及点外部收起均正常。
自动化无法定位不激活的桌宠，以上单独标为用户目视结果，不以单元测试替代。
完整事件核对表及其余待验收项见适配方案。

## B23 — 隐藏后重新显示桌宠可能漏恢复气泡（已修复，待原生目视）

事件核对发现 Windows `apply` 隐藏了气泡，但显示分支只恢复宠物和任务入口。共享 Bubble 只在
`wanted` 变化时通知原生；如果最近回复没变，重新显示不会再收到 `SetBubbleVisible(true)`。
preview.9 原生沿用已保存的 `bubbleWanted` 恢复气泡，同时保持拖动期间隐藏；隐藏/开始拖动时
发送 `bubble-hidden` 清除阅读展开状态。desktop 测试和本地构建通过，实际隐藏/显示仍待目视。
属于 Windows 显示生命周期，不改变消息内容、用户授权或 Mac 行为。

### 待复现观察 — 冷启动期间的第二实例唤回

preview.9 启动后立刻从第二实例发送 `--settings`，未看到设置窗口；`native_ready` 后重试成功。
尚未区分单实例回调早于 Service ready、首个进程取得 owner 的时序或启动命令竞争。
保留为冷启动走查项，不把已就绪时的成功当作启动并发验收。

## B24 — 自启动注册成功却显示“不支持”（已修复，Windows 实机通过）

Windows Run 注册返回 `registered / supported=true / enabled=true`，共享前端状态联合和文案映射
遗漏 `registered`，落到“不支持”分支。不是 Windows 系统能力阻塞；修复呈现合同，保留系统
启动策略的独立所有权，不读取或改写私有 StartupApproved 数据。
preview.10 通过 Bot UI 关闭旧目录注册、在当前目录开启，实际显示“已设置为登录时启动”，
HKCU Run 的本产品命令与当前 EXE / `--login-launch` 一致。没有重启或注销用户桌面；
实际登录触发仍需该场景验收，系统策略是否禁用仍由 Windows 启动应用设置管理。

## B25 — 截图空白过渡及初始选区不明确（已修复，初始画面实机通过）

原生创建 WebView 后立即显示全屏窗口，截图图片还在解码；编辑画布的深色底暴露成黑屏。
改为首张图片和初始选区绘制就绪后显示。按用户要求默认当前屏幕选区、醒目边框和操作提示，
单击确认/拖动改选后显示工具栏；不能重新引入一进入就弹出全屏工具栏。
Windows 使用带窗口代次的首图就绪确认，排除旧编辑器回调误显示新贴图；解码失败有超时清理。
模型能力异步刷新，不阻塞本地截图。边框按 CSS 像素绘制，兼顾亮/暗背景和屏幕边界。
preview.10 实际 F1：2560×1440 默认全屏、底部提示、初始无工具栏；单击后显示紧凑卡片；
430×64 拖动选区边界清晰，卡片跟随。当前自动化截图没有覆盖逐帧过渡时序，
不能据此声称所有显卡、多屏/DPI 下零闪帧；该项仍待目视/录屏复验。

## B27 — 模型摘要被运行环境探测阻塞（代码与契约测试已修复，原生视觉待验收）

进入连接与模型页时，前端先等待两个 Runtime 的 InspectSetup / SetupModels，
之后才读取模型选择；SetupModels 内部又重复 inspect，且服务端由同一个 setup 锁串行执行。
未加载被误显示为未选择。

修复后先读取选择，再逐个更新运行环境；初次明确显示“加载中”，返回页面保留已有摘要并重新读取
选择。30 秒内复用运行环境探测结果；手动重新检测、保存模型、连接/安装操作仍强制刷新。
从 InspectSetup 复用经过协议协商的完整模型参数，过滤未认证项；旧适配器缺参数时才回退
SetupModels。Codex 检测现在返回已经取得的参数，不放宽兼容性或模型保存校验。

本机独立、只读探测（`p12-settings-timing.log`）：配置文件读取 <1ms，Caelis InspectSetup
1077.7ms，Codex InspectSetup 3367.2ms；之后重复的 SetupModels 分别 304.0ms / 87.1ms。
这解释了原页面接近 5 秒的前置等待，不能把它归因于 JSON 读取。
新配置摘要实际呈现耗时未取得原生 UI 测量；加载文案、先读取选择、逐 Runtime 完成、失败与
旧适配器回退已由渲染/异步契约测试覆盖。

复测命令（主动探测本机 Runtime，仅把 Bot 配置写入临时目录，不记录配置值/凭据）：
设置 `CAELIS_BOT_PERF_PROFILE` 为测试资料目录后执行
`go test ./internal/app -run '^TestWindowsSetupInspectionTiming$' -v -count=1`。
普通检查与 CI 自动跳过此实机探测。

## B28 — 截图首次显示延迟与贴图窗口语义错误（性能链路已测量，原生视觉待验收）

F1 路径包含固定 80ms 等待、整屏 PNG 编码、新建 WebView 和首次加载绘制；
此前没有阶段耗时证据。F3 被初始化为 draft，
贴图分支也使用带标题栏、常驻工具栏和额外边距的窗口，未满足图像直接置顶贴图的交互。
现已修复为直接 pinned：无标题栏、无常驻工具栏和额外留白，置顶、按图片比例显示、拖动、
滚轮等比缩放，右键复制/保存/标注/关闭，Space 进入已有标注流程；Ctrl+C/Ctrl+S 保留贴图。
遵循原生 Mac 实现与 [Snipaste 官方贴图交互](https://github.com/Snipaste/feedback/wiki/Getting-Started)。

截图路径改用快速无损 PNG（超出原字节预算时回退压缩），用
[DwmFlush](https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmflush)
代替固定 80ms 等待，并通过
[DWMWA_TRANSITIONS_FORCEDISABLED](https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/ne-dwmapi-dwmwindowattribute)
禁用截图/贴图窗口的系统过渡动画。前端按表面拆包；额外预加载一个隐藏的空截图 WebView，
使用后销毁，在空闲时补充一个空窗口，不复用旧文档、不预先抓屏。关闭截图功能会销毁空窗口。
代价是驻留一个额外 WebView；内存增量、启动峰值仍待专项采样。

本机 2560×1440 的阶段采样（毫秒，原生入口到前端完成首次绘制确认，不含硬件输入和显示器呈现）：

| 阶段 | 原路径（测量构建） | preview.12 | preview.13 预加载 |
| --- | ---: | ---: | ---: |
| GDI 抓屏 | 95.5 | 74.2 | 73.5 |
| PNG 编码 | 181.2 | 117.4 | 110.0 |
| 获取/编码全部完成，累计 | 387.8 | 235.2 | 223.2 |
| 窗口准备完成，累计 | 425.4 | 278.6 | 224.8 |
| 前端首帧绘制确认，累计 | 717.5 | 641.9 | 316.1 |

同次 preview.13 剪贴板贴图链路 44.6ms 到首次绘制确认。以上是少量诊断采样，非固定场景的
P50/P95 基准，不代表与 Snipaste 的严格对照或逐帧视觉验收。后续瓶颈集中在抓屏、PNG 编码、
跨桥传输/解码与 Canvas 绘制；若继续追求接近即时响应，应测量原生像素传递/呈现方案。
关闭/未知回执保留、slot 归属/失效、等比缩放/负坐标屏幕边界、PNG 像素一致性已有测试。
本轮工具反复返回 `foreground window did not report a process id`，F3 真实拖动/右键/缩放、
无黑帧与无动画的目视体验、多显示器 DPI 仍单列未验收。

Bot skill 已检查 `bot-core/references/screen-input.md`：无需改动。这里修正本地截图/贴图呈现与
设置读取顺序，没有改变 Bot 工具、输入权限、图片内容、回执语义或恢复规则；原指导已经称 F3 为贴图。

## B26 — 截图复制完成后仍停留在编辑器（已修复，主要完成路径实机通过）

复制分支只提示成功，没有退出；Enter 还被绑定为发送 Bot，违背常见截图的复制完成习惯。
参考 Snipaste 官方 [Getting Started](https://github.com/Snipaste/feedback/wiki/Getting-Started)
与 [Key Bindings](https://github.com/Snipaste/feedback/wiki/Key-Bindings)：复制成功退出、Ctrl+T 贴图、Enter/选区内双击复制、
Ctrl+A 全屏、右键重新选区/取消。保存成功退出而取消保存保留编辑；发送沿原回执关闭，错误/未知
保留可恢复文档，禁止为了关闭界面丢弃未确认请求。
完成时先解除 exporting，再关闭窗口，补接收确认早于导出返回的竞争条件。
preview.10 实测 Ctrl+C 关闭选区，F3 读回正确图片；Ctrl+T 切换独立贴图，原编辑器关闭；
Ctrl+S 取消保留编辑，实际保存 PNG 后关闭；Ask Bot 收到确认关闭、聊天正确读回测试文字。
保存和发送的 430×64 PNG 均为 6704 字节，SHA256
`f6545c88da3c0c26899d430d720e6503f5b1a1959b820fe038e6da4c9757d84d`，仅选区、无整屏附带。
preview.11 收紧双击为选区内触发，并区分剪贴板图片的初始提示；共享 Mac Enter/原生截图行为未改。
preview.11 实测选区外双击保留、选区内双击复制关闭，F3 读回正确图片和新提示，Enter 复制关闭；
自启动注册也已通过 Bot 开关迁到 preview.11，并保持原先开启意图。
