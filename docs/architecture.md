# Architecture

Read [product boundaries](product.md) before changing behavior. Runtime compatibility and
approval policy live in [runtime integration](caelis-integration.md); commands and evidence
limits live in [development](development.md). Source contracts remain authoritative for fields.

## Ownership

```text
native desktop surfaces → internal/desktop assembly → internal/app → backend.Service → adapter → native Runtime
                        ├── bot / care / tasks / notebook / botmemory
                        └── desktopcontrol → private Desktop World Go helper
renderer → typed product DTOs; no credentials, native IDs or local paths as authority
```

| Owner | Responsibility |
| --- | --- |
| `internal/desktop` | Menu bar, native windows/focus/geometry, OS permissions, capture, input, notifications |
| `internal/app` | Single product assembly, configuration, start/quit and admission fences |
| `internal/backend/api` | Product DTOs and explicit engine/capability ports; host-only ToolConnection stays private |
| `internal/backend/{codex,caelis}` | Native protocol projection, identity, execution policy, receipts, replay and recovery |
| `internal/bot`, `care`, `tasks` | Persistent identity, introduction, scheduling, care budget, delegation ledger and reports |
| `internal/notebook`, `botmemory`, `botskills` | Markdown, embedded Memory and packaged application-scoped English skills |
| `internal/taskterminal` | Owned external terminal application instance and connection receipt lifecycle |
| `internal/machines` | Native SSH profiles, host trust, machine identity, original task routing and cached observations |
| `internal/remotework`, `cmd/caelis-remote` | Linux headless owner and bounded typed SSH operations; full native Worker execution |
| `internal/desktopcontrol` | Desktop World Go host, independent turn grants, original receipts and bounded content |
| `frontend/src` | Presentation, transient interactions, character render resources; never execution authority |

`app.Host` injects OS actions. Desktop does not import concrete adapters. Unknown providers fail explicitly.
Optional capabilities are discovered by typed ports and native negotiation, not by text or animation.
`internal/desktop/assembly.go` constructs the sole `app.Application` and service
bindings without Wails/cgo. The macOS runtime owns Wails/AppKit windows, lifecycle,
restart, clipboard, native permissions and updater. `app.Host` supplies small
effect callbacks; `internal/secretstore.Store`, `localipc.Endpoint` and the
terminal script boundary keep OS authority local to their consumers. Frontend
feature DTOs distinguish supported, available, enabled and permission state;
permissions never overwrite a saved product choice. Unsupported hosts fail before
writing preferences. Windows 11 x64 native drivers, current-user Named Pipe,
Credential Manager, Job Objects and GUI acceptance remain the next stage.
Shared-core compilation and native Windows CI do not prove Windows GUI behavior.
`GOWORK=off`; no sibling private imports.

The General settings login switch reads macOS `SMAppService.mainApp` through a
native desktop adapter. Registration and approval status is system-owned,
separate from product feature preferences and permissions. A login launch skips
only automatic first-run window presentation; the normal menu bar, pet, single
instance owner and original-ID recovery still start. Unimplemented platform
adapters report unsupported without a writable switch.

Codex worker execution, saved task receipts, terminal observation, and Bot
subscriptions have separate lifetimes. A terminal turn starts a short Bot idle
grace; the adapter then uses read-only `thread/read` to confirm the exact last
turn and native idle state before calling `thread/unsubscribe` on its own
connection. The App Server retains the loaded runtime until its own
no-subscriber/no-activity grace expires; another client's subscription or a
running turn prevents that unload. Neither path archives history. Reconnection
reads unresolved workers without loading completed historical tasks; explicit
continuation resumes the same native thread and request ledger. Diagnostics
distinguish Bot subscription retirement from later native `thread/closed` unload.

The Bot IM transcript is a private local SQLite message log containing only user
and assistant messages with their original request/item identities. It loads a
200-message window and explicitly pages older **local** messages, including while
the Runtime is offline. Streamed text updates the same item; tool payloads are
never chat history. Disk failure retains the live cache and retries independently;
a corrupt display database is preserved as a damaged copy and rebuilt. Runtime
sessions, execution state and original action receipts stay with their native owners.

Codex connects with `thread/resume(excludeTurns:true)`, reads execution metadata
with `thread/read(includeTurns:false)`, and optionally synchronizes one latest Turn
through `thread/turns/list(itemsView:summary, limit:1)`. It never requests full
history or traverses past Runtime sessions. Optional display sync failure does not
block connection or input. Worker observation uses the same lightweight path and
retries separately; a failed Worker subscription cannot detach the resident observer.

