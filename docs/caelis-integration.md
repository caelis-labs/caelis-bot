# Caelis 接入与运行时管理

2026-09-23，本地实现，尚未发布。Bot 通过公开 HTTP/SSE 接入独立 Control Host；
不打包 Caelis，不导入兄弟仓库，不创建第二个 Runtime/Store owner。

## 接入基线

`protocol/caelis/manifest.json` 固定 Caelis 提交
`6ede951f3d383e131cf71b54b3573df401407e38`，以及公开 OpenAPI 和 wire 文件哈希。
此基线在交接时仅为本地提交，**已安装的 0.60.1 不包含这轮 Bot 能力**；
安装最新版成功不等于兼容，仍须通过协议能力检测。

握手要求 protocol 1、API v1、`caelis.control.envelope/v1`，以及：
`bot-mode-v1`、`bot-private-files-v1`、`bot-managed-work-v1`、
`bot-desktop-actions-v1`、`bot-reminder-grants-v1`、`bot-text-results-v1`。
图片另协商 `bot-image-input-v1`，并受模型能力限制。

## 使用入口

1. Bot 的「设置 → 运行时」打开 Caelis 管理页；标签仅选择管理对象，不改变当前运行时。
2. 自动发现本机安装；也可选择已有程序或在「更多管理」填写可执行文件完整路径。
3. 检测读取 CLI 版本并校验正在运行的 Host 能力。未安装时可点击「安装 Caelis」，调用官方 HTTPS 安装脚本。
   「检查更新」「更新」复用 `caelis update --check` / `caelis update`。
4. 数据目录默认为 `~/.caelis`；自建、开发版或隔离环境可指定完整路径。
   「启动服务」复用 `caelis service status/start`；已运行的共享 Host 不由 Bot 替换或重启。
5. 使用已有模型，或在「连接新模型」中配置；也可使用 Caelis 官方 `/connect`。凭据直接交给
   Caelis 保存，Bot 不持久化模型密钥。模型就绪后点击明确的「切换至 Caelis」，确认后重启生效。
   进行中工作、审批、未知结果会阻止切换；两个运行时的对话与配置独立保留。
6. 协议不兼容时「切换至 Caelis」置灰，悬停显示原因，辅助功能也可读取；管理与检查更新仍可用。
   2026-09-23 核实的最新正式版仍为 0.60.1，所需 Bot 能力在其发布后合入；需兼容的独立开发
   构建或后续发行版，不能把「已是最新版」当作可切换的依据。

服务不可用、旧版本缺能力、目录身份不匹配均明确报错，不能静默回退到 Codex。
Caelis 更新二进制后，如共享 Host 仍运行旧版本，需使用 Caelis 自身的服务流程完成重启。
安装、升级和服务启动都由用户明确点击；Bot 启动不会静默安装运行时。

`runtime.json` 示例（不含凭据）：

```json
{
  "version": 1,
  "runtime": "caelis",
  "cliPath": "/absolute/path/to/caelis",
  "caelisStore": "/absolute/path/to/isolated-store"
}
```

模型与权限页面读取 Caelis 模型目录和 Bot 专属配置；完整替换配置时保留未修改开关，
携带 revision/If-Match。effort/Fast 只展示当前模型支持的值；Caelis 的工作权限固定为
受限工作区，不能套用 Codex 的完全访问或 auto_review。

## 所有权与能力映射

| 产品操作 | Caelis owner / 映射 |
| --- | --- |
| 持续 Bot 身份 | Control 创建 Bot，绑定 store/principal/Bot/client；普通 Session 不能代替 |
| 聊天、中断 | prompt 稳定 operation ID；中断使用完整原生执行目标；主 Bot 不支持 steer |
| 专业工作 | Control 内置 ListWork/ReadWork/CreateWork/ContinueWork/SteerWork/InterruptWork；adapter 仅观察所属工作，不从 prose 补发 |
| 原生审批 | 每个工作独立状态与 SSE；原始目标和全部原生选项映射到 opaque UI handle；提交前重新核对 |
| 完成汇报 | Control 唯一触发，adapter 不安装 Codex TaskReporter；通知按 completion ID 去重 |
| 呈现确认 | 用户明确收起已完成气泡时，对该报告对应的 completion ack；GET/读取/OS 通知不等于 ack |
| 桌面动作 | 原生 Go 实现 clock/reminders/gesture；持久化 journal → claim 一次 → effect → receipt |
| 提醒 | Control 授权 grant；Bot 计时并只提交 grant/version/due；不再本地 Submit prompt |
| 模型设置 | Bot 专属 update + 真实目录；不用普通 Session 设置冒充成功 |
| 附件 | 最多四张、每张 8 MiB 的受支持图片；文本正文 256 KiB；无任意文件上传或产物下载 |
| 明确退出 | scoped client/exit 撤销当前 activation 并取消自有工作；不 shutdown/kill 共享 Host |

