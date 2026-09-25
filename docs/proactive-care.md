# 主动关怀与可编程条件

2026-09-25：`poc/eventbot` 的 CEL 方案已接入生产 Bot。首批数据源为本机当前分钟、
使用时长、前台应用变化；规则通过同一个 `bot_care` 工具列出、试算、保存和删除。
没有新增常开控制台、操作系统后台作业或场景专用工具。运行时退出停止采集，隐藏桌宠不中断。

## 数据与执行边界

```text
本机元数据 / 已注册连接器或采集器
  -> 有界 JSON Event（来源名称、宿主观察时间、数据）
  -> 本地 CEL 条件
  -> 持久化待激活记录
  -> 锁屏/睡眠/忙碌/预算/有效期检查
  -> 原有 Codex/Caelis 后台 prompt
  -> 正常工具、审批、静默结果与通知
```

`internal/care` 负责纯条件、来源目录、规则版本、持久化与准入；`internal/bot` 将其接入
现有 Tick/update/initialization 生命周期、工具宿主与后台提交；`internal/desktop/care_darwin.*`
提供元数据采样。CEL 0.32.0 固定，无自定义 I/O 函数。不是任意 JavaScript/Python/shell 回调。

- `clock.minute`：当前分钟，规则通过指定时区的 `local` 字段判断日期/窗口；不补跑退出期间的分钟。
- `desktop.usage`：约 30 秒提供 `activeSeconds`、`idleSeconds` 和应用 bundle ID。
  近期输入表示正在使用；累计跨应用，空闲 5 分钟、锁屏/未知、睡眠采样间隙或重启清零。
- `desktop.appChanged`：约 1 秒采样中观察到的 bundle ID 变化，含前后应用；不承诺捕捉瞬时切换。

没有采集窗口标题、屏幕、输入内容或 URL。查询活动会话使用
[CGSessionCopyCurrentDictionary](https://developer.apple.com/documentation/coregraphics/cgsessioncopycurrentdictionary())；
明确锁状态读取 XNU 发布的 `IOConsoleLocked` Boolean（[Apple 源码](https://github.com/apple/darwin-xnu/blob/main/iokit/Kernel/IOService.cpp)）。
该 registry 属性不是稳定的 AppKit 锁屏 API：缺失或类型改变一律未知，禁止主动发送。
仅 session-active、经过一段时间或 event 内的 `unlocked` 都不能授予在场状态。
每次发送前重新采样，仍不构成锁屏事件与模型执行之间的原子 OS 事务。

## 规则与交付

规则包含 `id/label/on/when/prompt/timeZone/cooldownSeconds/expiresSeconds`。
CEL 输入是 `event`、时间戳 `now` 和指定时区的整数 `local` 字段。
`test` 只编译并评估示例，不保存或发布事件；`list` 同时给出来源字段、队列状态与预算。
英文 Bot 使用指引与可复制样例只有一份：[proactive-care.md](../internal/botskills/skills/caelis-bot-memory/references/proactive-care.md)。

每 Runtime 保存独立 `care-<runtime>.json`，不切换执行已有规则。修改/删除先持久化禁用旧版本、
撤销待发记录，再操作 Caelis 原生 grant；未知授权保持禁用，同内容重试保留候选版本。
Caelis 只从真实 user 来源创建/更新后台授权，不能把 worker 或自动输出冒充用户请求。
Codex 使用现有应用专属工具配置与显式 `bot_care` 自动批准条目，不扩大全服务审批。

到期前排队，不 steer 当前用户回合；最多每规则一个待发项。先持久化 dispatching，再调用后端。
明确拒绝结束该 occurrence；未知不重发，原生保留回执可在后续用户消息之后继续核对。
磁盘写失败冻结主动关怀实例，重启后按原生事实恢复，不回滚为可再次发送的旧状态；
错误仍可查询，但不阻断独立存储的普通提醒。关怀授权 ID 使用普通提醒不可用的独立命名空间。
同一来源的宿主时间水位防重复和乱序重放；每个来源需按观察顺序发布，不能用远程对象更新时间替代。

边界：32 规则、32 个注册来源、2 KiB 条件、1,000 CEL 成本、16 KiB JSON、4 KiB prompt；
冷却至少 60 秒，默认一小时，过期默认一小时；全局每 5 分钟最多一次、滚动 24 小时最多
8 次主动关怀尝试。最多 32 个未决记录，终态回执保留最近 256 项，时间水位不随终态回执裁剪。
无定时 LLM 空转；只有命中且满足准入才激活。普通提醒和用户提交不受关怀预算影响。

## 可扩展来源与 gh/脚本结果

来源并非引擎中的场景白名单。`care.Source` 描述名称、用途和字段；宿主通过
`app.Host.CareSources` 注册，`bot_care list` 自动列出。应用内适配器调用
`Application.PublishCareEvent(ctx, care.Event{...})`，与内置源进入同一个条件/队列路径。
此接口不暴露给 renderer/MCP/HTTP，模型的 test 数据也不能发布真实事件。
来源暂时消失会显示 `source_unavailable`，取消其待发项；规则保留。

例如，未来 GitHub 采集器可以运行明确配置的 `gh` 参数，转换结果为：

```json
{"pullRequests":[{"number":42,"reviewRequested":true}]}
```

注册来源 `github.pullRequests` 后，条件为：
`event.pullRequests.exists(pr, pr.reviewRequested)`。也可接 webhook、连接器事件或自有
脚本输出；均无需改动 CEL 引擎。`gh` 安装存在不等于来源已注册，本轮不宣称已交付 gh
执行器、任意脚本自动发现、邮件或外部日历订阅。

实现采集器时必须在适配器一侧落实：

1. 明确采集配置及原有用户授权，限定 executable/argv/cwd/env，禁止由 CEL 拼接 shell。
2. 明确 cadence、超时、退出/取消、输出字节上限和 JSON schema；采集失败不是空集合。
3. 保持凭据在连接器/进程环境中；只发布判定所需字段，不记录原始私密输出。
4. 使用宿主接收时间与串行事件顺序；重连只发布当前新事实，不无界回灌历史。
5. 采集器启停受应用生命周期与配置所有者管理，不能另建绕过审批的任意命令执行工具。

上述执行器职责独立于 CEL。生产扩展接口与自定义 JSON 测试已实现，具体命令/连接器
适配器在接入时单独实现和验收。

## 验证

`make check`、`make smoke`、`make build`（macOS ad-hoc）、受影响包 race 和技能校验通过。
关怀核心语句覆盖率 89.2%，不等于硬件或真实模型效果覆盖率。

`internal/care` 的单元测试覆盖表达式类型/语法/成本、数字和集合、字段错误隔离、时区/DST、
重复与乱序、冷却/合并、锁定/未知/忙碌、过期、预算、规则替换/移除、授权失败、崩溃窗口、
持久化失败、并发、容量、回执保留及自定义 JSON 来源。Bot 测试覆盖工具到后台提交、更新暂停、
原生 grant/撤销、不可伪造 presence 和跨消息原生回执核对。

`script/care-native-test.sh` 运行生产采样代码，当前机器读到 awake/known/unlocked；
未主动锁屏或睡眠，物理锁/解锁、快速用户切换与 OS 版本差异仍须实机复验。
Codex/Caelis 隔离原生 Runtime + 合成 provider 测试覆盖 CEL 命中后的后台提交、授权和静默投影；
它们不证明真实模型的关怀质量或 Skill 自主遵循效果。无需真实账户或付费模型。