Wire projection drains tool output and inline image bytes before JSON decoding or
queue admission. Native approval schemas, scopes and decisions remain intact.
Approval projection also identifies the native owner: resident conversation and
its blocking children share one gate, while only a confirmed independent task
approval may coexist with resident Telegram typing. Missing owner stays blocking.
The 8 MiB projection bound is an allocation safeguard for a single projected value,
not a Runtime/Worker history limit. A completely drained oversized value fails only
its correlated read/display operation; subsequent approvals, completions and RPCs
continue on the same connection. Partial/invalid framing or notification queue
exhaustion restarts only that observer, retaining original owners and uncertain
receipts. Queues remain bounded (512 events, 16 MiB); fast reconnect uses finite
backoff, followed by quiet periodic retries. Started frames have a bounded parsing
time while idle connections can remain open.

Control snapshots use immutable caches, so slow component disk IO cannot hold
status reads. Native control admission has context deadlines; local atomic writes
have a bounded wait and never claim durability after a timeout. Logging has its own
bounded best-effort queue. Optional feature assembly runs after native surfaces are
ready and retries without quitting the application. Invalid native identities or
receipt journals are preserved and reopened after repair; they never authorize
replacement owners or uncertain-effect replay. Window rendering failures remount
presentation independently of native work and other windows.

Telegram checkpoints each received update and its original input in a durable
inbox before advancing its polling offset. Callback, control and ordinary-input
lanes are independent, so recovery, inference and downloads cannot stop polling or
`/status`. Callback receipt feedback precedes disk/native waits and grants no
execution authority. Approval claims remain durable-before-dispatch; their original
cards converge to submitted, uncertain or terminal state. Reconnect completion and
failure have explicit feedback. An unavailable Runtime keeps original messages
queued for admission after recovery. A quit/sleeping Mac still cannot receive them.

Caelis display recovery requests one recent Turn, preserving its exact cursor and
command evidence. Stream and Worker failures retry independently of the resident
connection; original commands/callback receipts retain their native recovery rules.

Caelis callback recovery reads the full receipt snapshot once per connection /
stream generation, including claimed calls. Subsequent reads use the native
pending-call wait endpoint, with a 45-second cancellable idle wait, instead of
polling completed tool payloads every 250 ms. The effect still runs only after a
confirmed claim; unknown claims and original result receipts retain their recovery
semantics. A retired connection cannot dispatch a response from its old wait.

## Telegram companion chat

`internal/telegram` mirrors one paired private chat through pinned Telego. It uses
native `backend.SubmitRemote` and the existing resident submission, interruption,
approval and artifact ports. It does not add transport information to Bot context
or change conversation identity. Desktop drafts and attachment selection remain
independent. Token storage uses the macOS Keychain; pairing requires a short-lived
link and desktop account confirmation. Polling cursors, original input outcomes,
Telegram message IDs and text digests are saved privately. Uncertain creates and
inputs are never replayed; streamed edits are coalesced and rate limits respected.
Only approval deliveries retain bounded original card text in the private
Telegram ledger, so a terminal edit can keep its request after recovery. The
edit reuses the original message ID, removes buttons, and appends a decision
label only when the original native request resolves with a correlated selected
response. A resolution without that decision remains neutral.
Approval buttons use the same `api.Choice` values as the native panel: provider
`Label` remains literal, while Bot-owned `LabelKey` resolves through the shared
display catalog. An unreadable or ambiguous choice leaves the entire approval
keyboard unavailable and directs the user to the Mac. Telegram callbacks require
the original delivered message, a current pending approval, and an offered choice;
the durable decision claim prevents a second native submission.
The Telegram adapter renders mirrored Mac user items with a source heading and
quoted body. Each long Mac user-message part carries its heading and UTF-16 entity
ranges. Assistant replies retain their plain original text, including edits and
long-message parts. Display labels never enter Bot input or change native request/
item identity. Existing plain-text delivery digests are honored so a formatting
upgrade does not republish history.
Assistant Markdown uses native `sendRichMessage` with `rich_message.markdown`.
Messages containing raw HTML syntax use escaped `sendMessage` HTML so literal
model text is not consumed as markup. An explicit format or method rejection
can also fall back to `sendMessage` HTML,
then exact-source plain text only after an HTML format rejection. Stream edits
keep the original Telegram message ID. Authentication, chat,
permission, rate-limit and unknown network results do not change send format.
Local diagnostics record only API method, numeric Bot API error code, a locally
classified reason, and a fingerprint of the original delivery key with its part
number. Telegram descriptions, chat IDs, tokens and content are not logged.
Previously journaled definite rejections remain recorded and are not
automatically backfilled after an update; the Mac transcript holds their text.
Backend recovery establishes native input authority before remote input is
accepted; local display history is independent. One cancellable output worker coalesces snapshots independently of input
and control actions. A separate cancellable worker sends Telegram typing actions
only while the connected main Turn is sending or working. It renews at a bounded
rate, stops for unresolved approvals, uncertain or terminal outcomes and disconnects, and
does not persist actions as conversation history or receipts. Rate limits and
transport failures cannot delay input, decisions or recovery. Configuration
changes join both workers. Attachment identity
uses the submission and attachment index, preserving distinct same-named files.
Static stickers use their downloaded image bytes. Animated and video stickers use
the Telegram thumbnail as one explicitly labelled frame; a missing or invalid
thumbnail is a rejected input, never a claimed understanding of the animation.
The adapter supplies ordinary Bot image input and retains the original Telegram
message request ID, receipt and no-replay rule.
Host task and command completion reports enter through `SubmitReport` as internal
inputs. Codex persists their exact client IDs in the native binding and imports
delivered or uncertain IDs from the existing native product task ledger before
history projection; Caelis persists `application_summary` in its command
journal and correlates the canonical turn. Both project the input as `hostNotice`
through live updates, reconnect and older history. Presentation and Telegram
skip that kind before reading text or attachments; the subsequent assistant
result remains visible. No prose pattern or renderer-supplied flag establishes
this source. Unknown command admission fences uncorrelated input until the
original receipt identifies it.
Approval keyboards use the pending native option IDs and the Telegram message
ID that carried them. A text-only approval gains buttons by editing that same
message when choices arrive. The first callback claims the original approval
durably; later callbacks, stale message IDs and unknown decision results fail
closed. The native head and option check remains authoritative. Recovery
baselines terminal status notices; a lifecycle notice is tied to its original
user receipt in the current Turn so an old receipt cannot label new work.
The native macOS proxy resolver executes automatic configuration through CFNetwork,
honors the ordered system candidates and cancels PAC work with the HTTP request.
Failed or stalled PAC resolution continues to subsequent system candidates; DIRECT
is used only when explicitly present in that list. A PAC attempt has a three-second
budget, independent of request cancellation. Exhausted candidates report a network
failure. App-level DIRECT retains OS routing, including third-party TUN proxies.
This selection happens before dispatch and does not replay uncertain HTTP writes.

