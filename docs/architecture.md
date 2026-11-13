# Architecture

[Product](product.md) defines the user-facing boundaries. [Development](development.md)
records commands and verification limits; [release](release.md) owns distribution gates.

## Assembly and ownership

```text
native desktop / headless → internal/app → backend.Service → adapter → native Runtime
                            ├── Bot identity, Notebook, tasks, schedules
                            ├── channels, peer connections, plugins
renderer / browser → typed product operations and snapshots
```

| Owner | Responsibility |
| --- | --- |
| `internal/desktop` | Native windows, menu bar/tray, focus, geometry, permissions, capture, input and notifications |
| `internal/app` | One application, configuration, admission, start/quit and service assembly |
| `internal/backend/api` | Product DTOs and explicit capability ports; private host tool connections |
| `internal/backend/{codex,caelis}` | Native protocol projection, execution bindings, policy, receipts and recovery |
| `internal/bot`, `tasks`, `eventplan`, `scheduledata` | Identity, introduction, delegation, follow-ups, occurrence and wake budgets |
| `internal/notebook`, `botskills` | Editable Markdown and application-scoped skill installation |
| `internal/machines`, `taskterminal` | Peer routing and owned external task-observation lifecycles |
| `internal/textchannel`, `telegram`, `weixin` | Shared interaction controls and independent authenticated channel ledgers |
| `internal/plugins` | Reviewed portable packages, credentials and generation-fenced Runtime selection |
| `internal/websurface`, `managedupdate` | Browser boundary and managed headless update boundary |
| `frontend/src` | Presentation, temporary interaction, character poses and graphics resources |

`internal/desktop/assembly.go` assembles `app.Application` without Wails/cgo; `app.Host`
injects OS effects. Desktop code does not import concrete adapters. Native services own
processes, bytes, files and OS decisions; renderers receive opaque handles and typed facts.
Supported, available, enabled and OS permission remain distinct. Unsupported operations
fail before changing preferences. `GOWORK=off` excludes sibling private source trees.

macOS owns AppKit/Wails surfaces and shared service attachment. Windows uses WebView2,
current-user Named Pipe/SID checks, private ACL credential files and owned Job Objects.
Its FileStore is not Windows Credential Manager. Cross-builds and CI do not establish
native GUI, permissions, minimum-OS or distribution acceptance. Linux is headless only.

## Runtime projection and authority

The [API contract](../internal/backend/api/contract.go) and adapters retain exact native
Session/Thread/Turn/run/item/request targets, missing/null/explicit-empty distinctions,
approval scope and option IDs. Native identifiers stay internal; product handles route
to the original owner. Optional capabilities require typed discovery/native negotiation.
A message, card, accepted dispatch or animation never establishes execution success.

Persist mutation intent, request identity and digest before dispatch. Accepted, rejected
and unknown are distinct outcomes. Unknown effects reconcile their original receipt;
they cannot be replayed with a new ID, moved to another Runtime or retried as new work.
Late replies are fenced by connection/binding generation. Terminal native events outrank
late responses; replayed/history items deduplicate facts without redispatching effects.
Callback execution binds its original catalog, configuration, tools version and invocation;
missing historical handlers fail closed rather than rerouting a same-name tool.

Codex uses the user's installation and standard `initialize` → `initialized` handshake,
then the consumed native methods and field semantics. CLI versions are not an allowlist;
the embedded [schema manifest](../internal/backend/codex/schema/manifest.json) is the
reproducible tested baseline. The handshake does not negotiate an invented numeric
protocol range or establish every experimental capability. Unknown server requests are
rejected; compatibility never relaxes approval policy or discards original bindings.

Caelis uses public service discovery/control and capability negotiation, not private
imports. The app's pinned bundled CLI takes precedence over PATH unless explicitly
overridden; Bot does not overwrite a global CLI or create a second Store, credential or
model authority. Required native Guardian/Agent Team capabilities gate execution.
Bot and TUI share the Host principal; Bot configures only its explicit resident profile.
Native Team settings select reviewer models; existing work keeps its execution snapshot.

