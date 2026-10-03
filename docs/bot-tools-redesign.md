# Bot 工具精简方案

2026-10-03，调研基线：`3c87eeb`。原方案已按后续授权实现；下面保留设计依据与验收标准。
实际入口、迁移边界与验证记录见 [工具精简实现与验收](bot-tools-acceptance.md)。

## 结论与范围

建议将完整 Bot 私有目录从 **22 个工具收敛为 10 个**，按“记忆、任务、持续安排、桌面、表达”组织。
数量减少约 55%，但验收目标是模型更容易选对工具、填对参数、核对结果，并减少无意义调用。
10 个是首选设计，不是硬指标：如果合并后的某个工具明显更难使用，就保留必要拆分。

本轮设计覆盖现有能力，不增加任务级 Runtime 选择、自动远端调度、任意脚本执行、连接配置工具或新的默认 UI。
用户仍选择 Worker 默认后端；新任务继承默认值，旧任务通过原绑定查看、续跑、停止与打开。
未指定远端机器的工作留在本机。Caelis Team 仍是可选增强，不进入普通委派参数。

## 调研发现

| 当前问题 | 代码证据 | 设计影响 |
| --- | --- | --- |
| 完整目录含 13 个基础工具、9 个桌面工具 | `internal/bot/tools.go:toolSpecs`；`internal/desktopcontrol/definitions.go:Definitions`；固定依赖的 `protocol.Tools()` | 不是每个 Runtime 都会展示 22 个；继续按真实能力过滤，不能把不可用能力藏在统一入口内假装可用。 |
| Codex、Caelis 当前都按工具名称投影审批策略 | `internal/botpolicy/policy.go`；`internal/backend/codex/bot.go:toolConfig`；`internal/backend/caelis/application.go:ConfigureBotTools` | 查询与需要审核的写入不能随意合并。将混合工具整体直通会扩大权限，整体审核则增加不必要的审核。 |
| 任务入口分散，描述重复承担列表/Dock 规则 | `internal/bot/tools.go:toolSpecs`；`internal/bot/task_catalog.go` | 将发现、查询、展示与停止收敛；启动/继续保持独立的受审入口。Dock 细则移到按需加载的任务参考文档。 |
| 只读关怀 Schema 复制写入 Schema，删掉 policy 后仍保留大量不相关字段 | `internal/bot/care.go:careReadSpec` | 为每一种请求独立生成字段，不能靠模型从一大堆可选参数中辨认哪些有效。 |
| 桌面目录主要来自底层 SDK 的一对一投影 | `internal/desktopcontrol/definitions.go` | 保留 SDK 的内部职责，但向模型提供更简洁的观察与结果入口。 |
| 桌面只读操作也要求模型填写 requestId，并暴露不同的读取/增量/回执身份 | `internal/desktopcontrol/controller.go:CallTool` | 只读查询的内部 ID 由 Host 生成；会发送输入的操作保留模型可复用的稳定 ID。原操作与查询自身的身份不能混淆。 |
| 查询已经有分页与桌面预算；任务完成也有原生通知 | `internal/bot/task_catalog.go`；任务与桌面 skill 参考文件 | 保留这些能力，避免精简后退化为全量列表、完整转录或高频模型轮询。 |

这次没有读取用户私有对话来统计使用频率，也没有运行模型 A/B 测试。因此工具易用性、Token 和耗时改善是待验证假设，不是已证明的收益。

