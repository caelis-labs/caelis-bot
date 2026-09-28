# Caelis Guardian 联调与发布条件

2026-09-28。Bot PR #38；Core 公共协议和外部 CLI 源码固定到
[#91](https://github.com/caelis-labs/caelis/pull/91) 的合并提交
`e8281aa7bb89b0ae330044eb5bd6a13916fd68fc`。通过公开 HTTP/SSE 联调，`GOWORK=off`，
不导入 Core 私有包、不更改用户 Core checkout、日常 Store 或全局模型设置。

## 上层装配

新建 Bot Runtime 显式选择 `permissions.approval_mode: auto-review` 和
`reviewer: {kind: guardian, model: <configured model>}`；默认固定初始 Bot 模型，
原生装配可通过 `Options.ReviewerModel` 指定另一个已配置模型。后续主模型变更不替换 reviewer。
`workspace-write`、HOME/CWD、环境继承和 shell 配置独立保留；没有用 full-access/never 代替审查。

连接通过 `application-guardian-review-v1` 协商，读取应用作用域的 `reviewer-state`，核对
Session、模式、reviewer 与 ready 状态；unavailable 明确报错，不转为人工或免审批。
ready 只表示本地装配就绪，不保证下一次模型调用成功。

旧版 Bot 的 manual 是代码固定值，没有用户可选的 manual 模式。已存在或创建结果未决的
Runtime 不热修改；**版本升级后的首次恢复启动**是新配置交接点。两种后端都记录创建时的
Bot 版本；启动恢复完成且旧工作空闲后，复用有效 Dream 交接，或立即发起一次交接整理，
不等待 15 分钟闲置。完成后自动创建新版 Runtime 并保留历史、Notebook、任务和既有提醒授权。
新 Runtime 使用当前版本的工具/指令与环境装配，保留用户选定模型、工作目录和独立沙箱范围。
同版本普通重启恢复原绑定；普通 Dream 仍在下一次用户输入交接。未决结果按原 ID 核对；
失败不形成空闲重试循环，用户输入仍可中断整理并优先继续工作。交接内容只在新上下文首个
请求获持久接受后消费。显式手动验收使用 `RequireApproval`；普通
Session 和 Worker 继续由 Core 各自的原生配置负责，Bot 不改全局审批设置。

## 注入工具的策略与成本

| 工具 / 操作 | 策略 | 原因 |
| --- | --- | --- |
| `bot_memory` 全部操作；Notebook 内文件 | 产品范围内直接执行 | Bot 工作区与低风险记忆，不增加 Guardian 成本；越界文件访问仍受原生政策约束 |
| `bot_clock`、`bot_task_read`、`bot_reminders_list`、`bot_care_read`（list/test） | 直接读取 | 只读观察与纯条件测试不审批 |
| `bot_gesture`、`bot_tasks` 的列表/固定/锁定/清理、`bot_task_stop` | 直接执行 | 本地呈现或停止已拥有的工作；不扩大执行权限，不删除任务历史 |
| `bot_task_start`、`bot_task_send` | 每次派生 / 新指令由 Guardian 审查 | 新执行或扩大工作指令；审批后直接继续，Worker 内部仍有自己的 sandbox / reviewer |
| `bot_reminders` save/remove、`bot_care` save/remove/configure | 修改持续安排时审查 | 审查未来工作授权；读取使用独立免审入口，匹配每次时钟事件不会再审查注册动作 |
| `bot_desktop_observe`、`bot_desktop_capture` | 只读直通 | 保留原系统权限、模型图片能力门控 |
| `bot_desktop_authorize` | 每个 App、每个连续任务 Turn 审查一次 | Guardian 审查本次任务的 App 访问目的和已观察到的 App |
| `bot_desktop_perform` | 当前 Turn 内已获授权 App 免逐次 Guardian | 每次输入前由原生 helper 校验 App 授权和新鲜目标；不按点击次数或操作批次收费 |
| 未明确列出的 callback | `approval_policy: required` | 新能力不会默认为免审；Codex 同样只免审明确列出的工具 |

Computer Use 授权属于当前连续任务，不向用户暴露 Session 概念。相同 App 的窗口共享授权，
另一个 App 必须单独授权。当前实现用原生 PID 与应用名称绑定本次运行的 App，不按窗口标题授权，
不同进程实例不会因同名而继承授权。授权仅在 helper 内存中存在，由 Bot 原生生命周期通过私有
管道提供 Turn 标识；模型参数不能创建或延长 Turn。完成、停止、中断、新 Turn 或 helper 重启后
不复用授权。长任务不设置按分钟或点击次数重新审批的限制。

App 授权不取消输入的新鲜目标检查、不扩大系统权限，也不把页面里的指令变成用户授权。
每次输入仍返回新观察并重规划，未知结果不自动重放。系统权限、登录和必要信息选择保留原交互。
Codex 使用原生 Computer Use 的应用授权语义，不另装 Bot Cua 绕行路径。

## 审查事实与恢复

审查中、批准、拒绝、失败、超时按原 Session/Turn/approval request 关联；保留工具调用及
实际动作证据。审查事实只读，不生成“用户已同意”的回执，也不能通过 Bot 的 Decide 接口
人工抢答自动审查。失败与超时独立于拒绝，不自动回退为手动审批。仅 canonical mirror 的
批准/拒绝进入派生持久记录，进度/失败/超时不伪造成持久决策。

自动审查中和静默通过不消耗可见打扰额度；可见拒绝/失败反馈按对应关怀激活计量。
callbacks 继续沿用原调用 ID、原 catalog 和参数的 claim/result 账本；未知结果只核对，
重连、重复 result、Host 重启都不重新执行副作用。

## 已验证

- `make check`：公开 schema/hash、Go vet/test、前端/i18n、原生生命周期与可移植编译检查通过。
- 受影响包 `go test -race`；Cua JS 行为测试和真实子进程/管道的取消、未知结果、授权失效检查。
- 外部 Core #91 的 `TestNativeHostIntegration` 与 `TestGuardianHostIntegration` 以 race 通过。
  原有环境、普通 Session/Worker 隔离、技能、Dream、关怀、手动审批后执行、重启和 content-v1 回归通过。
- Guardian callback 在审查中无可 claim 的意图；批准后只执行一次；拒绝、无效 reviewer 回复、取消、
  真实 90 秒审查超时均不执行，也不产生人工审批。相同结果重交幂等，重复 claim 被拒绝。
- 真实 macOS 原生命令与文件的允许/拒绝及外部路径副作用通过；Host 重启后只有决策回放，没有重跑。
- Core → Bot Runtime → 生产 Cua facade/管道完整链路：一个 Turn、一个 App 两次输入只有一次 Guardian；
  新 Turn 复用旧授权及拒绝后输入均被拒绝。该 Cua 测试仅替换底层 SDK 为确定性原生界面夹具，
  不操作真实 WPS 或日常应用；独立 JS 测试覆盖同 App 不同窗口和另一 App 的边界。
- App 升级的旧 manual → Guardian、新版配置与历史/交接内容继承、同版本不重复交接，纳入外部 Host 验收。
- 实时审查与 callback 使用同一原生 item 身份；同一 Turn 重用 provider 调用 ID 仍区分 invocation。
  清空 Bot 派生缓存并重启 Host 后，从公共回放核对相同身份。历史/外部 agent 路径允许合法缺失，Bot 不补造 ID。
- 英文 Bot skill 已更新：减少重复口头确认，使用免审读取工具和 App × Turn 授权，遵守拒绝/未知恢复。
  原有 Codex/Caelis skill discovery、按需读取和 Worker 隔离回归仍通过。

证据：`.cache/core-91-final-integration.log`、`.cache/guardian-final-{check,race,smoke,build}.log`、
`.cache/guardian-cua-tests-final.log`、`.cache/upgrade-final-race.log`。构建是 ad-hoc `Caelis Bot Dev.app`，不是公证发行。
未调用付费模型，未评估真实 Guardian 风险判断质量；没有新增 Windows/Linux 原生执行或真实 WPS 验收。

## Core 下一版 release 满足条件

| 条件 | 当前状态 |
| --- | --- |
| 发布包包含 #85 环境契约、#89 Guardian 公共能力和 #91 身份修复；握手报告必需 capabilities，运行中的 Host 也已升级 | 源码构建通过；发行包发布后仍需验证，版本号不能代替握手 |
| reviewer 显式装配、作用域就绪查询、sandbox 独立、无隐式放宽或人工回退 | 外部 Host 通过 |
| callback 审查在可 claim 意图之前；命令/文件批准和拒绝走同一原生 continuation | 外部 Host 与 macOS 原生副作用通过 |
| 拒绝、故障、取消、真实超时均无副作用；重启、重复回执不重新执行 | 通过 |
| #88/#89 承诺的 immutable item_id 在真实 live/replay 路径与实际 invocation 相同 | 已由 [Core #91](https://github.com/caelis-labs/caelis/pull/91) 修复；实时、callback、同 Turn 重复 provider ID 和重启回放通过 |
| 使用 #91 最终合并提交更新协议 pin / 外部二进制并运行完整 smoke-caelis | 通过，完整 race 集成 150.485 秒 |

Bot 所需的 Core #85/#89/#91 源码能力已通过最终联调，可进入下一版 release。
发行工件发布后的安装、握手和签名验收仍独立进行；
合成模型验证的是审批与执行链路，不替代真实 Guardian 风险判断质量评估。
不要为 Bot 裁剪 Core 环境或增加 Bot 专用白名单；App/Turn 授权是上层使用通用 callback review
装配出的产品策略。