## Weixin text POC

`internal/weixin` is a Go adapter for the published Tencent iLink JSON client
protocol, pinned to the inspected `@tencent-weixin/openclaw-weixin@2.4.9`
source. It shares the resident `backend.SubmitRemote` path with Telegram and
keeps its own private cursor, owner-bound inbox, original ingress outcomes and
outbound chunk intents. Keychain holds the bearer token. Pairing QR state lives
only in memory; settings starts or refreshes it while visible. Start/stop
presence and disposable typing requests are independent of message receipts.
Only owner private text and final assistant text are in scope. A send with an
unknown HTTP outcome is never replayed; see [POC scope](weixin-channel-poc.md).

## Remote machines

The resident adapter remains local. `machines.Service` multiplexes the existing
`api.WorkRuntime` port by an explicitly selected machine; absent targets stay local.
`tasks.Manager` persists the machine and actual backend before dispatch. `WorkRouter`
freezes the backend before workspace preparation; retries never re-resolve a default. Remote
workspaces are resolved on that machine, never through the controller filesystem.
Remote request identity and ownership survive a resident adapter change. Existing
task routes cannot be removed or retargeted by editing the SSH connection.

Local `localWorkers` retains owned bindings from both provider directories. Its
independent default is stored in `worker-runtime.json`; an inactive adapter exposes
only Workers, never its resident conversation or Bot callbacks. The current resident
still validates native delegation authority before any start or continuation. Per-
provider settings preserve store identity, models and account ownership. Task backend
bindings survive a resident change and remain visible if their owner is unavailable.

Remote v1 routes migrate once from their locked profile runtime to a durable per-task
backend map. Observation caches are keyed by machine and backend; reading, sending,
stopping and terminal resolution explicitly address that retained backend. Changing
a default keeps both owners alive and affects only newly bound requests.

Native OpenSSH owns transport and authentication. Host keys require an explicit
fingerprint confirmation and changed keys fail closed. Passwords/passphrases are
write-only temporary inputs, optionally saved in the macOS Keychain. They are absent
from readback DTOs, JSON state, command arguments and diagnostics. The short private
multiplexing socket directory rejects symlinks and group/world permissions. Existing
SSH aliases and a single configured ProxyJump are conveniences; direct host/user/port,
agent, password and private-key inputs are first-class. Script ProxyCommand and
multi-hop configuration are outside this slice.

An explicit authenticated connection ships only the app-built, checksum-verified
Linux amd64/arm64 helper into a private target directory. It installs no Runtime and
copies no model credentials. The SSH proxy connects to a detached, per-profile owner
over a private Unix socket. Fixed typed operations expose detection, readiness,
node-local model settings, optional Caelis Team configuration and existing work ports;
there is no arbitrary renderer/model-supplied command endpoint.

