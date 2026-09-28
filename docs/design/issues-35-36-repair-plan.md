# Issues #35 / #36 修复记录

日期：2026-09-28。Bot 改动在 `codex/care-runtime-fixes`；起点为 `a95e031`。
同一 worktree 分 commit 实现；Core #84 已合并，公共配置接入与外部二进制联调已完成。
原在途 PR #37 独立推进，未将其未合并改动带入本分支。

## Core 与 Bot 的职责

[Core #84](https://github.com/caelis-labs/caelis/issues/84) 已随 [Core PR #85](https://github.com/caelis-labs/caelis/pull/85) 合并。
Core 不识别 Bot/Notebook 等上层产品身份，不为 Bot 定制裁剪环境；提供可装配的通用配置
和公共 Runtime 能力。具体如何初始化 Runtime、使用哪个 CWD、继承和覆盖哪些环境、
shell 如何启动，由上层装配。协议可参考 Codex app-server 的配置入口与作用域语义，
但不假设两种 Runtime 的字段完全相同。sandbox、审批和原生执行权限保持各自所有权。

修复前 Core 的 `applicationExecutionRuntime.command` 覆盖整张请求环境、固定 PATH、
将 HOME/TMPDIR/ZDOTDIR 设为 CWD；随后登录 shell 还可能重组 PATH。它发生在 Bot
启动探测之后，不会反向修改父进程。只有 Bot 从已污染环境启动时，探测本身才会读错 home。
本次未修改 Core 代码。Bot 明确要求新版能力，旧 Host 需要用户经原有入口升级/启用。

## #35 已实现

- `on` 保持旧字符串编码，新增 `onAny`。集合排序、去重，单元素归一为 `on`，等价意图
  不改变版本或授权；旧规则授权指纹不变。所有源必须在保存/试算时注册。
- 任一订阅源到达只求值当前数据，每规则共享一次待发/未决激活与冷却；来源水位独立。
  `appChanged` 增加同次原生采样的 activeSeconds/idleSeconds，不引入跨来源快照缓存。
  部分来源失效仍允许可用来源触发，并报告 source_unavailable。
- 两个 adapter 按原请求/turn 关系保留结果，复用同一静默呈现逻辑。正文、制品、审批与
  失败反馈按 occurrence 最多计一次；流式后台正文仍暂存，精确 skip 仍静默。
  人类接管后的交互不新增自动打扰，已经展示的审批不因最终静默而退款。
- 派发前持久化潜在打扰预留。静默/明确拒绝释放，未知保留原请求身份、不重发、只挡住
  同规则；其他规则在剩余额度、presence 与 Runtime 空闲准入允许时继续。所有预留占满
  时仍需原生证据，不能靠超时/重启释放。普通提醒、用户消息和审批处理不受 care 预算限制。
- 冷却从派发开始；未派发过期/取消、明确拒绝不消耗完整冷却。静默仍经过规则冷却与
  派发间隔，避免持续条件重复唤醒模型。
- 默认 8 次可见打扰/24h、300 秒派发间隔；`bot_care configure` 接收完整 policy，按用户
  明确要求更改。list 返回生效 policy、interruptionsUsed/reserved/migrationReservations/remaining。
- v1 原子迁移到 v2，保留规则、授权、水位与回执身份。旧尝试没有可见证据，单独作为剩余
  24h 的迁移预留；历史 accepted 标记 legacy，仍未知的派发独立保留预留。写盘失败冻结 care，
  不影响独立存储的普通提醒。ObservedAt 明确为宿主首次观察时间，不冒充原生时间或用户已阅读。

提交：`bf42025` 多源订阅；`af1b599` 结果记账、冷却、政策与恢复。英文 Bot skill 已同步。

## #36 Bot 侧已实现

- 在应用数据目录解析与 Runtime 创建之前统一执行一次账户修复、登录/交互 shell 探测与安装。
- 缺失/相对或已知 Notebook HOME 恢复账户 home；已知 Notebook ZDOTDIR 清除，TMPDIR
  恢复原生用户临时目录。合法自定义 HOME/ZDOTDIR 保留，不修改用户 dotfiles。
- shell PATH 优先、继承目录追加去重；失败返回已纠正的继承环境。不硬编码 Homebrew 路径，
  不重定向运行期 HOME 到 Notebook，也不通过放宽 sandbox 修补工具发现。
- Codex/Caelis 自有进程启动共用 `runtimeenv.Clean`，只清调用者身份和私有控制通信；
  保留用户导出的配置/工具变量及显式 CODEX_HOME、Runtime、Bot 数据目录选择。
- 诊断只记录恢复状态、补回目录数量与错误原因，不写环境值、dotfiles 或 shell 输出。
  环境初始化属于宿主装配，不向 Bot skill 加入临时 HOME/source 绕法。

## 验证与剩余交付

受影响 Go 包 race 通过。真实 zsh 的合成 home 覆盖 Finder 最小环境、Notebook 污染、
自定义 ZDOTDIR、自定义工具目录、继承 PATH 恢复、缺失账户变量及有界失败。
安装版 Codex/Caelis + 本机合成 provider 已通过原生技能渐进加载、Worker 隔离、可见计数、
静默释放和既有后台路径；没有使用真实账户或付费模型。

完整仓库 gate 结果见 [准备记录](../preparation-status.md)。本次未新增 GUI 视觉验收，
也未验证物理锁屏/睡眠、真实模型长期关怀效果或用户账号下的 gh 登录态。

Core 合并后的装配与联调：

- schema/wire 和测试二进制固定 `bdebd8d2d4bfe6b1bec455fca0f19da49b673c8f`，通过源码归档独立
  构建外部 CLI，不导入 Core 私有包、不改 Core 工作区或安装版。
- 新 profile 显式 `environment.inherit: true`、`shell.login: false`；不写入环境值/凭据。
  既有 profile、未知创建请求与 Dream 交接保留创建时配置，不通过热更新更换环境。
- 原生夹具验证 Bot 的 HOME/Notebook CWD 分离，自定义 CLI 读取合成 home 配置，PATH 与
  合法 ZDOTDIR 保留，shell 恢复只发生一次；同步命令、TTY/input、一次性审批、Worker、
  普通 Session、Host 重启后执行均通过。继承配置不允许额外 HOME 写入。
- 同一 Host 上显式不继承、inherit+set/unset、空值与普通无配置 Session 互不污染；执行配置
  热更新被原生拒绝。旧 Host 缺少能力时明确不兼容，不生成新会话或悄悄改走另一 Runtime。
- Bot main 仍为 `a95e031`，PR #37 未合并，本分支保持独立。两个 issue 的修复合为一个 Bot PR。