Codex defaults to `on-request`, `auto_review` and `workspace-write`; Caelis uses native
Guardian auto-review and its workspace boundary. Only explicitly listed built-in Bot
tools receive direct approval policy in [botpolicy](../internal/botpolicy/policy.go).
Worker creation/continuation and persistent schedule mutations remain reviewed.
Native deferred tool discovery, external MCP services and ordinary sessions retain their
policies. Tool annotations, prose and character state are never permission grants.
Review failure, rejection or timeout cannot turn into a manual Allow bypass.

Only safely typed, inspectable independent Worker requests can be coordinated by Bot;
marked-secret, URL and opaque requests retain the user path. Uncertain private reports
cannot hide it. Resident approvals remain under native review/user decision; historical
decided reviews are display facts and cannot become actionable cards.

## Independent lifecycle and recovery

Windows, pet visibility, chat, settings, task observation, Worker execution and Runtime
attachment have independent lifetimes. Closing or hiding a surface cannot stop work.
Codex reaps only an owned process and exact owned work; PID fallback also checks birth
identity, never a global process-name kill. Bot detaches from the shared Caelis Host
without stopping it or cancelling shared native Workers.

Runtime/model selection first enters a durable last-choice journal. A cancellable
coordinator applies it; settings distinguish saved from applied and remain responsive.
Activation commits after a successful handshake, otherwise restores the old adapter and
draft. Existing tasks, execution snapshots and approvals keep their original adapter;
Worker-default changes affect new work. Preference changes do not cancel outgoing work.
Only first activation transfers unsent attachments to an empty target.

Recovery reconnects the original owner first. Automatic Caelis recovery starts a service
only after public status confirms `stopped` for the saved Store/principal. Bundled CLI
preparation and explicit activation also use public `service start`; Core owns compatible
Host reuse, busy/idle replacement and non-downgrade. Bot never kills a shared Host.
Codex shared-daemon startup requires retained native process identity and confirmed
absence; an unreachable socket alone is insufficient.
Explicit lost-owner recovery fences the old owner and retains unknown receipts. Readable
archived history does not authorize resuming a lost execution or settling its effects.

Observers and reads recover with bounded attempts/deadlines; one failed Worker/channel
cannot gate resident connection or controls. Wire projection drains oversized values;
bounded queues and observer restart preserve receipts. Reconnection does not prove that
work survived. Activity evidence is separate from receipts: idle can release capacity
without resolving unknown input. Completed/not-loaded is not a permanent Worker ban.

Draft restoration never sends. Exact receipts reconcile optimistic messages; rejection/
uncertainty retains content without overwriting newer edits. Screen submissions have
independent receipts. Stop records its exact target before dispatch and cannot affect a
newer execution. Cancellation after dispatch may be unknown, never presumed unexecuted.

Failures degrade only their owning capability. Raw errors, protocol states, retries and
credentials never enter chat, bubbles, OS alerts or settings warnings. Diagnostics are
sanitized, deduplicated and bounded. Exhausted unsafe recovery exposes one calm recovery
surface with privacy-safe feedback and restart, preserving work without restart loops.

## Durable data, IM and skills

Bot identity, Notebook, appearance and product tasks survive Runtime choices. Native
bindings, credentials and execution receipts retain provider scope; switching never
migrates unknown work. Private writes use restricted files and atomic replacement;
corrupt identity/receipt evidence is preserved and cannot authorize a replacement owner.
Diagnostics export allowlisted states/counts/versions, not prose, paths, arguments or IDs.

[Notebook](../internal/notebook/vault.go) is the resident writable workspace, not HOME.
`MEMORY.md` and dated Markdown notes own long-term memory; generated `INDEX.md` indexes
paths/titles and ignores hidden directories/symlinks. Native reads or `rg` search bodies.
Only the index is rebuilt; edits/deletions persist, with no automatic Git/cloud sync.
Legacy private Memory data remains stored but `bot_memory` is not executable/discoverable
and is not automatically migrated or claimed as Notebook search coverage. Workers have
independent directories/instructions; these scopes do not isolate processes of the same
OS user or full-access execution.