Private Codex/Caelis `WorkOwner` wrappers expose only native worker contracts. They do
not expose a resident Bot or Bot tools. Codex persists its original native endpoint
before task dispatch and refuses replacement when that endpoint disappears. Caelis
uses native application worker receipts and target-local credential references.
Readiness checks account, model and native work admission separately from optional
Team configuration. Model defaults are canonicalized through the native catalog;
invalid persisted selections never silently revert to another model.

Background observation reads native facts and projects lost connections as unknown
work/offline machines. It snapshots routes under the machine lock, performs SSH
and readiness checks outside it, and accepts results only for the unchanged
profile and original backend. Local task operations do not wait for remote polls.
New routes remain durable pre-dispatch reservations until the Worker dispatch
fence; preparation or ledger failure releases only these reservations. Legacy and
unknown dispatched owners are never released by preparation cleanup. A restart
can remove an empty machine containing only pre-dispatch reservations.
Reconnection reads original bindings; it never resends work.
`TerminalTarget.SSH` is host-only, assembled by the connection owner. The terminal
script allocates SSH PTY and runs the original `codex --remote ... resume ...` or
`caelis attach ...` on the target. Remote paths and token-file references stay on the
target. Closing the observer does not close the native Worker owner.

The renderer uses one machine editor with responsive CSS, a stable draft, fixed
connection action, guarded background navigation and scoped advanced failures.
Node login remains a clear target-TUI/device-auth guide; OAuth/ACP flows that the
native surface cannot complete have an explicit TUI placeholder. See the
[acceptance record](evidence/remote-machines-v1/acceptance.md) for live and fixture limits.

## Execution and recovery

Native request/Thread/Session/Turn/item identities remain private and exact. The renderer receives opaque
handles, typed lifecycle facts, and native approval choices. Missing/null/explicit-empty fields differ.
A displayed message, task card, gesture, tool discovery or accepted dispatch never proves execution success.

Persist mutation intent and stable request ID before dispatch. Accepted/rejected/unknown remain distinct.
Unknown operations only reconcile the original receipt; never resend with a fresh ID or switch Runtime.
Early terminal events win over late acceptance. Replayed items replace/deduplicate facts; old history pages
cannot replay approvals, create workers or overwrite live state. Callback dispatch uses the original catalog,
configuration revision, tools version and invocation; an unavailable old handler fails rather than rerouting.

Outbox bubbles use client IDs or exact native receipt targets, never matching prose. Rejected/unknown input
retains the draft. Host drafts persist across surfaces/restart but are not an automatic outbox; restoration
never sends. Screen submission has its own durable receipt and cannot consume the composer draft.
Terminal local bubbles stay beside their original history anchor while visible
and retire if bounded Runtime replacement removes it. Image presentation
restores only unknown or in-flight input; accepted/rejected image receipts stay
available for canonical history by original request ID without resurfacing as
new tail messages after restart.
Read-only Composer/Recent snapshots avoid serializing all history; older-page loading does not own lifecycle.
Caelis startup/reconnect requests only one recent Turn. Explicit older-page loads use the existing eight-Turn traversal and merge decided mirror reviews with messages at one persisted boundary. A bounded page failure retains the live cursor, seen watermark, approvals and receipts; historical reviews never become actionable approval targets.

Codex owns only the server it launched. Explicit stop/quit interrupts exact owned work, asks native terminal
cleanup, then reaps that process. macOS descendant fallback uses captured PID plus birth identity; no global
process-name kill. Closing/hiding UI has none of this authority. Shared Caelis Host/Workers survive Bot detach;
Bot neither stops the shared Host nor cancels native workers on ordinary close. Cancellation after dispatch
may leave an unknown effect, which cannot be called “not executed.”
Conversation Stop records exact native targets before a bounded pre-cleanup attempt. A durable prepared state
means no turn interruption was dispatched and permits an explicit retry of those targets; once dispatch is
attempted, reconnect only observes the original native turn and cleanup result. Terminal native status remains
authoritative when a prepared Stop was never sent.

Resident Runtime replacement is fenced by active work, approvals and unknown outcomes, then persisted for restart.
Worker-default changes are live and have no task-state fence; existing tasks keep their native owners.
A read-only check or selecting a settings tab does not change the execution owner. Model/effort/tier changes
may apply to the next native request; directories, sandbox and inheritance are create-time configuration.

## Durable data

Default data is under `~/Library/Application Support/Caelis Bot`; Dev uses `Caelis Bot Dev`.
`CAELIS_BOT_DATA_DIR` selects an isolated acceptance profile. Private state uses same-directory temporary
files, sync and replacement with restricted permissions. macOS rename behavior is not a Windows guarantee.

