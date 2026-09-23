# Caelis 候选基线真实模型验收

2026-09-23。Core 待发布 `origin/main`：`4a3c05964d6d240ec55414e189205930588ef099`。
**本轮覆盖的真实模型路径通过，没有发现新的 Core 发布阻塞。** 对应 Bot 修复与测试随本报告交付。
验收时 Core 尚未发布，未合并 Release PR #68，未升级日常安装。原生 GUI 和发行安装不属于本次验收。

## 固定输入与隔离范围

- 候选二进制：`/tmp/caelis-application-candidate-20260923/caelis`。
- SHA-256：`aebbce17da92156e7a18878f9afd6f47373e3b14e514dc40770ad8af6dea0b53`，本轮已重新计算核对。
- 公开 initialize 的实际 BuildID：`4a3c05964d6d240ec55414e189205930588ef099@2026-09-23T13:29:13Z`；`build_kind=dev`。
- Core 报告及 CI/源码树一致性证据：`/tmp/caelis-application-candidate-20260923/implementation-report.md`、`baseline.json`。
- Bot 协议清单已更新到该 SHA；仅固定公开 OpenAPI/Go wire，不引入 sibling Go 依赖。
- 使用实际 `xiaomi/mimo-v2.6-flash` 和 `openai-codex/gpt-6-luna`。MiMo 回执 effort 为 high；Luna 热切换与 Fast 使用 low。
- 用户指定的 MiMo 隔离配置来自 `/private/tmp/caelis-bot-mimo.ddXnIL/store`。在用户后续明确授权下，
  仅复制本机 Codex OAuth 认证供 Luna 验收；没有复制日常会话或修改日常配置。
- 本轮新 Store：`/private/tmp/caelis-bot-live-4a3c059/store`。Host 使用临时 HOME；独立 Notebook、worker
  目录与应用连接。模型仅接收合成验收请求。所有启动和停止都针对测试自行创建的精确子进程。

验收驱动为真实 Bot HTTP/SSE adapter、实际候选 Host、真实模型及 macOS 原生工具。
`LiveCheckpoint/LiveDelegate/LiveSchedule/LiveVersion` 是应用注入的验收工具，用于确定性交错和授权入口；
不是伪造的 provider 响应，也不代表完整 GUI 或生产秘书工具编排已全量验收。

## B01–B12

| 编号 | 本轮证据 | 范围与结果 |
| --- | --- | --- |
| B01 | 六项 initialize 能力；公开 replay 中原生 Read/Write/RunCommand | **通过**：实际模型驱动原生工具，无 tools-only 替代 |
| B02 | MiMo 写 MEMORY.md 和日期笔记、下一 Turn 回读；Luna 在 Host 重启后 Read MEMORY.md | **通过**：磁盘字节与核心哨兵核对，INDEX 自动更新；应用专属 memory skill 绑定。普通 Session 不继承仍由确定性/Core 回归证明 |
| B03 | MiMo→Luna，high→low；独立 priority 请求完成 | **通过**：实际 `last_request` 包含新 model/effort；Fast 记录 `service_tier=priority` 且真实请求成功。不是延迟、计费档位或所有模型支持 Fast 的证明 |
| B04 | 真实模型到达应用回调屏障时保存配置，再释放回调 | **通过**：desired revision 2、旧请求 revision 1；同一个 Turn 的下一请求采用 revision 2。模型 HTTP 请求本身正在传输时的快照稳定性另由确定性测试证明 |
| B05 | 旧 LiveCheckpoint handler 与新 integer schema 的 LiveVersion | **通过**：旧、新 handler 各一次，旧回调未被新目录接管；丢 claim/result 的恢复仍使用故障注入测试 |
| B06 | 笔记写读前后 revision 1；同值配置更新保持 revision 2 | **通过**：不把普通文件写入变成配置变更，不声明供应商 KV Cache 命中 |
| B07 | 停止测试 Host，启动同一二进制/Store，重连并实际再读记忆 | **通过**：应用 Session 和 revision 4 保持；Luna 继续执行。丢配置响应及精确历史回执使用既有确定性故障测试，不称为真实线上断网验证 |
| B08 | 两个真实 worker 的原生命令停在各自 FIFO 屏障 | **通过**：两者同时 working；只取消 A，B 不受影响；不是用 sleep 推断并发 |
| B09 | 用户来源 callback 创建 grant，SubmitBackground 实际调用模型，重复与撤销检查 | **通过**：同 ID 无新 Turn，撤销后拒绝。产品计时器、睡眠唤醒/跨时区不是本轮实测范围 |
| B10 | 上传→ReadResource→Read/RunCommand→PublishArtifact→Bot 下载项→实际下载 | **通过**：31 bytes，SHA-256 `c1afad38ddc8e0d11289dbb500c7c507ec4d88002eaeaea1c81ee02c6e354b34`。跨应用拒绝/坏摘要由本候选确定性测试覆盖 |
| B11 | 待审批断开重连、原审批目标一次性批准；worker 执行时 adapter detach/reopen | **通过**：批准前无 marker，重连目标不变，allow_once 后实际产生 marker；A interrupted，B completed。未知 cancel/approval 回执的完整产品恢复仍待补齐 |
| B12 | make check、相关 Go race、make smoke、make build | **通过**：Core CI/原生 Windows范围引用候选报告；本轮没有重跑 sibling gate，不声称 Bot Windows 或 GUI 已通过 |

