# PR 164 connection and execution scope handoff

This branch is based on `6391669e`. Apply `94de4010`, `c1b87eb5`, `2809b0de`,
`beedf9f1`, and this branch's incremental review-fix commit in order when integrating. The onboarding
`64dee69d` and Team `9036243a` commits are separate; the optional `scene` prop
in `RuntimeSettings.tsx` is compatible with the baseline wizard until they meet.

## Caelis Bot full access: Core dependency

Verified against sibling Core HEAD `1305eff4d424199a68c6091d683d1d3bce09a612`
and Bot source at `beedf9f1`. Core `docs/application-runtime.md` under
"Workspace and permissions" defines creation-bound `permissions.mode` and
`permissions.approval_mode`. `danger-full-access` selects unsandboxed Host native
execution. It does **not** change the callback approval policy. Bot
`internal/backend/caelis/application.go` marks callbacks outside its small
`ApprovedTools` allowlist as `required`, including `bot_delegate` and
`bot_schedule_update`. Core `control/application/callback_policy.go` returns
`ActionAskApproval` for each such call; `app/gatewayapp/application_runtime.go`
evaluates that decision before the native policy. With `manual`, Core asks a
person. With `auto-review` and a creation-bound Guardian, Guardian can still
deny or time out. Neither choice gives Codex-like full execution access,
where Bot maps full access to `approvalPolicy=never` and
`sandbox=danger-full-access` in `internal/backend/codex/execution.go`.

The **minimal Core dependency** is a negotiated, creation-bound Application
Session execution approval contract that distinguishes Runtime execution review
from independent application authorization. Do not invent a field name in Bot
before Core defines and generates it. Core must specify and test how full access
handles native command/file execution, MCP execution, and `required` Bot callback
execution. For a callback that has passed the separate Bot/Host authorization,
full access must not send it through Guardian solely as a Runtime execution
review. Core must continue exact connection and catalog binding, schema and
argument validation, callback claim/receipt idempotence, Host ownership and
explicit background grants. App-level Computer Use/turn consent, account login,
OS permissions, and missing user decisions stay independent. Core must expose
the effective creation-bound approval contract in the returned binding, with a
capability for negotiation. Fixtures must prove: auto-review still routes its
required execution calls to Guardian; full access does not; independent Host and
OS gates still apply; existing Sessions keep their original profile; unknown
contract values fail closed. Reviewer readiness alone cannot prove this.

Until that Core contract and Bot adapter integration exist, Caelis Bot offers
only automatic review. `ExecutionOptions` omits full access,
`SaveBotExecutionMode` rejects it for Caelis, and new creation or handoff refuses
a pending full-access profile. Existing bound Sessions recover under their
original immutable permissions. Codex Bot full access is unaffected. To enable
Caelis after Core integration, remove the guards in
`internal/backend/caelis/settings.go` (`ExecutionOptions`),
`internal/backend/execution.go` (`SaveBotExecutionMode`), and
`internal/backend/caelis/application.go` (`rejectUnsupportedFullAccess` and
`rejectUnsupportedCreatedBinding`, also called by `context.go`). Replace the
provisional mapping in `internal/backend/caelis/reviewer.go` with the generated
Core contract, verify effective mode/approval/reviewer against the pre-dispatch
creation snapshot, and update the Caelis button detail in
`frontend/src/settings/runtime/RuntimeWorkspace.tsx`. Do not add all callbacks
to `ApprovedTools`, infer authority from model prose, or change an old binding.

## Integration interfaces and gates

- `SelectedLocalWorker`, `InspectWorkerRuntime`, `SelectLocalWorker`,
  `SaveLocalWorkerModel`, and `SaveLocalWorkerExecutionMode` operate on the
  separately saved Worker runtime. An unready target cannot become the Worker
  default. Maintenance of either connection does not switch the Bot or Worker.
- `BotExecutionMode` and `SaveBotExecutionMode` store only a future Bot Session
  preference per Runtime; invalid persisted values fail closed. New Bot
  connections and natural handoffs consume it; existing sessions keep their
  original immutable profile. Worker preferences live in each provider's
  `work-execution.json`; no Bot mode is inherited by a Worker.
- `RuntimeSettings.tsx` passes `scene="first-run"` only from onboarding and
  calls `onDone` after `ActivateRuntime` succeeds and the refreshed overview
  confirms a committed choice. `RuntimeWorkspace.tsx` is management only.
- Codex Worker full access stays disabled in `LocalWorkerSettings.tsx` (the
  full-access button), `internal/app/worker_settings.go`
  (`SaveLocalWorkerExecutionMode`), and the two guards in
  `internal/backend/codex/work_execution.go`. The capacity worker owns the
  Codex task creation/receipt pinning; remove these guards only after its
  end-to-end fixture proves every new task receives its selected mode and
  existing task continuations keep the old one.
- Caelis Worker mode controls are replaced by an unsupported explanation in
  `LocalWorkerSettings.tsx`. `internal/app/worker_settings.go` and
  `internal/backend/caelis/work_execution.go` reject mode writes. The separate
  Core worker extension must first accept a creation-bound permissions/reviewer
  request on `/application/workers`, verify the Host policy, and return the
  effective binding. Then wire the mode at the adapter's Worker create point;
  keep Core user/background authorization and existing Worker bindings intact.
  Remove these guards and show the Worker buttons only after that fixture passes.
  Do not use the Bot Application Profile as a substitute for Worker authority.

## Evidence and remaining acceptance

The repository fixtures cover all four Bot/Worker Runtime combinations, an
unready Worker target, failed and cancelled Bot switching, original task route
retention, and newest draft revision on rollback. `script/runtime-settings.test.mjs`
covers busy release after coalesced/failed refresh and first-run setup state.
This branch's Caelis fixtures cover the provisional
`danger-full-access + auto-review + Guardian` assembly, the new-binding
readback fence, the Core-contract gate, and old-profile immutability. They do
not prove the pending Core full-access contract. The callback conclusion is
from the exact Core policy source paths above.
These are source and isolated fixture results. Native GUI flow, real account,
actual model judgment, Core Worker extension, and merged-head acceptance remain
for the coordinator's serial integration and independent review.

No Bot-facing skill change is needed: these switches belong to settings and
native admission. Bot tool schemas, skill discovery, and recovery instructions
do not change.