Bot identity, Notebook/Memory, appearance and product task records are shared across Runtime choices.
Native bindings/credentials and scheduled execution remain provider-scoped; old Codex root files remain
readable, Caelis uses `providers/caelis`. Switching never migrates an unknown operation to another provider.
Caelis application credentials are distinct from Host credentials. Logs/diagnostic export contain allowlisted
states/counts/versions, not conversation prose, paths, native identifiers, arguments or credentials.

Notebook has `MEMORY.md`, generated `INDEX.md`, historical `HANDOFF.md` from older versions, and local-date `YYYY/MM/DD/*.md`.
Only INDEX is automatically rebuilt; it indexes Markdown paths/titles, ignores hidden directories/symlinks,
and refreshes at startup, before submit and on completion. No background model or body database owns notes.
User edits/deletions persist. Legacy personal data is copied once with a marker outside Notebook; conflicts
stop migration and preserve both sides. No automatic Git commit or cloud sync.

Embedded public Memory provides recall/remember/correct/forget in a stable Bot scope. Mutation request IDs
and receipt chains prevent forgotten evidence returning on replay. This does not erase chat, Git or backups.
Notebook is the resident writable workspace, not HOME; Workers have independent directories and instructions.
Neither scope claims isolation from another process under the same OS user or from full-access execution.

## Bot tool catalog

Bot-owned portable plugin packages and their reviewed source adapter are described
in [plugin-packages.md](plugin-packages.md). Package MCP services do not join the
built-in automatic approval list.

The complete resident catalog contains ten tools: `bot_memory`, `bot_tasks`,
`bot_delegate`, `bot_schedule`, `bot_schedule_update`, `bot_desktop_inspect`,
`bot_desktop_authorize`, `bot_desktop_act`, `bot_desktop_result`, and `bot_gesture`.
Unavailable Worker/scheduling/desktop capabilities remain absent. Typed request
variants use application-owned schemas and native dispatch validation. Delegation,
standing-arrangement writes and app grants remain separately reviewed; read/list,
watchlist/stop and permitted desktop operations retain their original direct policy.

The new catalog never advertises legacy aliases. Private MCP invocations carry a
catalog generation, and Caelis restores the exact original legacy schema/policy
version for old pending callbacks. This preserves original receipts without
rerouting them to the current same-name schema or replaying mutations. Unknown
catalog versions still fail closed. Task request lookup uses the owned ledger;
missing/ambiguous records are unconfirmed. Old follow-ups without a recorded
request identity still need their original task handle. Worker defaults, native
bindings, approval policies, Notebook isolation and event/calendar grants have
independent semantic owners.

Base replies expose `ok`, bounded native `data`, retained `outcome`, and useful
receipt-based `next` only where appropriate. Large structured replies avoid a
second JSON copy; output overflow retains mutation identity and reports its
budget limit rather than replaying. Task and arrangement lists are paged. Desktop
receipts/pixels retain the public SDK's stricter projection contract.

## Skills and context handoff

`internal/botskills/skills/` is the sole English Bot behavior source. Install complete directories into private
`app-skills`; only name, description and location enter resident instructions. Body/references are read on
demand through native file tools. Do not install globally or copy the Bot catalog/Notebook into Workers.
This is an application-scoped file catalog, projected through the resident Runtime's selected Skill paths.
Update skill guidance for behavior changes; implementation-only fixes need no duplicate guidance.
Codex receives the resident Skill metadata guide in developer instructions. Core uses its selected
`skill_roots` to publish the same metadata in each model request, so Bot does not also append that
guide to Core profile instructions. Both adapters load Skill bodies only on demand.

First introduction is a normal user message with a durable stable delivery ID; until acceptance it precedes
other submissions. The host does not write the Bot personality or claim that acceptance proves memory saved.

The Bot maintains Notebook and `MEMORY.md` during ordinary work. When obsolete task
detail or recalled tool schemas are substantial, the gated `bot_dream` contract
requires a bounded handoff summary. Its result must force completion of the owning turn before
another same-batch tool effect or model step. The handoff carries identity, current
goals, unfinished work and original uncertain receipts. Neither an idle threshold
nor a version upgrade dispatches a maintenance turn. The retired `bot-dream` Skill
is removed from fresh installations.

The APP keeps handoff data in a private durable record keyed by original call and
source Session, outside the Notebook. The new Session's first accepted Host input
contains current `MEMORY.md` and the handoff as saved context, never as a fabricated
user request or higher-priority instruction. Only durable acceptance consumes the
matching private digest. Normal inbound messages, task feedback, MCP connection
configuration and auth stay with the Bot; old model history and ToolSearch results
stay with the old Session. An unknown creation is reconciled under its original ID.

