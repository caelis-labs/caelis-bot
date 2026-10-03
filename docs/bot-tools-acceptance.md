# Bot 工具精简：实现与验收

2026-10-03；设计依据见 [Bot 工具精简方案](bot-tools-redesign.md)。

## 实现结果

完整 Bot 私有目录由 22 个工具收敛为 10 个。基础入口 6 个，桌面入口 4 个；实际目录仍由 Runtime 能力和桌面适配器决定。Worker 不继承这套目录。

| 入口 | 主要操作 |
| --- | --- |
| `bot_memory` | recall / remember / correct / forget |
| `bot_tasks` | list / read / machines / watchlist / stop |
| `bot_delegate` | start / continue |
| `bot_schedule` | context / list / sources / test |
| `bot_schedule_update` | save / remove / configure |
| `bot_desktop_inspect` | outline / text / delta / image |
| `bot_desktop_authorize` | 当前任务的精确应用授权 |
| `bot_desktop_act` | 有限步骤、稳定 requestId |
| `bot_desktop_result` | status / cancel |
| `bot_gesture` | attention / nod / celebrate |

任务、安排、桌面查询与桌面结果使用有类型的 `request` 分支。记忆、表达、应用授权保持小的原有字段；桌面输入采用紧凑的平面 `requestId + steps`。Host 拒绝未知字段、非法组合、重复 JSON 键及超限参数。

委派、安排写入和桌面授权继续经过 Runtime 审核。其余入口沿用既有直通策略，仍检查所有者、来源、应用授权和活动 Turn。日历与事件安排使用不同命名空间；条件测试不会注册或发布事件。

`bot_tasks.read` 可以通过原始 requestId 找回已拥有的任务。启动和继续的请求身份在派发前持久保存；查询不到仍是未确认，不能据此创建替代任务。任务完成通知同步使用新入口。用户选择新任务默认后端，旧任务继续沿原绑定工作。

桌面只读身份由 Host 生成。分页 continuation 仅恢复同一 Turn 的原查询，晚到结果不会污染新 Turn。只有原操作 status 可在 Turn 撤销后恢复回执；输入、取消、截图不能越过撤销边界。桌面输入保留原生执行器、稳定身份、16 步限制与结果预算。

Codex 的私有 MCP 桥使用版本 2 和 Host 提供的精确目录。Caelis 按原 tools version 保存回调处理器；历史 22 工具仅供原版本绑定恢复，不进入新模型目录。内容块与结构化结果继续使用原生 content-v1，较大结果避免再次复制完整 JSON。

English Bot skill 已更新核心路由、任务、记忆、提醒、关怀和桌面参考文件，保持按需发现与加载。

## 真实模型覆盖

验收使用独立 Bot 绑定、Notebook/Memory、工作目录与可逆 AppKit 测试窗口；没有读取用户私有对话。账户认证来自已安装 Runtime，测试模型只写入验收配置。

| 场景 | Codex / GPT-6-Luna medium | Caelis / deepseek-flash high | 独立核对 |
| --- | --- | --- | --- |
| 重连恢复原对话 | 通过 | 通过 | 原生历史中的合成连续性标记 |
| 记忆、查找、更正、遗忘 | 通过 | 通过 | 本地 Memory 证据及清理结果 |
| 时间、事件源、纯 CEL 测试 | 通过 | 通过 | 原生工具回执；测试不发布事件 |
| 保存、查询、分别删除日历/事件安排 | 通过 | 通过 | Host 状态、命名空间及原生注册/撤销 |
| 一个独立 Worker 写入文件 | 通过 | 通过 | 仅一个文件，精确字节 `COMPACT_WORKER_OK\n` |
| 查询原请求、同任务续跑 | 通过 | 通过 | 原始请求查询、同一任务、续跑后文件不变 |
| 清单 lock/unlock/unpin/pin/clear | 通过 | 通过 | 原任务仍保留且可继续 |
| 桌面授权、观察、输入、增量及回执查询 | 通过 | 通过 | 真实控件值及 AppKit 独立 JSON：`submissions:1` |
| 到时唤醒 Bot 并调用表达 | 通过 | 通过 | 原生唤醒状态及 Host 表达回调；Caelis 从真实用户回合登记后台授权 |

两种真实模型均调用了全部 10 个新入口。原生调用记录分别包含 Codex 48 次调用、Caelis 41 次调用；计数来自工具调用/应用回调记录，未将模型正文提及工具名算作调用。

首轮 Codex 完成链路中有 4 次可纠正的工具错误：模糊的 `Local` 时区、过小的桌面预算、无效 `invoke` 参数和缺少验证条件。Host 现在提供实际 IANA 时区，无法确定时明确返回 unavailable；桌面 guide 和 Schema 已补充原生步骤约束。更新后的 Caelis 全链路 41 次回调无工具错误，并返回 `Asia/Shanghai`、`known` 和当前 UTC offset。此次结果证明所测流程可用，不宣称所有模型首轮零错误。

验收准备中另有两项非产品失败：测试 Worker 模型不受当前 ChatGPT 账号支持，以及独立测试复用请求 ID 导致 Caelis 拒绝绑定另一个工作目录。模型改为已验证可用的验收配置；每次独立验收生成新的 ID，同一次操作保持稳定身份。没有重放未知提交。

Caelis 真实桌面阶段约 29 秒；Codex 首轮约 220 秒。两者使用不同模型和运行条件，不作为新旧工具性能比较。完整目录 JSON 为 35,360 → 28,879 字节，减少约 18%；这是字节测量，不是 tokenizer 的 Token 数。

## 合同、生命周期与原生验收

- `make check`、`make smoke`、`make build` 通过。
- Bot、任务、桌面控制、Codex、Caelis、应用层竞态检查通过。
- 安装的 Codex App Server 通过 Bot-scoped progressive skill 加载验证。
- 安装的 Caelis NativeHost/GuardianHost 通过原生合同与审核集，包括图片内容块、撤销、Worker 隔离和真实 90 秒审核超时；该合同集的 provider/input 是受控 fixture，区别于上面的真实模型和真实桌面输入。
- 打包的 Desktop World helper 通过握手、有界读取、非法输入拒绝和原回执恢复。
- 新合同覆盖原 requestId 查询、分页、命名空间、能力过滤、超限结果保留原回执、Turn 撤销、过期 continuation、重复/非法参数及历史版本回调。
- 通过 `script/build_and_run.sh --verify` 构建、`--restart` 启动最新原生 Dev Bot，在独立数据目录检查首次使用、设置和聊天。实际输入中文验收请求后，Bot 通过新工具返回当前本机时间、`Asia/Shanghai` 时区并调用点头；界面由等待状态正常回到可输入状态，回复文本没有溢出。

本地日志位于 `.cache/tool-compact-*.log`，真实链路证据保存在 `.cache/compact-real/compact-*/`。这些是本机验收材料，不纳入公开仓库；只在摘要中记录模型、时间、工具名及验证结果，凭据和合成原生转录保留在私有绑定中。

## 验证边界

这次真实模型验收覆盖全部 10 个入口和上表正向链路。每个枚举分支的所有异常组合、图片/取消/停止交错、容量满、远端离线、旧后端切换恢复等仍主要由合同与生命周期回归覆盖；未把 fixture 结果写成远端实机证明，也未新增真实 Fedora 全链路验收。

未完成旧 22 工具与新 10 工具的受控模型 A/B、40 个场景留出集或统计显著性评估，因此不宣称普适成功率、Token 或延迟改善。此次变更没有新增用户设置页面、任务级 Runtime 选择、远端自动调度或工具权限。
