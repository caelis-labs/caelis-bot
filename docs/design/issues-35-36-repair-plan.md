# Issues #35 / #36 修复记录

日期：2026-09-28。Bot 改动在 `codex/care-runtime-fixes`；起点为 `a95e031`。
同一 worktree 分 commit 实现，等待 Core 修复合并后再接入公共配置、完成联调并提交一个 Bot PR。
原在途 PR #37 独立推进，未将其未合并改动带入本分支。

## Core 与 Bot 的职责

[Core #84](https://github.com/caelis-labs/caelis/issues/84) 已单独登记，由 Core 独立修复。
Core 不识别 Bot/Notebook 等上层产品身份，不为 Bot 定制裁剪环境；提供可装配的通用配置
和公共 Runtime 能力。具体如何初始化 Runtime、使用哪个 CWD、继承和覆盖哪些环境、
shell 如何启动，由上层装配。协议可参考 Codex app-server 的配置入口与作用域语义，
但不假设两种 Runtime 的字段完全相同。sandbox、审批和原生执行权限保持各自所有权。

当前 Core 的 `applicationExecutionRuntime.command` 覆盖整张请求环境、固定 PATH、
将 HOME/TMPDIR/ZDOTDIR 设为 CWD；随后登录 shell 还可能重组 PATH。它发生在 Bot
启动探测之后，不会反向修改父进程。只有 Bot 从已污染环境启动时，探测本身才会读错 home。
本次未修改 Core 代码，也不把旧 Host 的环境问题归为 Bot 启动修复已解决。

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

Core #84 合并后：

1. 按公共能力更新 Bot 的协议基线/配置装配，核对未指定、显式空、继承与覆盖的语义。
2. 使用修复后的外部二进制验证 Bot → Application Run/Start、审批后执行、普通会话和 Worker；
   HOME 为用户 home，CWD 为工作目录，自定义工具可用，sandbox/审批仍按原生政策执行。
3. 共享旧 Host 单独说明其启动环境，不静默重启。刷新 main，处理与 PR #37 的交集。
4. 补齐必要回归后，在同一个 Bot PR 中提交本分支的全部修复。此前不推送或创建 Bot PR。