Legacy `dream-*.json` receipts are read without dispatching another maintenance
turn. A matching completed `HANDOFF.md` may migrate once into the private record;
the Notebook file remains untouched. Rejected renewal continues the old binding.
Runtime tool publication remains gated on an atomic terminal-result contract in
both adapters. A tool result followed by a best-effort cancellation is insufficient.

## Scheduling and care

Reminders run only while Bot is alive. Schedules and accepted/unknown dispatches retain exact original IDs.
Admission waits for initialization, idle native state and update fences. Background text is typed automatic
input, not a fabricated human message. Silent marker handling uses the same projection in chat and accounting.

`care` evaluates bounded CEL over registered JSON events; no I/O functions or arbitrary script eval. Built-ins
are clock.minute, desktop.usage and desktop.appChanged, carrying metadata rather than screen/input content.
`on` or nonempty `onAny` is exclusive; normalized sources share one rule, cooldown and pending occurrence.
Conditions see the current event only. Unavailable sources cancel their queued events but retain rules/other sources.

Every dispatch rechecks fresh native presence (unknown/locked/sleeping deny), busy state, expiry and budget.
Persist dispatching before native calls. Unknown/uncompleted work reserves one possible interruption;
visible body/artifact/manual approval/failed review/error consumes it once, silent completion releases it.
Reviews must belong to that native Thread/Turn; in-progress/approved or unrelated reviews do not consume it.
Human steering ends automatic attribution. Once visible, a later silent completion cannot refund it.

Default policy: 8 visible interruptions per rolling 24 h, minimum 5-minute dispatch gap; 32 rules/sources,
32 unresolved records, 256 terminal receipts, 2 KiB CEL/1,000 cost, 16 KiB event, 4 KiB prompt. Cooldown minimum
60 seconds, default 1 h; expiry default 1 h. v1 attempts migrate as bounded legacy reservations, not invented
visible events. Per-source host observation watermarks survive receipt pruning and reject replay/out-of-order data.

Rule mutation disables/revokes old queued work durably before changing native grants. Failed storage freezes
care without losing unknown IDs; corrupt/unreadable care stops only care, preserving files and ordinary reminders.
Reminder receipt recovery runs before care admission. Only an exact accepted receipt successfully saved releases
its unknown barrier. Other rules can use remaining budget once native idle is known.

Host adapters register `CareSources` and publish via `Application.PublishCareEvent`; renderer/test inputs cannot
publish real events. A future connector owns consent, cadence, bounded output/schema, cancellation and credentials.
No gh executor, arbitrary script discovery, mail or calendar subscription is implied by this extension point.

## Tasks and external terminals

Product tasks own directories, ordering, visibility/locks and finite reports. Native Runtime owns execution,
permissions and worker history. New worker instructions do not inherit resident skills/private tool endpoints.
Reports are application notifications, not new user authority; execution status comes from native events.

A task card owns a dedicated GUI application instance established by LaunchServices completion and PID/birth,
not window title, TTY, foreground state or count. A single controller serializes intent. An existing app returned
by launch is rejected. Open Documents delivers a one-use attach script; its receipt proves command delivery,
not a window identity. Standard Quit waits for actual exit and honors terminal cancellation without strong kill.

A terminal may acknowledge a document after cancelling script execution. After the native reply, allow a bounded
three-second receipt grace, then atomically revoke the unclaimed token; only proven revocation permits explicit
retry. Concurrent claimed receipt wins. Unknown launch retains its pending owner, including a late callback.
A confirmed exited TUI client can reconnect in the same owned GUI instance; do not inject into its old shell.
No automatic reconnect, task prompt replay or idle instance cleanup. Closing a terminal does not stop a Worker.
Hide/show, placement and still preview are optional driver capabilities; unsupported actions preserve ownership.

## Desktop and presentation

Desktop World is the resident desktop backend for both Codex and Caelis. The pinned
public Go module and packaged `dtw` helper are v0.1.0-rc.3, revision
`43172bb1f3dd26b90a88c346bc9978cbabab5785`. The old Cua/Node driver, native focus
ports and optional whole-desktop experiment are retired. F1 screen input and
passive character context remain separate. Codex resident configuration denies
native Computer Use app access when Desktop World is bound; ordinary sessions
and workers retain their own configuration.