Private SQLite IM stores visible user/assistant messages, control receipts and completed
Artifact-bearing tool labels with stable request/item IDs. Ingress precedes submission;
original receipts and streaming patch the same item. Chat pages local IM even offline;
Runtime owns execution/tool history. Reconnect reads a recent Turn, not full history.
Optional display sync/Worker reads cannot block admission. Damaged display databases are
preserved before rebuilding; disk failure retains live cache. Artifacts are host-resolved
handles, never model-written path authority; preview/open/save revalidate the file.

[Packaged English skills](../internal/botskills/skills.go) install owned bundles under
private `app-skills`, preserving Notebook/plugin/custom skills. Discovery exposes only
name, description and location; native file tools load bodies/references on demand.
Codex uses resident developer metadata; Caelis uses selected `skill_roots` without a
second profile guide. Neither installs globally or copies the private catalog/Notebook
into ordinary sessions/Workers. Running Workers retain start context. Behavior changes
require checking Bot guidance; architecture-only edits do not.

Introduction is an ordinary user message with durable identity, not host-written memory.
`bot_dream` performs a gated context handoff after its owning native turn terminates.
Private durable handoff data retains goal, constraints, original tasks/uncertain receipts
and next action; the first accepted input receives it with current `MEMORY.md` as saved
context. Consumption follows exact acceptance once. Human ingress/Stop/decisions preempt
adoption; failed or superseded renewal preserves the old binding and original creation
receipt. Handoff prose supplies context, never permission or business-state authority.
Idle, recovery, upgrades and age/turn count alone do not dispatch maintenance turns.

## Workers, peers and files

[WorkRuntime](../internal/backend/api/work_service.go) is shared by local adapters and
Bot peers. `tasks.Manager` owns product ordering/visibility, routing and finite reports;
native Runtime owns execution, permissions, messages and task state. Submission IDs derive
from the original tool invocation, so identical prose is not deduplication identity.
Creation-only title/workspace/environment cannot rebind a retained task.

Machine, Runtime, owner and operation persist before execution; unknown results never
select a fallback environment. Observations/history use canonical message IDs; unreadable
history retains cached content, never empty success. Completion, explicit task finish
and question closure remain distinct and execution-fenced. Host reports use durable
`SubmitSystem`/`SystemReceipt`; native provenance hides them from IM/channels. Prose
cannot classify human input as a host notice.

Local cards observe native work through an external terminal owned by launch completion
and PID/birth identity. One-use attach receipts prove delivery; unknown launches remain
fenced. Quit waits for exit and honors cancellation, without shell injection or prompt
replay. Peer windows/browser task panes are read-only; closing stops only observation.
Questions/approvals return to Bot conversation.

[machines](../internal/machines/service.go) keeps A's product task and B's isolated work
owner. Foreign work never enters B's resident chat, task list, notifications or Agent.
B alone owns its accounts/models/Worker configuration. No recursive delegation, Bot
migration, remote installation/helper or generic file synchronization is provided.
A persists a target-bound pairing intent/proof and pins B's identity before requesting
access. B's authenticated user approves the exact request; grants/decisions commit
atomically. A returns on admission and reads the original request on explicit check/page
entry. Pending peers have no work routes. Each direction requires independent authority;
browser cookies, loopback, network reachability and SSH do not authorize peer execution.

Peer traffic rejects browser Origins and credential-following redirects. Plain HTTP
requires protected external transport. SSH uses system/private OpenSSH configuration
only as a one-way network channel; passwords/passphrases use transient askpass.
Established SSH recovery has capped backoff; explicit stop and authentication/host-key/
configuration failures fence automatic starts. Every recovered channel revalidates
original peer/pairing/Runtime identity without replaying pairing or task writes.
Network operations do not hold shared route locks or block local chat/controls.
Deleting a connection durably removes local profile/proof and resumes owned credential
cleanup under the original receipt without contacting B. Historical task routes survive;
a new same-name machine cannot inherit them. Unknown metadata writes require readback.

