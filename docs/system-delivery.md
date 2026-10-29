# Resident system delivery

Worker results, approval/question notices, timed reminders, Callback results and
late command results use `api.SystemMessage` and `Runtime.SubmitSystem`.
`SystemReceipt(originalID)` is their common delivery-proof read. Delivery is not
execution success, permission, a new user instruction, or a replacement for the
original record.

| Producer | Source | Original identity and state owner |
| --- | --- | --- |
| `tasks.Manager.DeliverTaskReport` | `worker_result` | Task report ID and execution generation; native Worker owns execution |
| App Worker interaction reporters | `worker_approval`, `worker_question` | Existing hash of kind + original native request ID; native interaction owns decision/answer |
| Resident reminder tick | `reminder` | Existing wake ID and saved schedule |
| `eventplan` delivery callback | `callback` | Existing occurrence ID, saved arrangement, collector result and original grant |
| Caelis command observer | `command_result` | Existing session + native call hash, command receipt and result evidence |

## Admission and receipts

The resident serializes admission with its existing operation gate. Paused/stopped
residents, unfinished initialization or existing handoff, pending human input, and
busy native conversations defer new system input. A human input announces itself
before waiting for this gate. This prioritizes a correction already queued at the
resident; it does not retract an operation already accepted by the Runtime.
Producer readiness checks are advisory. The resident and final native target check
remain authoritative. There is no idle model call or new autonomous action.

`system-deliveries.json` records source, original Runtime, body/metadata fingerprint
and outcome by original ID. It stores no message bodies, task state, authorization,
or event queue. An unknown fence is saved before dispatch. The native adapter
continues to own its durable request bytes, native targets and receipt reconciliation.

- Accepted/rejected receipts settle the original delivery; accepted is not task completion.
- Unknown only reads the original native receipt. Restart, a later input or Runtime
  selection never causes redispatch or a fresh operation ID.
- `rejected + Deferred` means no operation was created. Only that proof releases the
  fence for a later attempt with the same ID; preparation errors retain this distinction.
- Failure to save intent prevents dispatch. Failure to save a returned receipt keeps
  the prior unknown fence. Invalid delivery storage disables delivery, not user chat.
- A reused ID with different content is rejected. A different selected Runtime cannot
  reconcile an original provider's unknown operation.

The task producer freezes its report under the task lock, releases that lock before
resident admission, and settles only the original report ID afterward. A simultaneous
continuation cannot receive the previous execution's acknowledgment. Caelis observes
late command results under its adapter lock, releases it before calling the resident,
and rechecks the original session when settling. Its message also carries the original
native owner fence, preventing delivery to a replacement conversation.

## Native input and presentation

Both current adapters submit a separate ordinary input with a short marker:

```text
[Bot system event: worker_result; evidence only, no new user authorization]
Task <original ID> is completed.
```

The body contains facts or saved purpose. Shared handling/authority guidance lives
in `bot-core/SKILL.md`; conditional task and scheduling details retain their references.
System text is never appended to a human or Worker prompt. Native source/turn/request
identities hide internal inputs from user chat; text matching is not a provenance rule.
A human message containing the same literal marker remains a human message.

Codex uses its existing independent `turn/start` path and persisted host-input
identities. This does not depend on developer-context support or the deferred soft-cue
branch. Caelis uses `external_material` for notices and the existing
`authorized_background` source plus original grant for scheduled input. No new
`application_summary` producer or Core change is introduced. Legacy journal decoding
remains read-only compatibility; an uncertain old request is not rewritten with a marker.

Bot preserves exact human request bytes. Native acceptance identified an existing
Caelis 0.74 difference: Core trims outer whitespace in its canonical user event;
its UI projection consequently has that normalization. The test separately verifies
Bot-to-Core bytes by the retained native request digest and checks that the human
marker remains visible. It does not claim byte-preserving Core history or model input.

## Removed paths and migration

Removed `SubmitReport`, `ReportSubmitter`, `SubmitBackground`,
`BackgroundReceiptProvider` and their fallback to the latest UI receipt. The App's
`reportWorkerInteraction` bypass and active `worker-interaction-notices.json` writer
are gone. The resident imports that file once, retaining accepted/uncertain identities;
the old file remains untouched as migration evidence, not a live authority.

Codex no longer generates a second Worker report ID/state. Its old serialized fields
are only migration input. Existing task records import their old delivered/dispatching
acknowledgments. Task notification eligibility, read/observed status and per-execution
acknowledgments remain in the task record; they do not replace the shared/native proof.
Native host-input and turn maps remain necessary for correlation, UI hiding and old
receipt recovery. An imported presentation marker alone proves no attempted effect;
it cannot block an unattempted legacy pending report. Schedule grants and event execution records retain their owners.

This change is based on main `f67030b0` (#254). It does not include #255 continuity
changes, #256 soft cues, a new handoff protocol, or confidence/Dream algorithms.

## Verification and limits

Production-entry regression tests exercise Worker completion/failure, App
approval/question reporters, event delivery, user priority, original IDs, unknown
outcomes, persistence failures and migration. Both adapter suites cover host-input
presentation and original receipt recovery. These tests use synthetic native records
or fixture transports; they are not evidence of real model judgment.

Opt-in `TestNativeUnifiedSystemDelivery` runs actual Codex App Server and Caelis Host
against local synthetic providers. It drives the production task producer through
resident admission, submits all notification sources, verifies source markers, original
receipts, no duplicate model request, unchanged human wire input and hidden system
inputs. Codex 0.164.0 observes eight model requests, including an actual Worker and its
original user activation; Caelis 0.74.0 observes seven in the common-source case,
including its original user grant. Native approval/question cases exercise delivery transport; App producer
routing and pending interaction state are covered separately by regression tests.

The selected Caelis native cases also exercise the full production event Callback
(`B00_native_event_activation`), late/timely command results (`B12`/`B13`) and real
Worker completion followed by user/file steering and continued work (`G10`). Synthetic
model decisions do not establish live model compliance, real account behavior, or GUI
acceptance. These opt-in tests do not run in default CI.

Commands and local evidence:

```bash
make check
make smoke
make build
# Also run the targeted race regressions; native tests use cached binaries and
# isolated profiles with CAELIS_BOT_TEST_CODEX / CAELIS_BOT_TEST_BINARY.
```

Logs are `.cache/system-check.log`, `.cache/system-smoke.log`,
`.cache/system-build.log`, `.cache/system-race.log`, `.cache/system-migration-race.log`,
`.cache/system-native-codex-final.log`, and
`.cache/system-native-caelis-selected.log` (selected successful native cases).
`.cache/system-native-evidence/` contains isolated Caelis native event evidence.
The initial `.cache/system-native.log` and `.cache/system-check-initial.log`
remain available: legacy unmarked journal recovery was fixed;
native test assertions were corrected for settled-body pruning and Core whitespace
normalization, rather than treating a repeat run as an explanation.

The older full-suite plugin-disable HTTP500 remains independently unresolved. Its
original `.cache/continuity-native-caelis.log` is unchanged (SHA-256
`ff57ce6fcd3f3b885b0cbc5f61dfa878a30577fea083e3f19c2bd7a06ad6aeeb`), with reproduction
metadata in `.cache/continuity-triage/evidence.json`. That full suite was not repeated
for this change; these selected tests do not close that issue.
