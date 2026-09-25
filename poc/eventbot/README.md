# Event-driven Bot POC

结论：真实 Codex App Server 已验证零 Worker 状态轮询、双客户端协作及原生 TUI
共享会话。主动关怀采用可编程 CEL 条件加统一 prompt 激活；样例场景只在 JSON
配置中，未增加场景专用工具。锁屏、冷却、去重、过期和未知回执使用确定性测试。
实际 macOS 锁屏判断仍未验收，因此原生探针保留未知状态并阻止自主激活。

2026-09-24. Isolated executable and Go module; **not connected to the shipping
Bot, its tools, settings, schedules, or daily Codex sessions**. No changes to the
production dependency graph. No LaunchAgent is installed. No external model or
user credentials are used. Only `protocol`, `pty`, and `terminal` start a short-lived
local test service; `terminal` opens a new window in the associated terminal app.

## Decision tested

Use standard Codex App Server subscriptions for Worker progress and completion.
Use a small local event queue and [CEL](https://cel.dev/) predicates for programmable
activation. A rule emits a prompt to the Bot's ordinary runtime; it cannot execute
shell, make HTTP requests, or obtain OS capabilities. Tool actions after activation
must still follow the runtime's normal permissions.

The rule is data: `on` (source), `when` (CEL boolean), `prompt`, and
`cooldownSeconds`. [Examples](examples/rules.json) contain usage/date/custom-object
rules; these are replaceable configurations, not three specialized Bot tools.

```json
{
  "id": "relevant-change",
  "on": "custom.changed",
  "when": "event.items.exists(x, x.kind == 'important' && x.score >= 80)",
  "prompt": "Review the relevant change within the existing authorization.",
  "cooldownSeconds": 3600
}
```

Data sources publish events; the host supplies `now` and trusted presence separately.
No model runs to evaluate a predicate. The protocol demo also exercises a one-shot
Go timer: the event is queued while locked and delivered on the explicit unlocked
fixture. Elapsed time alone never changes presence to unlocked.

## Reproduce

From the repository root, with Go 1.26.8 and the user's installed Codex CLI:

```sh
bash poc/eventbot/run.sh test
bash poc/eventbot/run.sh replay
bash poc/eventbot/run.sh protocol
bash poc/eventbot/run.sh pty
```

`replay` uses the example rule and JSONL event files, emits activation metadata,
and never starts Codex. To try another rule, change the JSON file or run the binary
with `-rules /path/rules.json -events /path/events.jsonl`; omitting `-events` reads
stdin. These envelopes are trusted test fixtures, **not a public event ingress or
an authorization API**.

Optional native probes:

```sh
# Emits OS idle time and subscribes to public AppKit workspace notifications.
bash poc/eventbot/run.sh native 8
# Opens a new external terminal with the exact shared-runtime attach command.
bash poc/eventbot/run.sh terminal
```

The protocol/PTY probes require local listening sockets and native subprocesses.
The test runner creates a private temporary `CODEX_HOME`, a local synthetic
Responses provider and one App Server listening on a private Unix socket. It removes
its temporary data and stops its owned service afterward. The external terminal
window may remain showing process completion; only that POC client is stopped.
All provider replies are `POC_EVENT_ACK`; this is native protocol/TUI validation,
not a live-model quality, native-tool or approval acceptance test.

## Recorded results

Baseline: Bot `a63775a`, installed `codex-cli 0.156.1`, macOS Apple Silicon.

| Check | Result |
| --- | --- |
| Two independent clients observe the same Worker completion | PASS |
| Second client steers the active Worker turn | PASS |
| User-originated next turn reaches the Bot's existing subscription | PASS |
| Unsubscribe one client; Bot continues on the same Worker | PASS |
| Completion notice starts one synthetic Bot response | PASS |
| Programmable timer event starts one synthetic Bot response | PASS |
| Bot/protocol observer `thread/read` requests during these flows | **0** |
| Native Codex TUI displays the same persisted transcript in an isolated PTY | PASS |
| Native TUI input creates a turn observed by the Bot client | PASS |
| Default `.command` handler launches an external Terminal window | PASS; user confirmed correct visible Terminal output with a screenshot on 2026-09-24 |
| CEL compile/type checks, cost bound, input bound | PASS |
| Locked, asleep and unknown presence prevent dispatch | PASS, deterministic fixtures |
| Event body cannot set trusted presence | PASS |
| Cooldown, duplicate event, expiry and changed-rule suppression | PASS |
| Process restart does not repeat delivered or uncertain dispatch | PASS |
| macOS native idle-time read | PASS |
| CEL/Go POC Windows amd64 cross-compilation | PASS; no Windows runtime or native-adapter claim |
| Real lock/unlock/sleep lifecycle and user-presence policy | NOT VERIFIED |
| Real calendar/mail connector subscriptions | NOT IMPLEMENTED |

The zero-read count covers the two POC protocol clients. It does not assert that
the native TUI performs no initial history reads while attaching. The native TUI
proof comes from its real rendered terminal output and the Bot receiving its new
turn, not from merely finding a process or issuing `open`.

The user-supplied external Terminal screenshot also shows reconnect failure after
the short-lived POC service was cleaned up. This is expected for that test lifetime;
it is not evidence that production reconnect/session restoration has been verified.

Local evidence (ignored by Git): `.cache/eventbot-protocol-poc.log`,
`.cache/eventbot-pty-poc.log`, `.cache/eventbot-pty.txt`,
`.cache/eventbot-terminal-poc.log`, `.cache/eventbot-rules-test.log`,
`.cache/eventbot-rules-replay.jsonl`, `.cache/eventbot-native-events.jsonl`.
Repository `make check`, `make smoke` and `make build` also passed. Smoke needed
native process permissions outside the restricted sandbox; no model call was made.

## Small safety boundary

- CEL 0.32.0 is pinned in this independent module. Only standard pure functions
  and supplied data are exposed; no custom I/O functions or arbitrary script hook.
- 32 rules, 2 KiB expression, bounded parser recursion, 1,000 evaluation cost units,
  16 KiB event, 4 KiB prompt. Only boolean conditions are accepted.
- One serialized event loop owns the private ledger. Record `dispatching` before
  calling the sink. A lost receipt remains uncertain and is never automatically
  retried. Rule replacement invalidates previously queued intent.
- Unknown lock state blocks dispatch. No rule parameter can disable this gate.
- The ledger stops at 256 receipts rather than deleting idempotency evidence. This
  bound and one-hour pending expiry are POC choices, not production retention policy.
- The process-restart tests do not prove power-loss durability or cross-process
  coordination. Runtime receipt reconciliation, registration authority, global
  notification budgets and cancellation UI still need production integration.

## Why not directly install generated OS automation jobs?

macOS [launchd](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html)
already provides `StartCalendarInterval`, `WatchPaths`, and `QueueDirectories`.
It is useful for OS job lifecycle. It does not by itself provide Bot ownership,
arbitrary predicates, unlocked-user gating, message receipts, or a portable rule
contract. Giving generated jobs arbitrary shell execution would also introduce a
second execution/approval path.

For a resident Bot the smallest path is a local timer plus native/connector event
adapters, then the same portable CEL rule and prompt queue. Platform capabilities
are discovered; unsupported sources are rejected when registering a rule. CEL is
not a way to invent a missing mail subscription or reliable screen-lock signal.

The macOS probe uses public NSWorkspace notifications and CGEventSource idle time.
Session-active is not treated as proof of unlocked state. The probe therefore
publishes `Unlocked: null`; it will not autonomously wake the Bot from these samples.
Windows native adapters are not implemented by this POC.

## Integration after this POC

2026-09-25 后续状态：共享终端、Bot 规则工具及两种 Runtime 的后台激活现已接入正式代码，
见[任务委派](../../docs/task-delegation.md)、[主动关怀](../../docs/proactive-care.md)与
[准备记录](../../docs/preparation-status.md)。正式实现读取明确的本机锁状态，未知时禁止派发；
物理锁屏/睡眠切换与真实模型仍待实机复验，命令/外部连接器只有扩展接口。
以下保留当时 POC 的历史交接范围，不作为当前产品状态：

1. Codex: shared local endpoint, restore subscriptions after reconnect, and replace
   `watchChild` periodic history reads with events plus recovery reads. Update the
   Bot task projection for turns started by the human client. Preserve approvals.
2. Expose a compact work list and native attach launch. Attaching/detaching must not
   interrupt the task. Keep Bot operations on the protocol connection.
3. Add one generic trigger-registration capability to the existing Bot tool host;
   route emitted activations through its existing durable admission and diagnostics.
   This POC has no model-authored registration tool installed in the daily Bot.
4. Validate a real macOS lock/unlock adapter before enabling autonomous care.
   Add date/usage/connectors as event sources, without adding scene-specific tools.
5. Caelis follows the verified behavior: reuse canonical SSE and background grants;
   add user access to the exact managed Worker and in-flight messaging. Its current
   adapter requires a current user-originated call to register new background work;
   lasting user authorization must be represented explicitly rather than inferred
   from worker output. Keep condition evaluation in the Bot, not duplicated in Core.

This isolated POC does not modify production state. The production implementation
and its current verification boundaries are documented in the links above.