Remote paths are not A-local paths. Inputs resolve retained selected uploads, resident
Artifacts or owned Worker outputs; B stages authenticated task-scoped bytes. Outputs
require original task scope, metadata/digest checks and a private copy before opening,
readable offline. Adapter limits apply; cache cleanup protects active/unknown operations.

[Original URL challenges](../internal/backend/api/work_url.go) revalidate the original
pending native request before opening. Reachable HTTP(S) opens A's normal browser;
opening, webpage effects and native consent are separate facts. Codex's bounded portal
supports B literal-loopback HTTP static forms with a supported POST on the exact original
URL. The owner derives target/hidden values; A supplies only scoped field values and
revision. It is not a general browser proxy. B-only hostname, multistep/GET/redirect/JS,
different-action, loopback callback, certificate/profile-state cases are unsupported.
No credential/browser-state migration or automatic tunnel claims completion for them.
Original URL receipts remain readable through the frozen binding without replacing work.

## Scheduling and message channels

[eventplan](../internal/eventplan/engine.go) owns durable events/arrangements/occurrences;
`scheduledata` supplies pure CEL and native metadata, without a second dispatcher.
Timed, conditional, native-observation and task-completion arrangements share scheduling
and original receipts. No care engine or idle LLM poll exists. Collection runs bounded
native scripts or selected plugin tools; declared output schema and pure conditions
precede a natural-language wake carrying saved purpose and untrusted collected facts.
Script registration reviews pinned interpreter/source and ongoing observation; it grants
no Runtime sandbox bypass. Windows script registration is unavailable without equivalent
process lifecycle guarantees. Unknown collector effects suspend the arrangement, never
rerun automatically. Wakes persist identity before dispatch and reconcile original input.

Busy admission, original grants, cancellation, deadlines, deduplication and shared wake
budgets restrict dispatch; desktop presence/lock is not a global gate. Explicit collectors
may use it, while desktop input retains its own lock/permission checks. Sleep/exit pauses
execution. Terminal arrangements leave active lists, unreferenced events archive, and
bounded retention never prunes unresolved receipts or rebinds retained registration IDs.

[textchannel](../internal/textchannel/control.go) provides shared `/approve` and `/answer`
controls with durable short references and immutable native option catalogs. Transport
buttons/replies bind only confirmed original card deliveries, never quoted prose or the
latest request. Decision claims persist before dispatch. Short-reference expiry supplies
no native decision; async question dismissal/expiry supplies no answer or authorization.
Marked-secret answers bypass ordinary Bot context/IM/mirroring; pending plaintext lives
only in memory and must be re-entered after restart. Provider transport still sees sent
plaintext; the host cannot infer secret markers from prose or promise Runtime secrecy.

Telegram/Weixin share one conversation and independent durable inbox/outbox ledgers.
Authenticated ingress commits before cursor advance; accepted/unknown sends never replay.
Callback/control/ordinary input and output loops remain independent. Telegram pairs one
private chat with desktop confirmation and coalesces edits under rate limits. Weixin is
a paired-owner text POC with an owner-input window, bounded allowance and expiry;
Desktop/TG input does not refresh it. Its shared-IM spool prioritizes decisions/questions
and results over progress. Explicit excerpts cannot acknowledge full bodies; `/r` resumes
only eligible unattempted content from bounded current IM. Native options stay complete.
Channel failures/budgets cannot alter other output or execution authority.

## Plugins, desktop context and presentation

