# Architecture

Read [product boundaries](product.md) before changing behavior. Runtime compatibility and
approval policy live in [runtime integration](caelis-integration.md); commands and evidence
limits live in [development](development.md). Source contracts remain authoritative for fields.

## Ownership

```text
macOS surfaces → internal/app → backend.Service → adapter → native Runtime
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
| `internal/nodes` | Machine/Backend/Role capability registry; exact optional Worker routes, no native credentials or resident lifecycle |
| `internal/notebook`, `botmemory`, `botskills` | Markdown, embedded Memory and packaged application-scoped English skills |
| `internal/taskterminal` | Owned external terminal application instance and connection receipt lifecycle |
| `internal/desktopcontrol` | Desktop World Go host, independent turn grants, original receipts and bounded content |
| `frontend/src` | Presentation, transient interactions, character render resources; never execution authority |

`app.Host` injects OS actions. Desktop does not import concrete adapters. Unknown providers fail explicitly.
Optional capabilities are discovered by typed ports and native negotiation, not by text or animation.
Wails/AppKit/cgo remain behind macOS drivers. Unsupported hosts fail before writing preferences.
Shared-core compilation does not prove native Windows behavior. `GOWORK=off`; no sibling private imports.

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
Read-only Composer/Recent snapshots avoid serializing all history; older-page loading does not own lifecycle.

Independent owned Runtime/Worker supervision is available on Linux, including cgo-free builds,
and on macOS builds with cgo for the IOKit system-power fence. macOS without cgo reports
unsupported before launching a helper or preparing owned runtime state. Both the owner and
its watchdog need this capability; a native companion cannot supply the parent's missing fence.
The packaged macOS node agent enables cgo for this headless system binding without APP/Wails
or AppKit composition.
The watchdog confirms the complete owned process tree, including adopted Linux children,
before issuing a stop receipt. A separate inherited pipe preserves that receipt after autonomous
expiry closes the command channel. Missing receipts remain unknown even when the parent cleans
its known processes; a watchdog's exit or an empty parent capture is not complete stop proof.

Codex owns only the server it launched. Explicit stop/quit interrupts exact owned work, asks native terminal
cleanup, then reaps that process. macOS descendant fallback uses captured PID plus birth identity; no global
process-name kill. Closing/hiding UI has none of this authority. Shared Caelis Host/Workers survive Bot detach;
Bot neither stops the shared Host nor cancels native workers on ordinary close. Cancellation after dispatch
may leave an unknown effect, which cannot be called “not executed.”

Runtime switching is fenced by active work, approvals and unknown outcomes, then persisted for restart.
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

Notebook has `MEMORY.md`, generated `INDEX.md`, optional `HANDOFF.md`, and local-date `YYYY/MM/DD/*.md`.
Only INDEX is automatically rebuilt; it indexes Markdown paths/titles, ignores hidden directories/symlinks,
and refreshes at startup, before submit and on completion. No background model or body database owns notes.
User edits/deletions persist. Legacy personal data is copied once with a marker outside Notebook; conflicts
stop migration and preserve both sides. No automatic Git commit or cloud sync. Opt-in ordinary-file SSH backups and manual
stop-first node switching are assembled by APP; see [Notebook backup](notebook-sync.md).
They transfer neither native session bindings nor SQLite/authentication.

Embedded public Memory provides recall/remember/correct/forget in a stable Bot scope. Mutation request IDs
and receipt chains prevent forgotten evidence returning on replay. This does not erase chat, Git or backups.
Notebook is the resident writable workspace, not HOME; Workers have independent directories and instructions.
Neither scope claims isolation from another process under the same OS user or from full-access execution.

## Skills and context handoff

`internal/botskills/skills/` is the sole English Bot behavior source. Install complete directories into private
`app-skills`; only name, description and location enter resident instructions. Body/references are read on
demand through native file tools. Do not install globally or copy the Bot catalog/Notebook into Workers.
This is an application-scoped file catalog, not a registration into Codex skills/list or Caelis native Skill.
Update skill guidance for behavior changes; implementation-only fixes need no duplicate guidance.

First introduction is a normal user message with a durable stable delivery ID; until acceptance it precedes
other submissions. The host does not write the Bot personality or claim that acceptance proves memory saved.

Ordinary Dream is an opportunistic local decision, never an idle model loop. It needs a live resident
context gauge and a recent confirmed model response; tool progress, window activity and replay do not
refresh model age. Codex uses the last response's total tokens (not cumulative billing); Caelis requires
both explicit context_gauge and provider_usage evidence on the same live resident turn. Missing facts
skip the opportunity. Native compaction/model changes invalidate or rebase the gauge.

Defaults: below 30% context skips; 30–70% needs five continuous idle minutes; at least 70% needs 90 seconds.
At least 8,000 tokens of growth since the last attempt are required, with a 30-minute attempt cooldown.
The first observed conversation uses zero as its growth baseline; compaction rebases to the smaller gauge.
One attempt consumes the dirty generation, including rejection/failure. Dream's own usage is excluded
from subsequent growth when available. The host allows at most 15 wall-clock minutes since model activity,
reserving three minutes for its two-minute execution budget and margin. This is an opportunity budget,
not a provider cache-TTL guarantee or proof of a future cache hit. No keep-alive calls are sent.

Independent native presence sampling tracks sleep/wake and lock/unlock generations. Unknown presence,
disconnection, restart, clock reversal or a polling gap over 45 seconds requires fresh model activity;
waking also requires 60 seconds of stability. Sleep counts against cache age, not continuous idle time.
Draft edits postpone admission by 60 seconds. Expired opportunities never catch up after wake/cooldown.
Admission diagnostics contain reason codes, token counts and model age, never conversation content.

Dream never starts during active/pending/unknown work. It reuses the resident Bot,
explicitly loads the Dream skill, writes a marked HANDOFF, and emits a concise recap. Success requires native
completion plus a matching nonempty handoff. Normal success only marks ready; next user text/screen request
renews. An intervening background turn invalidates readiness. User input interrupts only the maintenance Turn;
Workers continue. Maintenance over two minutes requests cancellation. Failure does not start an idle retry loop.

Only a confirmed running native Dream projects `maintenance: dreaming`. Pet, bubble and chat share
that fact: a quiet napping pose/zzz and transient status, with static presentation for reduced motion.
Approvals, recovery, interruption and errors take precedence; completion clears the nap even while a
handoff remains ready. This status is not a transcript item, notification, or reason to show a hidden pet.
The host owns it; the model emits only its final recap. User submission retains interruption priority.

Runtime creation records the Bot version. After an App upgrade, recover/observe old work first; when idle,
reuse a valid handoff or initiate one after native presence stabilizes, then create current configuration.
This finite upgrade handoff bypasses ordinary context, cache-opportunity and cooldown gates. Same-version restart
resumes the existing binding. Preserve model/CWD/sandbox and history; don't hot-rewrite an unresolved runtime.
Explicit creation rejection ends renewal and allows input on the old binding; unknown creation keeps its ID.

New-context first input includes complete MEMORY and nonempty HANDOFF; later inputs do not rewrite prefixes.
Files are bounded regular UTF-8, no symlinks (128 KiB each; handoff validation 16 KiB). Consume HANDOFF only
after native acceptance is durably saved, and only if its digest still matches; concurrent user edits survive.
These files are historical data, not new authorization. Codex preserves old-thread history indexes; Caelis
preserves projections and associates existing reminder grants without inventing new user consent.

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

Worker location is separate from the resident Bot driver. `WorkTarget` binds a machine NodeID,
native backend and Bot/Worker role; a Host, Store, instance ID or SSH destination is not a NodeID.
The default APP plus local Runtime still calls its in-process adapter directly, with no node daemon,
SSH, enrollment or probe prerequisite. Optional targets are candidate/ready/unavailable from trusted
native assembly; selecting an unavailable target fails without moving work to another machine/backend.

One configured machine may expose both Codex and Caelis Workers. Native setup keys connection state
and actions by the exact NodeID/backend/Worker target; ID-only compatibility actions reject a machine
with multiple backends. The shared machine label and configured SSH association stay consistent,
while each backend retains its own immutable scope. Legacy empty-backend Caelis configuration keeps
its original protocol and private binding directory; adding Codex does not adopt or replace that binding.

`tasks.OpenRouted` adds optional Worker ports while preserving resident-provider watchlist/report scope.
The product ledger retains TaskID, exact target, target-owned workspace, request digest and source;
native adapters retain their original Thread/Session/Turn receipts. Old records acquire `local` plus
their existing backend/Worker role without changing IDs, native generations or prior completion receipts.
A repeated request cannot change target or task intent. Unknown starts return the original ledger entry;
read/continue/stop use its exact route. Remote state cannot adopt unrelated native workers or change a
record's target/workspace. A reconnected remote Worker may still project exact-route history
from another resident provider; those records remain inert under that original provider and do not
block current work. Current native-adapter ownership and target/workspace conflicts still reject.
Registry disconnection preserves the ledger rather than declaring cancellation.

Cross-target mutation requires `WorkSourceProvider` attestation from the resident native invocation;
model/renderer task arguments cannot supply it. Codex reports `native_activation` under its existing
native admission gate, because its current binding does not retain a distinct user/background kind.
Remote continuation intent is persisted before dispatch; reconciliation preserves its original source
and request digest. Output, completion notices, labels and target discovery are not authorization.

Remote workspaces use a target port: resolve canonical paths without mutation, persist intent, then
prepare/revalidate the exact directory. Client filesystem checks cannot validate a Linux path. Optional
Worker approval and artifact ports carry exact owned task/target bindings; approvals preserve original
native IDs/choices and reject stale bindings. Artifacts are bounded bytes from that task's projected IDs,
not arbitrary Host filesystem paths. The application owns any local download destination.

Linux scope is headless Runtime/Worker hosting behind these optional ports. This contract slice provides
fixture-covered routing foundations; SSH setup, native remote approvals/resources and actual Linux
execution require their own adapter/assembly and live acceptance. It adds no Linux desktop product,
high availability, ownership epochs, session transfer or identity/memory migration. macOS remains the
complete desktop release target; a remote Worker does not establish a remote resident Bot or thin APP.

`codex.WorkerClient` is a target-side Worker-only native client. It performs the standard
handshake/account projection without creating a resident Bot thread or inventing a resident activation.
Fresh mutations require the authenticated host invocation's exact native source; the private journal
retains original source, request digest and native binding. Receipt queries can reconcile that original
intent after the host advances to a new activation. Unknown native thread creation cannot be adopted or
redispatched. Work model defaults come from explicit target settings or the target's native config.

`internal/nodeworker.Owner` holds this client independently of observers: `Observer.Detach` cancels
observation only; explicit `Owner.Stop` interrupts owned turns, checks tool cleanup and stops its retained
native process. A native observation socket failure retains the original private endpoint; reconnect
restores exact retained threads and never silently launches a replacement process. The persistent node
service must own this owner. This slice supplies native protocol/process fixtures and Linux compilation;
Worker role RPC assembly, supervisor restart and Mac-to-Linux live acceptance remain separate gates.

`internal/workerwire` is a separate closed typed Worker stream with bounded versioned frames,
correlated requests and cancellation of admitted partial writes by closing that observer stream.
Large state snapshots span bounded sequential frames and become visible only when every frame
arrives with the same revision and request identity. Per-frame limits do not truncate the native
journal or impose a cumulative task-count limit. Interrupted or mixed snapshots remain unpublished.
Its configured pairing fixes WorkTarget, origin product Bot identity and allowed source Node/Backend.
Strict SSH plus a same-user private Unix socket authenticate the native entry; the target treats the
source as paired foreign attestation, never claims its Codex CLI can inspect a Mac native thread.
The node profile persists that pairing and its dedicated journal directory before exposing the socket.
Source is private wire metadata generated by the resident native port, excluded from model/renderer
TaskStart JSON. Target calls receive it through private per-call context, with no mutable shared source.
Exact original receipt queries need no new activation; only a missing intent can request the host's
fresh native attestation. Reads/cancel/decisions/resources accept task-owned identities, no arbitrary
Thread ID or shell command. Codex downloadable artifacts come only from completed native fileChange
projections inside that task workspace, read through a bounded root handle, never from assistant prose.
`nodes.CodexSSHWorker` consumes the frozen Worker ports and closes only its SSH helper/observer.
`Server.ServeUnix` and `ProxyUnix` likewise do not stop the persistent owner. Supervisor service/root
assembly and actual Mac-to-Linux model execution remain separate from these fixture-covered ports.

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

Terminal attachment resolves the task ledger's original Node/backend/Worker target and workspace.
The native adapter explicitly stamps endpoint locality; opaque Node IDs and local-looking paths do not
establish it. Missing locality, changed backend/workspace or a conflicting target fail before launch.
The current paired Worker/product streams provide no remote TTY attachment channel: remote routes report that absence without copying target credentials, executing a local substitute,
starting a new task or moving the original work. Local in-process terminals remain independent of
the machine's Node ID. Terminal settings and application-window ownership retain their existing scope.
An attached GUI instance retains its original endpoint and native Thread/Session binding; a task-card
show/hide gesture revalidates that exact target before mutating the window. Node disconnection or a
changed attach generation preserves the existing window, while explicit close remains available.

## Optional remote APP

`product-connection.json` is an explicit client connection choice, loaded before
local Runtime, Memory, Notebook, resident Bot or task initialization. Missing
pairing selects the existing in-process local path. Remote mode binds a pinned
Bot/Node/backend/role and generation through standard SSH and the closed F2
product stream; it does not start a local Runtime or remote owner.

The remote Bot owns conversation, tasks and memory. APP owns presentation
acknowledgements and user-selected attachment metadata. Closing or quitting APP
closes its SSH observer and detaches; only the separately authorized target owner
stop terminates the remote Bot. Reconnect is explicit. Pending original sends and
management requests retain their IDs and scopes; uncertainty blocks a replacement
mutation until the original receipt is resolved. A new owner generation is not
proof that an old command had no effect.

Remote management uses typed, capability-advertised target operations and the
existing configuration presentation. It has no generic method dispatcher or
credential-transfer action. Authentication entry belongs to the user on the
target Runtime. Linux desktop operations report unsupported; transport acceptance,
native APP readiness, visual GUI and authenticated model acceptance are separate.

## Desktop and presentation

Desktop World is the resident desktop backend for both Codex and Caelis. The pinned
public Go module and independently signed helper are v0.1.0-alpha.1, revision
`5a2ae97ddf65579d2d0051e82a33efd588f17942`. The old Cua/Node driver, native focus
ports and optional whole-desktop experiment are retired. F1 screen input and
passive character context remain separate. Codex resident configuration denies
native Computer Use app access when Desktop World is bound; ordinary sessions
and workers retain their own configuration.

`desktopcontrol.Controller` lazily starts the bundled helper through the public
`host` SDK. Data uses private stdio; independent FD 3/4 control pipes own
BeginTurn/Grant/EndTurn. Neither model arguments nor the renderer can choose a
helper, turn, process identity or startup grants. Only read/observe/sync/act/
capture/get/cancel are exposed. `bot_desktop_authorize` is separately reviewed
and checks the exact observed application Ref/name before Grant. App × Turn
approval covers all windows of that live application instance. Finish, interrupt,
shutdown and runtime replacement cancel the tool context and revoke even idle
grants with a fresh short control deadline. No native work executes in Wails.

Each tool requires a stable `requestId`; the managed helper derives epoch and plan
identity from its trusted turn and stable SDK envelope ID. Duplicate IDs with
identical arguments return the original response;
conflicting arguments fail. `bot_desktop_reconcile` remains available after a
turn ends, reads the original receipt and never sends input. Unknown/partial
results preserve their receipts. There is no automatic helper restart, backend
fallback or mutation replay. The SDK bounds retained requests/turns to 4096;
process restart loses prior receipt history and is not proof of no effect.

Observations use scope/fields/budgets and native cursor sync (upserts/removals,
coverage and reset_required). Reads default to 60 objects/8 KiB when no budget
is supplied. Ordered act plans support up to 16 steps and local predicates;
new dialogs require new observations before targeting their controls. Actions
return receipts, not implicit full trees or screenshots. `host.Content` bounds
text plus structured JSON to 32 KiB with a receipt-preserving overflow notice.
Caelis content-v1 suppresses only identical JSON text duplication.

Only explicit capture returns pixels, after checking model image support and
application authorization. The helper writes to a private temporary asset root;
Bot reads only that root, strips paths, validates tile dimensions, preserves the
image transform and compresses one image to at most 256 KiB. Multi-tile or
oversized captures require a narrower request. Reconciliation returns metadata,
never captures again. Capture is visible-region evidence; window_content and
unobscured background composition remain capability-dependent. Managed raw
points are forbidden; use object Refs or bounded object anchors.

Prefer semantic invoke/set_value operations when supported. The alpha shares
system focus and pointer; it provides neither a virtual mouse nor a universal
background delivery mode. Functionality takes priority over avoiding activation,
without promising that focus stays unchanged. Delivery is distinct from verified
postconditions. Desktop World frame/topology units are independent of the pet's
native placement coordinates below.

The managed JavaScript bridge is not part of this first Go host integration. The
upstream Node CLI uses a different startup authorization route and must not be
launched as an alternative writer. Distribution is currently macOS arm64 / 14+;
Bot re-signs its nested helper. Public preview availability grants no open-source
license; preserve the upstream NOTICE and separate Bot licensing.

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

System permissions are independent fresh OS facts. Explicit requests may prompt; background reads/capture never
request consent. Ordinary enable does not reset TCC; the separate repair action requires a checkbox and targets
one category/current bundle. No Full Disk Access request or direct TCC database edits.
