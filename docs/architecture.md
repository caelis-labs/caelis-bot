# Architecture

Read [product boundaries](product.md) before changing behavior. Runtime compatibility and
approval policy live in [runtime integration](caelis-integration.md); commands and evidence
limits live in [development](development.md). Source contracts remain authoritative for fields.

## Ownership

```text
macOS surfaces → internal/app → backend.Service → adapter → native Runtime
                        ├── bot / care / tasks / notebook / botmemory
                        └── desktopcontrol → private Node/Cua helper
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
| `internal/desktopcontrol` | Cua helper process, cancellation, private Turn updates and bounded content |
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
stop migration and preserve both sides. No automatic Git commit or cloud sync.

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

Runtimes with native Computer Use retain ownership; the Bot never installs a competing fallback.
Other Runtimes use the private Cua helper through `bot_desktop_observe/authorize/perform`. App input
grants remain App × task Turn. The adapter checks native PID/window identity, not AX title equality.
Bounded AX walks expose paginated targets, query and an explicit larger walk; unseen/truncated nodes
are not evidence of absence. Element pages share one snapshot and expire with it.

Element input, exact-window shortcuts and screenshot-based window input share the same authorization
and cancellation path. Image points use the native capture dimensions; clicks consume Cua's immutable
capture receipt. Scroll and drag use the same fresh snapshot and checked window geometry. Pixel focus
and keyboard input are separate mutations with observation between them. No desktop-wide targeting
or arbitrary native-tool passthrough is exposed. Each perform dispatches one mutation and observes
again; unknown effects invalidate references and are never replayed.

Explicit `focus` is a separate authorized window mutation. The private helper validates the current
observation and App × Turn grant before requesting the native shell's focus port. The shell verifies
CGWindow ownership and exact AX window identity, raises it and confirms the foreground focused window.
Only that private pipe carries native IDs; no arbitrary launch, script, global input or automatic retry
is exposed. Cancellation closes this exchange before the helper can resume. This fills the gap where
Cua's foreground delivery reports dispatch but the inactive app does not accept the event.

The private pipe permits bounded native PNGs; Go preserves dimensions and first tries lossless PNG
optimization, then the highest tested JPEG quality under the 256 KiB public image budget. Compression
does not change coordinates. Corrupt or mismatched images abort the helper instead of leaving usable
references to unseen pixels. Images from post-action observations reach the same content envelope.

Native logical coordinates use primary-display bottom-left origin, Y up, including negative coordinates.
Device pixels/DPI do not belong in saved character scale. AppKit owns nonactivating panels, hit masks, drag,
focus, Space/display changes and geometry; Three.js owns model/mixer/local poses and GPU resource disposal.
Scaling updates geometry, projection, anchors and hit coverage coherently. Transparent clicks need real OS tests.

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

Native screen input owns immutable image/thumbnail bytes and its independent durable receipt; renderer receives
opaque handles. Capture preferences freeze at acquisition; redaction affects both images. Submitted documents
cannot change scope. Ask Bot rechecks negotiated model image support immediately before dispatch. Unknown delivery
never resends; unreadable receipts remain uncertain. Attachment maintenance uses native Trash, protects originals,
selected/active files and symlinks, and never constitutes image resubmission.

System permissions are independent fresh OS facts. Explicit requests may prompt; background reads/capture never
request consent. Ordinary enable does not reset TCC; the separate repair action requires a checkbox and targets
one category/current bundle. No Full Disk Access request or direct TCC database edits.