[Plugin packages](../internal/plugins) are reviewed immutable portable bytes selected
only for Bot. Offline marketplace metadata requires a separately approved hash inventory;
an index or install-by-default claim grants no trust. No install hooks, arbitrary Git/URL
fetches or global Codex marketplace registration run during install. MCP discovery does
not call tools; service claims do not extend built-in approval exemptions. Generation-
fenced relays inject write-only private credentials only at dispatch, cap responses and
block redirects; unknown tool effects are not retried. Hosted service implementation is
publisher-owned and cannot be byte-pinned by a local package hash.

Caelis owns its complete Desktop World MCP/Skill source and native App × Turn approval,
execution and recovery; Bot supplies no proxy, installer, switch or alternative authority.
Codex receives no DTW from Bot. OS Accessibility/Screen Recording consent remains
independent. Shared mouse/focus input is not an independent background input device.

OS adapters own nonactivating surfaces, hit masks, drag, Space/display changes, logical
geometry, capture and permissions. Native coordinates use primary-display bottom-left,
Y up; saved scale is independent of pixels/DPI. Three.js owns model/mixer/local poses
and GPU disposal; chat portraits use finished sprite atlases. Character contracts and
provenance live in [content packs](content-packs.md), independently of conversation/task
identity. Context records source/freshness/unknown fields; foreground is not task binding.
Local idle/attention and independent bounded pass-through props use no idle model.
User input and pending decisions preempt decoration; hide/Space/disposal reclaims props.

Chat/settings retain independent windows and drafts; closing a native sheet/window grants
no action. Capture owns immutable bytes, opaque handles and independent receipts; only
confirmed annotated selections send, with negotiated image support checked at dispatch.
Permission reads never prompt; ordinary enable never resets TCC. Explicit permission
repair targets one category/current bundle, without Full Disk Access or database edits.
Streaming/reduced-motion/visibility policies affect presentation only. Final text stays
exact; controls and approval/Stop facts never wait for animation. Safe Markdown disables
raw HTML/remote image loads. OS alerts use real reminder content or current decisions,
not raw errors, ordinary replies or completion-only facts; clicks only open the app.

## Headless network and managed update boundaries

`serve` assembles the same application without native windows; disconnecting a browser
cannot cancel work. [websurface](../internal/websurface/routes.go) exposes a fixed product
operation allowlist, bounded snapshots and scoped file transfer, never arbitrary RPC/shell.
Direct local trust requires a loopback listener, actual loopback peer and exact allowed
Host/Origin. Writes retain CSRF/custom-header checks; forwarded headers grant no trust.
Nonlocal clients pair using a private short-lived one-use code, never a URL/log value.
Remote grants expire after inactivity; expired grants cannot revive. Peer authorization
is separate. A proxy/SSH forward that hides remote source must use `--require-pairing`;
the optional host-loopback relay uses a private proof and has no execution authority.
No NAT address or origin string proves a forwarded client is local.

[Managed updates](../internal/managedupdate/contract.go) use one stable same-user launcher,
private canonical installation and external profile. Code rollback never rolls back user
state. Inherited process locks prevent another launcher taking a live child's ownership.
Containers are image-managed; system/read-only installs cannot self-install. Production
trust is a pinned Ed25519 key and fixed release feed. Exact signed manifest bytes bind
platform/source/version, executable pins and every archive member; traversal, links,
unexpected members, hashes/modes/size or architecture mismatch fail before old work closes.
QA trust is explicit and separate from production; missing metadata is not a valid offer.

Browser commands accept server-issued offers and original operation IDs, never arbitrary
URLs, executable paths, shell strings or keys. Automatic checks/downloads do not authorize
restart. Installation waits for safe idle, pending decisions and unknown receipts, joins
channel loops and checkpoints them without replay. Health requires authenticated readback
of the new running identity; download completion or accepted receipt is insufficient.
Rollback changes code only. Bundled Caelis updates use public native service selection,
never kill/downgrade a shared Host; deferred Host activation remains visibly pending.
macOS release trust additionally requires Developer ID, hardened runtime, timestamp,
notarization, stapling and Gatekeeper acceptance; signed-only is not notarized.