设计依据是减少模型决策负担、将确定性工作交给代码、用结构化参数限制非法组合，而非追求最少函数名。
[OpenAI 的函数设计建议](https://developers.openai.com/api/docs/guides/function-calling)支持这些原则，也明确工具数量建议需要评估；
[Anthropic 的工具设计实践](https://www.anthropic.com/engineering/writing-tools-for-agents)强调围绕实际工作流设计工具、返回相关信息并用真实任务验证。
这些资料提供设计依据，不替代本项目的双 Runtime 验收。

## 首选工具目录

以下名称为方案名称，最终以模型评估为准。每个工具只承担一个明确职责；同一职责内允许少量有明确字段边界的请求类型。

| 工具 | 模型需要理解的职责 | 请求类型 | 审批与生命周期 |
| --- | --- | --- | --- |
| `bot_memory` | 记住、查找、更正、忘记用户信息 | 保留 recall / remember / correct / forget | 保留产品本地记忆直通策略，不扩大到 Notebook 文件删除或历史擦除。 |
| `bot_tasks` | 找到、查看、展示或停止 Bot 已拥有的任务；发现用户指定的机器 | list / read / machines / watchlist / stop | 保留现有列表、展示与精确停止的直通策略；不是纯只读工具。不得启动或继续工作。 |
| `bot_delegate` | 安排新工作，或给既有工作追加方向 | start / continue | 保留当前 start/send 的 Runtime 审核、来源与容量约束。 |
| `bot_schedule` | 查时间、查安排与可用事件，测试条件 | context / list / sources / test | 直通查询；纯测试不能注册、派发或发布真实事件。 |
| `bot_schedule_update` | 保存、删除持续安排，或修改关怀打扰预算 | save / remove / configure | 保留 Runtime 审核及原生注册/撤销授权，不能混入查询入口。 |
| `bot_desktop_inspect` | 观察界面、读文本、刷新变化，或明确请求图片 | outline / text / delta / image | 元数据查询直通；image 仍须检查模型图像能力及当前 App × Turn 授权，不自动截图。 |
| `bot_desktop_authorize` | 为这次用户任务申请操作某个真实应用 | 保留 exact application / name / purpose | 单独审核，保留应用实例 × Bot Turn 授权。 |
| `bot_desktop_act` | 对已观察、已授权的界面发送有限步骤 | 有类型的 steps | 保留原生授权检查与 16 步上限；不增加逐点击审核。 |
| `bot_desktop_result` | 查询原桌面操作的结果，或停止尚未执行的步骤 | status / cancel | status 按原 ID 核对，Turn 结束后仍可用；cancel 保留当前活动 Turn 与授权限制。 |
| `bot_gesture` | 有意识地给用户简短的角色反馈 | attention / nod / celebrate | 保留现有直通与隐藏/减少动态效果语义。回执只说明接收，不能证明用户看见。 |

保留 `bot_gesture` 的原因：它很小、职责独立，而且符合桌面角色定位。为了少一个名字把它塞进任务管理或桌面输入，会损害可理解性。

`bot_tasks.stop` 只能停止用户指向的 Bot-owned 活动执行，不能成为 stop-all、终端关闭或任务删除入口。
`watchlist` 的动作限定为 pin / unpin / lock / unlock / clear；clear 仅清空未锁定展示项，不停止任务、不删除历史。
列表默认 20、上限 50，沿用原有游标约束。机器列表增加按用户给出的名称搜索，避免模型读取所有机器；重名必须返回候选，不替模型选机器。

## 旧能力映射：22 → 10

| 现有工具 | 新入口 |
| --- | --- |
| `bot_memory` | `bot_memory` |
| `bot_tasks` | `bot_tasks.list / watchlist` |
| `bot_task_machines` | `bot_tasks.machines` |
| `bot_task_read` | `bot_tasks.read` |
| `bot_task_stop` | `bot_tasks.stop` |
| `bot_task_start` | `bot_delegate.start` |
| `bot_task_send` | `bot_delegate.continue` |
| `bot_clock` | `bot_schedule.context`；相关安排查询/写入回执也返回新鲜时间与时区 |
| `bot_reminders_list` | `bot_schedule.list`，可按 calendar 过滤 |
| `bot_care_read` | `bot_schedule.list / sources / test` |
| `bot_reminders` | `bot_schedule_update.save / remove`，calendar trigger |
| `bot_care` | `bot_schedule_update.save / remove / configure`，event trigger / policy |
| `bot_desktop_observe` | `bot_desktop_inspect.outline` |
| `bot_desktop_read` | `bot_desktop_inspect.text` |
| `bot_desktop_sync` | `bot_desktop_inspect.delta` |
| `bot_desktop_capture` | `bot_desktop_inspect.image` |
| `bot_desktop_authorize` | `bot_desktop_authorize` |
| `bot_desktop_act` | `bot_desktop_act` |
| `bot_desktop_get` | `bot_desktop_result.status` |
| `bot_desktop_reconcile` | `bot_desktop_result.status`，原 requestId 查询路径 |
| `bot_desktop_cancel` | `bot_desktop_result.cancel` |
| `bot_gesture` | `bot_gesture` |

这是模型入口合并；原生层仍保留查询、停止、回执恢复、事件注册等不同 handler。

## 参数：减少无关字段，而非增加一个万能 args

统一采用 `{ "request": { "type": "…", … } }` 的有类型请求。
每个分支规定自身必填项、可选项、限额与未知字段拒绝；不暴露一个自由 JSON/string 参数供模型填写底层 API。
根对象保持普通 object，内部候选使用有判别字段的 Schema；两种 Runtime 的实际兼容性须先 POC，不能凭 OpenAI API 文档推定 App Server 和 Caelis 的全部 Schema 行为。
现有桌面 Schema 已用 oneOf/const，但这不能证明新目录及任意 Schema 子集均兼容。

示例均为设计，不是当前可调用协议：

```json
{"request":{"type":"start","requestId":"review-login-001","title":"检查登录流程","prompt":"检查现有项目的登录流程，给出可以复现的问题和修复建议。"}}
```

`bot_delegate.start` 最少只需要 requestId、title、prompt；machine、workspace 保持可选。
没有 runtime、model、effort、team、nativeThreadId 参数。Host 使用用户已选的 Worker 默认值。

```json
{"request":{"type":"continue","task":"<返回的任务 handle>","requestId":"review-login-002","prompt":"补充检查登录失效后的提示与恢复路径。"}}
```

continue 不接受 machine / workspace / Runtime 重绑定；Host 必须读取存量任务绑定，不受当前默认值影响。

```json
{"request":{"type":"read","task":"<返回的任务 handle>"}}
```

`bot_tasks.read` 同时规划一个互斥的 `requestId` 查询分支，以便提交丢失响应、尚未取得 task handle 时找回原记录。
这是需要新增的 Host 查询合同，当前接口没有公开该能力；不能把空列表当作未执行，也不能用新 ID 再提交。
任务结果读取仍保留完成通知确认语义，普通分页/搜索不能误确认所有通知。

```json
{"request":{"type":"save","id":"water-hourly","label":"喝水提醒","prompt":"提醒我喝水。","trigger":{"type":"calendar","schedule":{"type":"interval","everyMinutes":60},"timeZone":"Asia/Shanghai"}}}
```

calendar 的 schedule 只能选择 at / interval / daily / times 之一，weekdays/window 只在适用分支中出现。
event trigger 则只包含明确的已注册 sources、纯 CEL condition、timeZone、cooldown 与 expiry。
两类 trigger 不能同时填写。普通提醒不接触 CEL 或关怀 policy；configure 只接收 policy。
list 返回统一的 automation handle、类型、摘要和自身状态；原 reminder/care ID、授权、持久文件和未决派发记录仍保留独立命名空间。
save 的稳定业务 ID 和现有更新/撤销语义不在这次重设计中改写，尤其不能借更新绕过未决派发。

```json
{"request":{"type":"outline","scope":{"refs":["<真实窗口 Ref>"]},"match":{"role":"button"}}}
```

桌面只读查询不要求模型生成 requestId；Host 为每次实际查询生成内部读取身份。
scope、cursor、continuation、Ref 保持实际返回的精确值，Host 不按相似名称自动重绑。
使用 continuation 时，Host 复用原查询上下文，不让模型重填整套参数；显式冲突参数应拒绝。
text 读对象自身文本，outline 用于碎片化后代文本，不能把两者混为“已经读完全文”。
image 仍是单独明确的请求类型，只返回指定可见区域的图片，不随观察或输入自动附带。

`bot_desktop_act` 保留稳定 requestId 与有类型的 steps，去掉工具外层多余的 args 包装。
每个 step 只暴露其操作所需字段，Host 负责 epoch、Turn、进程身份与原生 wire ID。
已有 invoke、set_value、focus、keyboard、pointer、wait、bind 能力保持；不得借简化悄悄开放任意坐标输入、JS eval 或自动修复过期 Ref。

```json
{"request":{"type":"status","requestId":"rename-note-001"}}
```

桌面 result 用原 requestId 查询；兼容原 run_id 的查询分支互斥，明确它们是“原操作身份”。
Host 能从已知回执定位 run 时负责转换，不能把模型猜出的 run 当作原操作。
cancel 仍需要自己的稳定请求身份，且只停止未完成步骤；status 不会产生新的输入或新图片。

## 返回值与描述：让模型知道下一步

统一结果形状采用小的公共外壳和各领域自己的 data，不强行把不同生命周期压成同一个状态：

```json
{
  "ok": true,
  "outcome": "accepted",
  "data": {"task":"<opaque handle>","title":"检查登录流程","status":"working"},
  "next": {"tool":"bot_tasks","request":{"type":"read","task":"<opaque handle>"},"when":"on_completion_or_user_request"}
}
```

- mutation 的 accepted / rejected / unknown 与 task 的 working / completed 分开。accepted 永远不等于工作完成。
- 结果默认摘要；列表不给完整转录。Task read 给有界结果与验证/阻塞信息，不凭 Worker 自述升级验证结论。
- query 的覆盖范围、截断、游标、reset_required 与事实 unknown 明确返回；截断不得伪装成完整空结果。
- 错误提供稳定 code、简短说明、原 receipt/task handle，以及必要的安全下一步。unknown 只能指导查询原记录。
- `next` 是确定性 Host 提示，不是来自网页/Worker 的新指令；结果正文仍属于不可信资料，不能产生新授权。
- 普通时间/时区从新鲜的 Host 事实提供；长对话中的“明天几点”仍可用 context 主动刷新，不能依靠开场时间。
- 已知 Host 信息不再要求模型填写：当前后端、机器已有配置、内部 Turn、epoch、catalog revision、只读 wire ID。
- 公共外壳保持简短。MCP text、structuredContent 和 Caelis content-v1 的投影须避免重复 JSON；图片仍使用原生内容块。
- 每个工具描述首先说明用途、输入核心、结果及一条最关键边界。复杂恢复/日历/桌面语义放到 Bot skill 的按需参考文件；幂等与 unknown 禁止重发等关键约束不能只藏在 skill 中。

## 典型调用流程

| 用户场景 | 首选流程 | 不应出现的额外负担 |
| --- | --- | --- |
| 本机完成一项持续工作 | delegate.start → 原生完成通知 → tasks.read；有必要时 continue 同一任务 | 不必先查机器、模型、容量或重复 pin；不为轮询维持一个模型回合。容量实际失败时返回当前限额与已有任务摘要。 |
| 明确指定 Fedora 工作 | tasks.machines 搜索用户给出的名字 → delegate.start 使用唯一返回的 machine | 不让 Bot 选择 Codex/Caelis；没有唯一匹配时不偷偷回退本机。 |
| 切换默认后端后继续旧任务 | tasks.list 搜索 → delegate.continue 用原 task | 不重新创建任务、不以当前默认值改写旧绑定。 |
| 提交返回 unknown | tasks.read 用原 task 或原 requestId | 不生成新的提交 ID；查不到仍是未确认。 |
| 移除 Dock 展示项 | tasks.watchlist 的 unpin 或 clear | 不停止 Worker，不删除历史，不关闭观察终端。 |
| 停止特定工作 | tasks.stop → 必要时 tasks.read 核对原生确认 | running/accepted 不能立即报告已停止。 |
| 明天上午提醒 | schedule.context 获取当前时间/时区 → schedule_update.save；保存回执包含摘要与下一次触发 | 无须再强制 list 一次；App 常驻限制明确，不能承诺退出后仍执行。 |
| 添加事件关怀 | schedule.sources → 必要时 schedule.test → schedule_update.save | 不把安装了 CLI 等同于已注册事件源；不偷偷提高打扰预算。 |
| 操作一个应用 | desktop_inspect 定位 → desktop_authorize 一次 → desktop_act → inspect.delta/text 核对必要事实 | 不每次点击重新审核、不每次输入抓全树、不默认截图。 |
| 桌面操作 partial/unknown | desktop_result.status 原 requestId → 必要时 inspect 查看当前事实 | status 在 Turn 撤销后可用；不重放输入，不换 Writer 或重启 Helper 来绕过未知结果。 |

## 审批、合同与迁移风险

1. **保持审批同类合并。** 新目录的直通名单为 memory、tasks、schedule、desktop_inspect、desktop_act、desktop_result、gesture；delegate、schedule_update、desktop_authorize 保持审核。直通不等于无限权限，Host 仍检查所有者、当前用户来源、App grant、活动 Turn、图像能力与取消状态。
2. **分支级生命周期先验证。** 当前 Bot 根据工具名称放行 reconcile 的撤销后查询、检查 capture 图像能力。合并后必须解析并校验请求，再按请求类型建立上下文。不得把 cancel、输入或截图带进撤销后的恢复例外。
3. **保留原调用身份。** 新请求固定到新 catalog；旧在途调用继续使用原 tools version/config revision/handler。旧名称不再进入新模型目录，但兼容 handler 在原绑定里保留到未决操作结清。不是在全局公布两套目录，也不是把旧回调转发到新默认后端。
4. **别名不制造重复副作用。** 旧入口与新入口只投影同一原生操作；幂等按原操作身份与内容检查，不能因工具改名重新执行。回滚只能回滚目录/展示，不回滚或重放已派发操作。
5. **目录只属于 Bot。** Worker 保持原生工具与权限，不继承 Bot 私有 MCP 地址、Notebook、常驻 skill 或新目录。
6. **Schema 兼容性有明确停止条件。** 如果某种判别式结构无法被两种 Runtime 正确消费，采用更简单的有限枚举与 Host 严格校验，必要时保留一个独立工具；不改成任意 JSON 字符串逃避校验。

## 实施路线与验收

### 阶段一：合同样机与风险 POC

- 导出当前真实可见目录，记录描述/Schema 输入 Token、有效操作分支、默认结果大小，作为基线。
- 先做新目录的 Schema 与结果投影样机，不改持久存储和业务规则。
- 在 Codex App Server 与 Caelis 分别确认判别分支、未知字段拒绝、审核名单、content-v1 图片与 JSON 去重。
- 强制验证：image 必须有图像能力与应用 grant；unknown 后仅 status 可越过已撤销 Turn；cancel 不能越过；混合工具不能新增审批绕过。
- POC 不通过就调整相应入口，不急于迁移全部工具。

### 阶段二：先落地任务与持续安排

- tasks + delegate：保留 WorkRouter/TaskLedger/旧 Runtime 绑定；补充原 requestId 查提交记录合同；缩短任务描述。
- schedule + schedule_update：分支独立 Schema、统一查询摘要、维持 reminder/care 独立授权与存储。
- memory 与 gesture 保持能力；仅在评估证明有帮助时整理描述/返回值。

### 阶段三：桌面适配与目录切换

- inspect + result 包装现有 Desktop World SDK，不改 Desktop World 执行权威。
- 去掉只读查询的模型 requestId 负担，保留动作与取消的稳定身份、精确 Ref 和有界反馈。
- 同步两种 Runtime 的版本化目录、审批映射、旧调用 handler 和 Bot skill。
- 新目录先仅用于隔离 profile；通过真实回归后再更新本机 Dev Bot。

### 评估方法与通过条件

使用相同业务实现、用户意图与模型设置比较旧 22 工具与新 10 工具；比较维度包含 Bot 运行时和实际模型，不能把 Runtime 与模型混为一项。
建议先准备 40 个场景，每类 8 个：任务；记忆/表达；日历提醒；事件关怀；桌面。部分场景用于迭代，至少四分之一留作未参与调参的验收。
复杂场景增加重复运行。测试只使用隔离数据、专用工作目录和可逆测试应用，不读取真实用户私有对话。

必须包含用户真实痛点：Fedora 重名/离线；旧后端任务继续；列表清空与停止区分；容量满；默认时区/跨夜；未知派发；截图能力缺失；过期 Ref；取消与完成交错；Turn 结束后的查询。
任务成功由原生记录与实际输出核对，不能只让模型自报“成功”；允许多条正确调用路径，不以固定调用序列作为唯一标准。

| 指标 | 通过原则 |
| --- | --- |
| 实际任务完成率 | 新目录不能有显著退化；常见流程与留出集均验证。小样本只是首轮门槛，不宣称普适统计优势。 |
| 审批与未知结果 | 回归场景中零新增越权、零未知结果重发、零跨 Runtime 误续跑。 |
| 工具选择与参数 | 记录首次选对率、非法参数/无关字段、错误修复轮数；合并后的高频流程应改善或保持。 |
| 调用与上下文成本 | 记录工具定义 Token、结果 Token、调用次数、响应到有效动作的时间；减少名称不算单独通过。 |
| 展示与通知 | clear 不停任务；普通 list 不吞完成通知；accepted 不被报告为完成；停止等待真实确认。 |
| 兼容与回滚 | 双 Runtime 合同测试、原在途回调、重复 ID、重启/断线、新旧目录切换和回滚均有覆盖。 |

实现阶段运行对应合同/生命周期测试以及 `make check`、`make smoke`、`make build`；按仓库要求通过 `script/build_and_run.sh` 做原生检查。
主要变化是模型工具，不添加新的设置页；原生验收关注聊天委派、完成回报、Dock、审批和远端原任务恢复，不能仅用 UI 截图代替合同与真实执行证据。

## Bot skill 与文档维护

已同步更新 English Bot skill 的领域路由及按需参考文件，与新目录一致。
实施时更新 English `SKILL.md` 的领域路由与 tasks/reminders/proactive-care/desktop-observation/expression 参考文件；用少量选择规则代替逐工具百科。
同步架构中工具名、审批与恢复合同，验证 Codex/Caelis 的 Bot-scoped 发现和按需加载；普通会话与 Worker 不受影响。

## 不采用的路线

- 一个 `bot(operation, args)` 总入口：名称少，但领域选择、参数组合与权限判断仍全部压给模型。
- 合并后把全部操作直通，或把全部查询都送审：破坏现有权限边界或常规调用体验。
- 仅依赖 deferred discovery：可作为支持它的 Runtime 的额外优化，但不能替代两种 Runtime 一致的好目录。
- 为省调用而自动截图、全量回传对话、自动选择远端/Runtime：增加成本或改变已确定的产品行为。
- 把日历、CEL、policy 全部铺成一张可选字段表，或让模型填一个自由 JSON 字符串：函数数量虽少，参数仍难用。

建议下一步先执行阶段一 POC 和模型用例对比；通过后再分域落地，不一次替换全部目录。
