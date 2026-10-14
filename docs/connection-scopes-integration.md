# PR 164 connection and execution scope handoff

This branch is based on `6391669e`. Apply `94de4010`, `c1b87eb5`, `2809b0de`,
and this branch's final commit in order when integrating. The onboarding
`64dee69d` and Team `9036243a` commits are separate; the optional `scene` prop
in `RuntimeSettings.tsx` is compatible with the baseline wizard until they meet.

## Caelis Bot full access contract

Core `docs/application-runtime.md` (workspace and permissions section) says
`permissions.mode=danger-full-access` selects Host execution for native tools,
while `permissions.approval_mode` chooses the reviewer of approval requests.
Core `app/gatewayapp/application_execution_runtime.go` selects the Host sandbox
backend for that mode; `application_runtime.go` assembles its explicit native
policy. The SDK `DangerFullAccessMode` allows native tools without its own
approval or command filtering, and `endpoint_approval_policy.go` resolves
controller/participant/subagent permission options before the manual requester.
These are scoped to the new Application Session; they do not set a process-wide
`--dangerously-skip-permissions` flag or change an existing Session.

`manual` **does still request human approval** for actions outside the native
policy. In particular, Bot `application.go` marks every callback not explicitly
listed in `ApprovedTools` as `approval_policy=required`. Core
`control/application/callback_policy.go` returns `ActionAskApproval` for those
callbacks **before** the native policy in `application_runtime.go` is consulted.
Core `application_reviewer.go` resolves `manual` from the creation-bound profile,
and `internal/kernel/gateway_approval.go` enqueues those requests for a person.
The old `danger-full-access + manual` mapping therefore did not meet a setting
that promises no manual execution prompts.

The Bot mapping in this branch uses `permissions.mode=danger-full-access`,
`permissions.approval_mode=auto-review`, and an explicit creation-bound
`reviewer: {kind: guardian, model: <configured model>}`. Core's
`control/application/bindings.go` and `reviewer.go` accept that combination for
native `workspace-write` execution; the adapter verifies `reviewer-state=ready`
before a new Session is used. Required Bot callbacks still pass through Guardian,
which may deny or fail closed. `direct` callbacks remain limited to the explicit
Bot allowlist. Application binding, exact callback catalog, Host ownership,
background grants, OS permissions, account login, and missing user decisions
retain their own gates. Full access is therefore Host execution with automatic
review of required Bot actions, **not** blanket authorization of application
effects or a promise that every action succeeds.

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
This branch's Caelis profile test covers new `danger-full-access + auto-review +
Guardian` assembly and old-profile immutability. Core's existing permission and
profile tests cover native Host policy and validation; the callback/manual
conclusion is also supported by the exact policy and queue source paths above.
These are source and isolated fixture results. Native GUI flow, real account,
actual model judgment, Core Worker extension, and merged-head acceptance remain
for the coordinator's serial integration and independent review.

No Bot-facing skill change is needed: these switches belong to settings and
native admission. Bot tool schemas, skill discovery, and recovery instructions
do not change.