## 实测发现与修复

候选 Host 在空闲终态可省略活动 Turn target。Bot 的结束 hook 只读取 `Run.TurnId`，导致真实模型
完成写入后漏刷 INDEX；worker 同样可能失去完成代次，或把多个 Turn 的助手正文混成一次结果。

修复：当活动 target 不存在时，从规范 Envelope 投影的最近 `TurnKey` 恢复展示和完成身份；
取消/审批继续重新读取原生精确 target，不使用该回退授权。`FinishedTurn` 仍去重。

- `TestIdleHeadWithoutTargetRefreshesNotebookOnce`：公开 HTTP 空闲头省略 target，验证 INDEX 刷新一次。
- `TestIdleWorkerReportsOnlyLatestCanonicalTurn`：旧结果不混入当前 worker 完成报告，执行代次稳定。
- 修复后完整真实模型 suite 通过，含 race；最终精确审批命令匹配另单独重验通过。

准备阶段还修正了两处验收环境/夹具问题：复制模型设置时排除了无关 Memory authority 配置；
待审批重连只等待连接恢复，不等待“可发送”。这些不是 Core 执行协议缺陷。

## 实测产物与复现

完整真实模型运行：`/tmp/caelis-bot-4a3c059-live-4.log`，suite 103.70 秒，7 个子项全部通过。
证据目录：
`/var/folders/hn/r4ffst5510s89657cwrcjj6w0000gn/T/caelis-bot-live-evidence-1492413941/`。

- `evidence.json`：实际模型/effort/tier、旧新 request/Turn/revision、字节摘要与逐项结果，无认证值。
- `public-replay-evidence.json`：通过公开 reconnect API 另行读取规范回执；主会话 107 个事件，
  取消/完成 worker 各 4/13 个事件；保留工具名称、状态和合成助手回复，不读取 Core 私有 Store。
- 最终审批守卫专项：`/tmp/caelis-bot-4a3c059-approval-final.log`。
- 确定性候选 Host：`/tmp/caelis-bot-4a3c059-native-final.log`；相关 race：`/tmp/caelis-bot-4a3c059-race.log`。
- 全仓检查：`/tmp/caelis-bot-4a3c059-check.log`；smoke/构建：`/tmp/caelis-bot-4a3c059-smoke-build.log`。

这些是临时本机证据，完整目录包含私有连接状态和隔离凭据，不应打包上传到公开仓库。
仓库只保留测试代码和本报告。初始环境失败及 INDEX 缺陷的中断日志保留在 `live.log`、`live-2.log`、
`live-3.log`（同 `/tmp/caelis-bot-4a3c059-` 前缀）；它们不计为通过结果。

复现前显式准备隔离 Store 中的模型和认证。下面会产生模型调用，并自行启动/停止该测试 Host：

```bash
CAELIS_BOT_LIVE_STORE=/absolute/path/to/isolated-store \
CAELIS_BOT_LIVE_BINARY=/absolute/path/to/candidate-caelis \
CAELIS_BOT_LIVE_MODEL=xiaomi/mimo-v2.6-flash \
CAELIS_BOT_LIVE_ALTERNATE_MODEL=openai-codex/gpt-6-luna \
CAELIS_BOT_LIVE_EFFORT=low \
CAELIS_BOT_LIVE_FAST_MODEL=openai-codex/gpt-6-luna \
GOWORK=off make smoke-caelis-live
```

夹具不复制凭据。提供 Fast selector 时，经公开 connect-model 接口使用隔离 Store 已有认证配置该模型。
未提供 Fast selector 则明确跳过 Fast；未提供 binary 则连接已有隔离 Host、不重启它，并跳过 Host 重启项。
子项筛选和缺失前提都记录为 `not_run`，不能以 suite 退出 0 宣称全部覆盖。

## 剩余限制与发布判断

- 没有发现需要阻止 Core `4a3c059` 发布的新问题。本报告不执行发布授权，也不合并 Release PR。
- 上传响应与 opaque resource ID 全部丢失时，仍无按上传 operation ID 的只读查询；保留未知，不猜 ID/自动重传。
- 未知 cancel/approval 的完整产品恢复、worker 制品进入秘书主对话、active Turn steer、实时权限切换仍未补齐。
- 本轮只验证应用注入验收工具和 adapter 端口；原生 GUI 的初始化/切换、真实用户整套秘书对话、
  长期驻留、发行安装和 Bot Windows 仍需各自验收。
- 本次没有独立压缩长上下文或做跨 Runtime 用户资料迁移；Notebook 真实闭环不能外推成完整长期记忆评估。
- 正式发行后仍应使用实际下载的发行二进制核对版本/摘要，并补一次隔离消费 smoke。
