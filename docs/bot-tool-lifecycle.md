# Bot tool lifecycle and output audit (#222)

The current catalog has eight tools. Original Worker prompts pass unchanged through
Bot, task manager, local/peer routing and native adapters. Async answers use the
explicit question-reply envelope, with the original question identity; closure is
local state and never becomes model input. Native execution and permissions remain
separate from temporary presentation.

| Tool | Check and resulting boundary | Verification |
| --- | --- | --- |
| `bot_delegate` | Original prompt and invocation-derived receipt are retained. Unknown dispatch reconciles the same receipt. Host-validated original question answers no longer require a live resident turn; ordinary continuation and new task creation still do. The Codex lifetime limit of 100 prompt receipts was removed; original digests/outcomes stay retained for replay protection. | Native protocol start, active steer, idle continuation, 200 historical receipts, duplicate suppression; existing delegation/attachment tests. |
| `bot_tasks` | Summary pages have a byte budget as well as a count limit; follow `nextOffset`. Message pages retain their existing excerpts; exact message and observation reads now have lossless JSON chunks. Environments and SSH configuration can also be chunked. File bytes use checked local copies, not model JSON. Stop/pin return compact facts. | Output/read tests, native task history and original-request reconciliation suites. |
| `bot_interactions` | Only pending or unresolved originals appear in todo pages. Closed records remain ID-readable while retained. Large approvals/questions already have exact chunked reads. Typed mutation overflow now retains the original ID/status/outcome instead of losing its receipt. Answer errors record a local reason class and fingerprint, never answer text or raw errors. | Interaction pagination/Unicode/changed-cursor tests, answer receipt/race/expiry tests, idle-resident answer regression. |
| `bot_schedule` | Calendar lists omit full prompts; exact `calendar:<id>` reads recover them. Event and arrangement history now pages by count and bytes. Large definition, occurrence and source reads have executable chunk cursors instead of permanent `model_output_budget` errors. | Summary/history and exact calendar/event output tests; existing schedule retention tests. |
| `bot_schedule_update` | Compact receipts preserve IDs and timing. The event engine already bounds active arrangements, lifetimes and terminal history and preserves unknown receipts. Calendar's explicit 32 saved-definition limit has an existing exact `remove` operation; definitions and unknown wake receipts are not silently deleted or resubmitted. | Existing grant, retention, cancellation and no-replay tests. |
| `bot_event` | Registration returns compact handles/schema; does not execute. Large original definitions now have lossless reads. Existing immutable IDs, archive/revocation, orphan retirement and retained unknown occurrence rules remain. | Original event read regression and event-plan suites. |
| `bot_gesture` | Fixed enum and compact result; transient native presentation, no task authority or retained interaction payload. Hidden/reduced-motion rules remain local. | Existing gesture/presentation contracts. |
| `bot_dream` | Bounded handoff, native terminal-result gate and original call receipt; handoff replacement does not copy old tool output or Worker context. No closure instructions are added. | Existing handoff/renewal and Bot-scoped skill tests in both adapters. |

Retired `bot_memory`, `bot_care`, `bot_care_read`, desktop aliases and unsupported
legacy task operations remain unavailable. Frozen historical catalogs validate
original callbacks; they do not publish old schemas or replay mutations. The
pre-#222 catalog is frozen separately before adding read cursors.

## Temporary interactions and pairing

Question expiry/dismissal stays in TextChannel. The obsolete closure delivery
queue no longer generates Worker traffic. Legacy Codex closure context is ignored
and disappears from its binding at the next normal atomic save; existing host
report IDs stay intact. Complete terminal payloads in recycled question generations
are retained for at most 128 generations/seven days, then reduced to binding,
outcome and receipt tombstones. Existing IM cards keep their original wording. Native Worker question observation payloads also leave their fixed 24-hour cache, retaining a digest to reject replay. That original deadline travels through peer observations into the Host card; late first observation cannot extend it. Original unknown input receipts remain available.
Pending, dispatching and unknown originals are preserved. Anti-replay tombstones
and native task history are necessary evidence and are not treated as active todos.

New peer handshake IDs include an immutable five-minute deadline. Closed/expired
requests leave the inbox; expired timed records are removed during registry
maintenance. A retired ID cannot recreate a request, while an exact grant ID
continues to read as approved/revoked from the separate grant. The 16-pending and
128-current-window admission bounds remain. New random IDs are rejected; only existing legacy requests can be read or retried. Old random IDs retain their small
receipt records because their expiry cannot be recovered statelessly, but expired
history no longer consumes admission capacity. Grants, source proof and task
bindings are never pruned with a handshake. No native owner is opened by cleanup.

## Q24 failure evidence

The read-only historical timeline shows the resident Codex turn completed at
2026-10-25 13:00:01.508 UTC. The answer's local rejection receipt is timestamped
13:00:09.826 UTC; the Worker turn started at 12:58:19.138 UTC and was still active.
The task manager retained that answer as rejected; its native Worker binding has
no corresponding answer dispatch. The old `localWorkers.SendWork` and
`codex.SendWork` required resident `WorkAdmission`/`taskAdmission`, whose `s.run`
check necessarily rejects that idle interval. The same gate also existed in
remote routing. This accounts for the valid question being refused before native
dispatch; reading history could not repair a missing resident activation.

A typed host context carries the validated original question call through local admission; it never enters the prompt or model arguments. Ordinary continuation still requires native activation. The regression closes the resident turn through a native fixture completion,
then submits the original Worker answer. It also covers a completed Worker turn,
active steering, receipt history and replay. No historical answer was resubmitted.
New diagnostics classify answer rejection/uncertainty and fingerprint omitted
error details. Native fixture evidence is separate from live-model consumption,
GUI behavior and release artifact acceptance.