`api.ControlCompanion` 与 `api.DesktopEffects` 是 Caelis 的原生装配边界；
Codex 继续使用 `BotToolBinder + TaskProvider + TaskReporter`。Caelis 不注册第二套 MCP，
不启动 Codex 本地提醒唤醒循环，也不把 Host token 或本地 IPC 命令提供给模型。

## 持久化与恢复

Bot 应用数据目录的 `providers/caelis` 保存本后端的 binding、execution 和桌面记录，
不会复制 Codex 根目录下的 conversation/Work/提醒。草稿、角色与界面偏好仍由产品管理。
凭据独立存于 `credential.json`：本机用户拥有的 0700 目录、0600 文件，拒绝符号链接及
宽松权限；当前使用受保护文件，**并非 macOS Keychain**。Windows ACL 尚未实现，拒绝降级。
原生读取 Caelis discovery/auth.token；Host Bearer 仅用于初始化、创建和注册；日常调用用 scoped Bearer。
不把凭据、原生执行目标、正文或参数加入 renderer 诊断。

未知 prompt 查询永久 request source，确认完整执行目标后恢复接受状态，绝不自动重发。
未知 reminder fire 通过原生 occurrence 核对队列准入，不能把 claimed 状态说成执行完成。
桌面 claim 回复丢失时不重领、不执行；effect 已执行且 receipt 已落盘时仅重送相同 receipt。
SSE 完整替换先暂存、结束后一次应用，游标按不透明字符串保存；HTTP 状态轮询不能覆盖
请求发出之后到达的新 SSE 事实。Host 重启重新核对 store/principal、更新 activation 并恢复观察。
不同 store/principal 不能自动挪用旧身份。

明确限制：创建/注册回执完全丢失且没有可查凭据时保留未知，需要人工核对与明确重新授权；
没有提供自动重新注册按钮。模型配置、审批、中断或呈现 ack 的未知回执不自动重放，
当前可能保留待核对状态，不能通过删记录或换 ID 绕过。历史恢复窗口为最近 64 个 turns，
工作/完成列表尚无分页归档。尚不支持接管已有项目或其他 App 的任务。

## 验证命令与证据

```bash
cd /Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot
GOWORK=off make check
GOWORK=off make smoke
GOWORK=off make build
CAELIS_BOT_TEST_BINARY="$PWD/.cache/caelis-integration" GOWORK=off make smoke-caelis
```

`smoke-caelis` 使用单独构建的基线二进制、临时数据目录和可控模型服务，经过真实
HTTP/SSE/Control/工作沙箱，验证聊天、两个模型创建的独立工作、原生审批、秘书报告与 ack、
同一工作继续执行、工作等待审批时主 Bot 可用、动作 claim/receipt、提醒 grant/fire、
Host 重启重新激活且不重放、客户端退出后共享 Host 存活。测试不读取日常模型凭据。
协议夹具另覆盖未知请求重启不重发、HTTP 状态与 typed outcome、过期审批、丢失 claim/receipt、
原子状态替换及乱序响应。真实模型与原生 UI 的证据单独记录于 `preparation-status.md`。

真实模型验收单独启用，不进入日常 CI。先用 Caelis 官方 `/connect` 在隔离数据目录配置
模型并启动该目录的 Host，再指定该目录与模型名：

```bash
CAELIS_BOT_LIVE_STORE=/absolute/path/to/isolated-store \
CAELIS_BOT_LIVE_MODEL=mimo-v2.6-flash \
GOWORK=off make smoke-caelis-live
```

这会产生真实模型调用费用。测试创建自己的 Bot/client，仅使用合成消息与独立工作目录；
只批准两个精确匹配的 `printf` 验收命令，不批准额外命令或永久授权。覆盖真实聊天、两个
独立工作与报告、同一工作继续执行、桌面 clock/gesture、一次提醒唤醒及 client 退出。
凭据始终由 Caelis 使用，测试不读取或复制模型密钥，也不停止已运行的共享 Host。
2026-09-23 已在 `xiaomi/mimo-v2.6-flash` 通过此验收。

原生 UI 可通过 `CAELIS_BOT_DATA_DIR=/absolute/path/to/test-profile` 启动独立 Bot
配置，配合 `script/build_and_run.sh --verify`；路径必须为绝对路径。测试 profile 内的
`runtime.json` 按前述示例指向隔离 Host。不传此环境变量即恢复日常应用目录；
此入口仅用于开发验收，不迁移用户配置。原生窗口与模型协议测试是两项独立证据。

配置说明与 Control 内部汇报上下文通过原生事件来源过滤，不以正文内容推断消息类型；
真实助手报告仍正常显示。升级展示缓存时只重建派生消息，不清除身份、未知操作记录或桌面 receipt。

此版本不宣称所有故障组合、任意产物传输、Windows 原生沙箱或发行兼容性均已完成。
Windows 仍在 macOS 发行后按 `backend-platform-plan.md` 独立适配。