`desktopcontrol.Controller` lazily starts the bundled helper through the public
`host` SDK. Data uses private stdio; independent FD 3/4 control pipes own
BeginTurn/Grant/Declare/Revoke/Grants/EndTurn. Neither model arguments nor the renderer can choose a
helper, turn, process identity or startup grants. The model sees `bot_desktop_inspect` (outline/text/delta/image/grants),
`bot_desktop_act`, and `bot_desktop_result` (status/cancel). Native typed operations
stay separate below the application adapter. `bot_desktop_authorize` is separately reviewed
and checks the exact observed application Ref/name before Grant. It may instead
declare one exact app name or window title through the same reviewed tool for an
app not yet running; the helper exposes pending/ambiguous/unresolved/active state
and binds only after complete unique discovery. A reviewed revocation removes a
specific grant during the turn. App × Turn approval covers all windows of that
live application instance. Finish, interrupt,
shutdown and runtime replacement cancel the tool context and revoke even idle
grants with a fresh short control deadline. No native work executes in Wails.

Inspection IDs are host-generated. Only input/cancel use model-supplied stable
`requestId`; the managed helper derives epoch and plan identity from its trusted
turn and stable SDK envelope ID. Duplicate IDs with identical arguments return
the original response; conflicting arguments fail. `bot_desktop_result.status`
remains available after a turn ends, reads the original receipt and never sends
input or new pixels. Cancellation cannot cross turn revocation. Outline
continuations restore the original query from a turn-scoped host cache; model
query fields cannot be mixed with a continuation. Unknown/partial
results preserve their receipts. There is no automatic helper restart, backend
fallback or mutation replay. The SDK bounds retained requests/turns to 4096;
process restart loses prior receipt history and is not proof of no effect.

Observations use scope/fields/budgets and native cursor sync (upserts/removals,
coverage and reset_required). Reads default to 32 objects/8 KiB when no budget
is supplied. Ordered act plans support up to 16 steps and local predicates;
unknown future dialogs require new observations; known uniquely scoped controls
can use native bind or `bind_focus` within one plan. Actions
return receipts, not implicit full trees or screenshots. `host.Content` bounds
text plus structured JSON to 32 KiB with a receipt-preserving overflow notice.
The Bot retains bounded step delivery/verification facts on overflow in addition
to original IDs, outcome, seat health and input restoration. Full evidence stays
in the original host receipt. Caelis content-v1 suppresses identical JSON text duplication.

The trusted host fixes `InputModeCooperative` and `InputShared`; neither is a model
argument. Semantic desired-state operations verify automatically without physical
fallback. A cooperative physical plan borrows focus for known click/keyboard/submit
steps together, then restores the prior app/window or yields to an app switch by
the user. The whole plan is checked before dispatch for 256 UTF-16-unit text bursts,
500ms drags and no raw Points. The helper owns the 1-second input budget, hit-test
convergence and cleanup. No plan splitting, truncation, mode fallback or replay is
automatic. Restoration failure remains unknown/fenced, not a success claim.

Only explicit capture returns pixels, after checking model image support and
application authorization. The helper writes to a private temporary asset root;
Bot reads only that root, strips paths, validates tile dimensions, preserves the
image transform and compresses one image to at most 256 KiB. Multi-tile or
oversized captures require a narrower request. Reconciliation returns metadata,
never captures again. App-scoped `capture_windows` returns dedicated capture Refs;
`window_content` requires one of those, a full target, no region/cursor and preserves
`image_to_target` without inventing a desktop mapping. Capture Refs cannot be used
for AX traversal or input. Visible-region evidence can include occluding apps,
requiring their grants. Hidden/minimized windows and capture capability remain
explicit native limits. Managed raw points are forbidden; use object Refs or anchors.

Prefer supported semantic desired-state operations. Cooperative input temporarily
uses system focus/pointer; it provides neither a virtual mouse nor universal
background delivery. Same-app human input is best effort; restoration is reported
in the receipt, not promised. Delivery is distinct from verified postconditions. Desktop World frame/topology units are independent of the pet's
native placement coordinates below.

The managed JavaScript bridge is not part of this first Go host integration. The
upstream Node CLI uses a different startup authorization route and must not be
launched as an alternative writer. Distribution is currently macOS arm64 / 14+;
Bot re-signs its nested helper. Public preview availability grants no open-source
license; preserve the upstream NOTICE, THIRD_PARTY_NOTICES.md and separate Bot licensing.

Native logical coordinates use primary-display bottom-left origin, Y up, including negative coordinates.
Device pixels/DPI do not belong in saved character scale. AppKit owns nonactivating panels, hit masks, drag,
focus, Space/display changes and geometry; Three.js owns model/mixer/local poses and GPU resource disposal.
Scaling updates geometry, projection, anchors and hit coverage coherently. Transparent clicks need real OS tests.
The passive bubble owns an always-active AppKit tracking area and forwards enter/exit facts to the
renderer without activating the app. WebKit's key-window hover delivery is not its native trigger.
The renderer retains delayed collapse, bounded scrolling and explicit approval interaction.

Desktop/Dock/ActiveWindow context records freshness/source and unknown/estimated fields; foreground does not
prove task association. Local behavior consumes facts without invoking an idle model. User input and pending
decisions preempt decoration. Independent paper-plane surfaces are bounded/pass-through and reclaimed on hide,
Space change or disposal. Planned movement/intent receipts must not be advertised as current bot_gesture semantics.

Streaming is presentation-only: a continuous 60–120 grapheme/second budget survives snapshot boundaries and
completion, with a capped time step after a stalled frame. New replies remain eligible even when first observed
as completed; arrival tracking is independent of avatar/execution state. Initial/reopened/reconnected history
and prepended pages establish a baseline without replay. Authoritative final Markdown remains exact, and
approvals, stop controls, copy actions and execution state never wait for the text animation.
Raw HTML is disabled and remote images are links, so reading cannot fetch resources.
Reduced motion/history/hidden surfaces display directly. GL contexts own and remove their canvases on disposal;
StrictMode/repeated previews must create fresh renderers and release textures/materials/bones.

Chat portraits consume finished, model-derived sprite atlases from official character contract v3;
they never construct a Three renderer. Only the current reply/work/approval portrait animates;
historical rows use a neutral poster. Typed activity selects thinking, searching, focus, replying,
waiting or Dream clips. A live observed successful turn edge can play one completion clip for six
seconds; opening/reconnecting history, quiet work and failures cannot trigger it. Waiting labels
share the same facts: visible current-turn assistant streaming suppresses tool/thinking dots,
while approvals, reviews and interruption retain their control priority. Completed commentary
does not suppress ongoing tool work. Animation never drives execution or authorization.
Codex recovered agent messages have no native item status; only live item-start/delta events
mark them in progress. An unknown-status recovered commentary cannot hide an ongoing tool.
Completion portrait eligibility expires with its six-second target; reveal-arrival history is
independent and cannot keep a completed portrait animating or revive it in a later tool-only turn.
Caelis projects main-scope ACP text chunks as in progress until an explicit final frame,
new foreground tool/thought segment or turn terminal. Child events and sparse tool updates
cannot end a concurrent reply; resumed text on the same message remains eligible for reveal.
The player keeps at most three cached atlas entries (each decoded atlas at most 8 MiB), loads on
visibility, fences late loads, crossfades through a small canvas and cancels frames on hide,
offscreen, reduced motion and disposal. Explicit custom PNG avatars keep their existing behavior.

Native screen input owns immutable image/thumbnail bytes and its independent durable receipt; renderer receives
opaque handles. Capture preferences freeze at acquisition; redaction affects both images. Submitted documents
cannot change scope. Ask Bot rechecks negotiated model image support immediately before dispatch. Unknown delivery
never resends; unreadable receipts remain uncertain. Attachment maintenance uses native Trash, protects originals,
selected/active files and symlinks, and never constitutes image resubmission.

The optional Extras gate owns only the manual F1 selection/annotation/Ask Bot and
F3 clipboard pin tools. Native admission, Carbon shortcuts, menu commands and
in-flight selection share the persisted gate; disabling cancels admitted sends
and keeps their original uncertain receipts if dispatch already began. It closes
temporary panels without deleting retained chat attachments or capture records.
Re-enabling restores saved child shortcuts and recoverable capture records. The
gate does not govern ordinary chat images or Desktop World task observation and
capture. Screen Recording remains a separate macOS permission; preference reads
and disabling never request it.
First-run feature selection has its own native completion marker, separate from
the permission guide. New installs save the chosen capture gate before marking
the feature step complete; an old completed permission guide migrates without
repeating the feature step or changing existing capture preferences. The
permission step reads the confirmed gate only to omit an unnecessary Screen
Recording prompt. A ready Runtime can resume setup after both markers complete.

Ordinary received images have a separate App-owned presentation store keyed by
the original submission ID. Local and remote submissions save bounded bytes before
native dispatch; saved user items and pending outbox rows resolve the same opaque
image IDs after history load or restart. The renderer reads only validated IDs
through `MediaImage`, never file paths. It does not label ordinary images as screen
captures, alter native model input, or infer acceptance from displayed bytes.
The saved caption is distinct from attachment filenames, so restored cards show
the user's text once alongside each image name.
PNG/JPEG/GIF tiles use a bounded retained JPEG thumbnail and load original bytes
only when opened. WebP uses its retained original for both views because the
renderer decodes it directly. Missing or invalid images show an unavailable tile.

System permissions are independent fresh OS facts. Explicit requests may prompt; background reads/capture never
request consent. Ordinary enable does not reset TCC; the separate repair action requires a checkbox and targets
one category/current bundle. No Full Disk Access request or direct TCC database edits.
