# Architecture and internal backend contract

Status: application-owned Bot foundation implemented locally; Codex execution connected.
Caelis generic application protocol is connected against official v0.61.0 (5e2546f); legacy Bot Mode stays disabled.
Installed release identity, isolated Host/native tools, real models and scoped macOS GUI acceptance pass;
see [release acceptance](caelis-release-acceptance.md) for remaining product and distribution limits.

2026-09-23: [Bot product-host architecture](bot-platform-architecture.md) and the
[generic Runtime extension proposal](runtime-extension-contract.md) define the target.
`internal/bot` owns identity, tool behavior and resident reminders; `internal/tasks`
owns task admission, workspace allocation, the product ledger and completion reports.
Notebook and embedded Memory v0.6.1 are application-owned and shared across providers;
see [personal data boundaries](personal-memory.md). The [Caelis implementation handoff](caelis-core-rebuild-handoff.md)
authorizes removal of legacy Bot Mode without compatibility; existing user data is preserved.

The agreed continuity model is a long-running Session with compaction, recall/remember,
and a plain Markdown Notebook: a host-generated INDEX.md, one editable MEMORY.md and
YYYY/MM/DD/ daily notes. Users edit files; the Bot uses Runtime file tools. The one-time initialization UI requires a name and accepts an optional description; it submits a visible ordinary user message; the Bot updates MEMORY.md.
Keep no second identity-settings authority and do not elevate this text into system policy.
Do not add dedicated notebook CRUD/UI, SOUL/USER files, a profile authority, bootstrap
snapshots or a background consolidation agent. Runtime access is scoped to the Notebook,
not the complete application data directory. The application-only notebook skill is bundled and exposed only to the resident Bot; workers keep their own workspace and instructions. Old private-format notes/profile are copied once without deleting originals. See [personal data boundaries](personal-memory.md).

The [capability contract](backend-contract.md) defines provider assembly independently
of OS hosting. Native adapters own execution bindings, approvals and uncertain receipts.
The [Caelis integration guide](caelis-integration.md) records the current public contract and
[scoped acceptance evidence](caelis-application-acceptance.md). Windows remains unimplemented;
its future host boundaries are recorded in the [backend/platform plan](backend-platform-plan.md).

The application implements host-only `api.TaskProvider` on top of `api.WorkRuntime`.
Codex maps native execution to App Server thread/turn methods, retaining exact targets
and original request provenance. It does not inherit Codex App IPC or adopt arbitrary
desktop conversations. See [task delegation](task-delegation.md).

```text
Caelis Bot: menu bar + scalable desktop pet + contextual panels
  -> internal backend contract
     -> Codex adapter -> native Codex App Server (first)
     -> Caelis adapter -> generic application API (public HTTP/SSE; no legacy fallback)
  -> character behavior -> Three.js -> GLB

Wails / Go: windows, OS integration, process lifetime and byte transport
```

## Native surfaces and ownership

The target is a menu-bar application with several small surfaces, not one permanent
foreground window. P1 has replaced the single-window fixture:

- Pet context menus expose only chat and hide. The menu-bar menu additionally exposes settings, manual update checking and explicit quit; visibility is refreshed when the menu opens.
- The bundled application icon and local chat avatar derive from the same user-provided illustration. Message avatars are local resources, not backend-provided remote URLs.
- One optional settings window groups pet size/notifications, runtime, storage, diagnostics and updates.
- A transparent, borderless pet surface renders the GLB. Host geometry tracks
  screen coordinates, proportional scale, hit regions and persisted placement.
- A contextual input surface takes focus only when requested, then closes after native
  send acceptance. Rejected/unknown submissions retain the draft.
- Composer attachment/reference menus are portalled, editor-width overlays. History
  prefers the space above its composer; the native quick-input host chooses the
  available side of the visible display, growing only its transparent envelope.
  The input rectangle and its glass backdrop stay anchored independently. Native
  activation IDs fence late menu requests; the quick menu sits above the pet while
  open and restores its normal window level on dismissal.
- A non-key message bubble shows only the current request's latest assistant response/lifecycle state.
  It never auto-opens the keyboard panel. Approval clicks expand this same bubble;
  an optional IM-style chat window shows user/assistant messages and necessary decisions.
- Approval/details surfaces show adequate decision context and preserve the exact
  backend request target. Larger content may expand beyond the pet's bounds.

The host owns their lifecycle and display placement; hiding a surface does not own
backend lifetime. A renderer reload cannot become a task cancellation or approval
decision. The application must remain accessible through the menu bar when every
surface is hidden. P1 must verify accessory activation/Dock behavior on the actual
pinned Wails version; do not assume latest online examples match the installed API.

Keep native bridge code narrow. Wails remains the default host; if its tested API
cannot express a required macOS interaction, isolate the smallest AppKit bridge
instead of replacing the stack before a concrete experiment.

The [platform baseline](platform-baseline.md) defines shared-core/native-driver
ownership and qualification gates. Only macOS has a native host today. Other builds
return an explicit unsupported error before creating surfaces; cross-compilation
does not qualify desktop behavior. OS imports stay in build-tagged runtime/driver
files, with pure-Go lifecycle and placement tests compiled for macOS/Windows architectures. Windows implementation starts only
after the complete macOS release; Linux is outside the current plan.

## Owners

The backend owns authoritative execution, tools, policy and durable conversation
truth. Adapters project typed backend facts and route commands to explicit targets.
The UI presents temporary interaction; the host shares a revision-fenced draft across surfaces. A character asset never
owns a backend identity, conversation, run, approval or memory binding.

Session/thread identifiers may exist in the adapter and persisted binding. They
must not become default user navigation, configuration prompts or status labels.
The native host does not recreate Codex or Caelis execution semantics.

## Contract shape

`internal/backend/api/contract.go` owns the host/renderer DTOs and Engine interface.
`cmd/contract-gen` generates `frontend/src/backend/contract.ts`; checks reject drift.
The Wails backend service resolves selected file handles and delegates to the selected
adapter. `internal/app` owns product assembly/lifetime, `botpolicy` the fixed roles, and
`localipc` the private tool transport. Desktop Service owns surfaces only. Snapshots carry revisions,
available actions, item results and exact opaque approval handles, not native IDs.

The first real vertical slice must preserve:

- initialization, authentication status and precise capability negotiation;
- internal binding to a backend instance and canonical conversation;
- submit, steer when supported, interrupt and explicit terminal outcomes;
- message/item identity, attachments, tool activity, results and artifacts;
- native approval request target, offered choices, response and resolution;
- pending/accepted/unknown outcomes across disconnects, and state rebuilding.

Memory, plugins, connectors and voice can grow through versioned capabilities.
Native worker ownership/approvals and resident scheduled activation now have a first implementation. They are not excluded by today's draft.
Neither arbitrary backend payloads nor generated prose may be promoted into
generic approval or execution authority.

This is an internal, versioned adapter contract first, not a new public protocol
or an exhaustive copy of Codex. Separate host-to-renderer calls from backend wire
messages. Preserve backend-specific semantics behind typed extensions where a
lossless generic operation is not available. Publish a mapping and conformance
case for each implemented capability; negotiate availability for the chosen backend.
The UI's minimalism must not shrink the backend contract into only pet animations
and chat text. Character states are projections of facts, never execution truth.

## Codex first

Codex **0.153.4**, pinned in `toolchain.json`, is the schema and regression baseline,
not a user-runtime requirement. Use native stdio JSONL without a JSON-RPC version
header. `make schema` generates stable and
experimental schemas; 58 consumed files are vendored byte-for-byte with hashes.
Desktop sessions enable experimental API for background-terminal cleanup and
native decision metadata. Unknown server requests still receive -32601.

Codex is discovered locally, never bundled or silently installed. The host first
looks for the standard `$CODEX_HOME/app-server-control/app-server-control.sock`,
or an explicit `CAELIS_CODEX_SOCKET` path. An existing socket uses HTTP Upgrade and
WebSocket text frames, then the same initialize/initialized/account/read handshake.
It requires no CLI and receives no process owner/kill callback. Closing the Bot
disconnects only this client after scoped Bot-work cleanup. Socket connection and
initialization get a bounded three-second attempt; an unavailable/incompatible
endpoint falls back to local CLI discovery. This is connection establishment only,
not a retry of a submitted turn or approval.

When an endpoint cannot handshake, the host searches PATH and standard user/Homebrew
installation locations. For the official npm layout
it uses that same installation's native entrypoint, avoiding Finder's missing Node
PATH. An explicit CODEX_BIN override is still available to developers. Discovery is
followed by the same standard handshake as a shared endpoint, with no CLI version
command, release allowlist, or upper/lower release bound. App Server currently has
no negotiated protocolVersion or comprehensive server-capability manifest. Validate
the consumed initialization response (userAgent), tolerate additive fields and absent
unused home/platform metadata, then validate account and subsequent typed interfaces
as used. Initialization alone does not establish support for every experimental API.
Missing methods, incompatible parameters and invalid required wire data are reported
without changing policy or replaying a mutation. See [compatibility](codex-compatibility.md).
The tested baseline remains 0.153.4. No private app IPC is used.

The runtime section of the shared settings window selects Codex and defaults to automatic CLI
discovery. A user may choose an absolute CLI path. Manual validation always probes
that exact installation (not the socket fallback), initializes and reads account
status without creating a thread/model call. Only then
is runtime.json atomically written with mode 0600 and reused at startup. Invalid
paths or failed validation retain the old settings. Runtime switching waits until
Bot work/decisions/unknown dispatch are resolved, and resumes the same binding.
The configured path is the CLI fallback; a usable standard socket still takes priority.

An App installation/running process alone does not establish an endpoint. This
machine's running Codex/ChatGPT App backend (0.155.0-alpha.9.2) uses stdio and has no
standard control socket/TCP listener. Bot cannot attach to that private stdio stream;
it does not launch the App, change its arguments or enable a daemon. Shared endpoint
support is not advertised as universal running-App integration.

Native automatic-review started/completed events project a separate Review fact,
including status, action and rationale for root/owned child work. They cannot create
an approval choice. Only actual server requests can do so. Authentication recovery
uses native unauthorized/HTTP 401 codes; successful browser login resumes the existing
binding without resubmitting a prompt. Unknown dispatch still requires reconciliation.

Native notifications are dispatched by method before decoding their method-specific
payload. Thread items are decoded as a tagged union on both live and replay paths;
unconsumed variants cannot collide with fields of another variant. Optional component
failures and native retries are diagnostic-only. A lost owned lifecycle or decision
fact still requires reconciliation; unknown native requests remain rejected. Caelis
keeps its canonical feed/cursor checks: unknown delivery semantics are not treated as
optional metadata. Both adapters write private, rotating structured diagnostics through
`internal/diagnosticlog`; the retention and exported-environment contract is documented
in [task delegation](task-delegation.md).

Native notifications consume backend revision changes and persisted reminder
occurrences, independently of renderer visibility. Permission is requested only from
an explicit user action. Restored completed history does not generate new alerts;
pending decisions and new completed work notify when the pet is hidden, and due
reminders notify even while unrelated work is busy. A notification click opens chat.
The macOS bridge uses UserNotifications; no daemon or background model loop is added.

Implemented mapping: thread start/read/resume; turn start/steer/interrupt; native
item/delta/completion; command/file/permission approval, tool user input and simple
MCP form/URL elicitation. Approval responses preserve offered native payloads,
request IDs and a transport-local generation, including reused native IDs.
Available skill references come from skills/list, including plugin-provided skills.
Plugin installation, arbitrary complex forms and external-token refresh are not
advertised. Native managed ChatGPT browser login/cancel is present; fresh-account
live verification remains separate from existing-account model checks.

The read-only `make smoke-adapter` performs initialize/account/read/close without
model calls. `make smoke-workflow` explicitly invokes real models/tools with
synthetic data in isolated directories. Neither attaches to Codex Desktop tasks.

## Lifecycle and recovery

Panel close, renderer disposal and hiding the pet have no execution authority.
Explicit quit and SIGTERM cancel outstanding RPC observation, interrupt the exact
active run, wait for a native terminal fact (bounded), request native background
terminal cleanup for the owned root/observed child threads, then close/reap the
owned App Server. Native cleanup failure is reported, not represented as success.
Real testing exposed a native registry gap: interruption can orphan a foreground
command before backgroundTerminals lists it. The macOS process layer records actual
OS descendants of its owned server before interrupt/pipe EOF, retaining PID plus
birth time. It terminates only those matching identities, including captured setsid
children; unrelated processes and reused PIDs are excluded. Explicit stop also
recycles its owned server and resumes the same history binding, without replay.
Native list/terminate/clean remains the first cleanup path. Descendants escaping
before capture, crashes and remote work are not guaranteed cleaned. No process-name
or global PID kill is used. Crash/network/sleep recovery remains a separate gate.
Developer Run only signals this workspace's executable and waits for graceful
shutdown; it refuses a second owner after timeout, rather than using SIGKILL.

The private `conversation.json` binding stores native thread identity and an
outstanding client submission identity, atomically with mode 0600. Persist pending
identity before dispatch. Native userMessage.clientId or a successful RPC proves
acceptance; unknown writes never replay. Reconnect observes a live owner in place
via thread/read, or resumes history in a replacement process after disconnect.
History reconstruction is not continuing interrupted work. Early terminal events
win over late start receipts; replay replaces existing items. Native history owns
conversation bytes. Host-held drafts survive renderer reloads, surface switches and
normal application restarts. `draft.json` stores text/references and `draft-files.json`
stores local attachment selections via atomic mode-0600 writes. Restore never sends;
only an accepted submission clears its matching draft. Missing source files remain
visible as unavailable so the user can remove or reselect them. These files are not
a durable outbox: a crash between native acceptance and local clearing can retain
already-accepted text, and does not authorize automatic resubmission.

Recent-history recovery probes the optional thread/turns/list interface by protocol,
then resumes with excludeTurns and reads 20 full-item turns in descending order.
Projection restores chronological order. An unsupported optional method/parameter
falls back to legacy resume without rejecting the CLI release. Read failures remain
errors; malformed pages are not accepted as empty history. Explicit older-page loads
prepend deduplicated user/assistant messages and artifact handles only: they cannot
replay approvals, worker creation, lifecycle or overwrite newer live content. The
chat asks for changed snapshots; unchanged polls do not serialize message history.
Pet polling uses recent content. Already-loaded history is retained, not virtualized.
Legacy resume and pending-write reconciliation may still hydrate full native history;
the 8 MiB wire limit remains, including a very large single turn.

The private `Work` directory holds task results and retained `.attachments` copies.
Selection is local metadata until send; send revalidates up to 8 regular files,
20 MiB each. Supported image headers map to localImage; other files become explicit
local path inputs for native tools. This is not a generic remote file upload API.
Result files are revealed in Finder; model prose cannot create execution authority.
The separate attachment-storage surface shows local copy size and offers explicit
Trash relocation for copies older than 30 days. Only flat host input directories
with regular files qualify; symlinks/unknown directories, originals, selected drafts
and result files are not deleted. Active work, pending submissions, decisions or
running native background terminals block cleaning. The native host uses macOS Trash,
not permanent removal. Future work may need the user to reattach cleared copies.
Assistant Markdown renders in the optional chat, with explicit http(s) links and
native message/code copying. Raw HTML is skipped and remote images become links,
so rendering a reply alone does not fetch remote resources.

## Rendering

The native host owns screen coordinates, displays and window interaction. Three.js
owns the model, animation mixer and hit regions inside the viewport. A raycast hit
is not proof that the OS-level transparent area passes clicks through correctly.
Idle animation is local; it does not continually call the language model.

Scale changes must update window geometry, model projection, attachment-panel
anchors and OS hit regions coherently. Keep logical screen points separate from
Retina render pixels. Persist the pet's visible bounds and recover placement when
its original display disappears. Pause rendering when hidden and measure idle cost.

## Code boundaries

| Boundary | Location | Responsibility |
| --- | --- | --- |
| Native host | `internal/desktop/` | Tray, surfaces, focus, placement, settings and lifecycle |
| Backend service | `internal/backend/` | Internal commands/events and capability contracts |
| Codex adapter | `internal/backend/codex/` | Owned process, transport, native mapping and recovery |
| Product panels | `frontend/src/` | Pet/input/approval presentation and temporary drafts |
| Character runtime | `frontend/src/character/` | GLB, animation, local interaction, disposal |
| Frontend boundary | `frontend/src/backend/` | Typed host projection; no subprocess or credential access |

The transport serializes JSONL writes, correlates native requests and preserves
structured errors internally. Caller contexts bound observation; cancellation
after attempted dispatch means unknown outcome. Partial writes, malformed wire
messages and overflow (64 queued messages, 8 MiB per line) disconnect explicitly.
There is no durable event queue; native history rebuilds the projection. Startup
is bounded to 30 seconds. No private conversation or credential payload is logged.
Stderr is discarded. A separate diagnostic export writes a mode-0600 JSON document
through a native save dialog; it never uploads. The report is built from allowlisted
system/component versions, finite connection/lifecycle states, booleans and counts.
It excludes conversation/draft content, native identities, paths, tool arguments,
approval bodies and credentials; it is a current-state report, not a log archive.

## Official references

- https://learn.chatgpt.com/docs/app-server
- https://v3.wails.io/
- https://threejs.org/examples/webgl_animation_skinning_morph.html
- https://www.khronos.org/gltf/

Research provenance and decisions: [references.md](references.md).
Milestones and native/backend acceptance: [implementation-plan.md](implementation-plan.md).

## Planned embodied behavior boundaries (2026-09-20)

This section defines the target boundaries, not a public protocol. See the
[product roadmap](roadmap.md). The first P4.2 slice now implements a native context
snapshot, a local BehaviorDirector, layered gaze/poses and one independent paper-plane
surface. Locomotion, explicit task/window associations and a general Agent action /
receipt protocol remain planned. The original five clips and rig are reused.
An independent local facial layer now blends eleven standard morph targets after the
clip/pose update. A typed expression profile and local performance timeline coordinate
face and near-body gestures. A two-link arm solver reuses the original rig, damps wrist
targets and restores its base before each frame; source clips retain ownership during
their one-shots. Editable Blender key-pose references are derived from this controller,
not extra baked runtime clips. These layers consume activity and interaction facts without generating Agent
work or approval decisions. The latest foundation asset refines local face, hair, hand/cuff
geometry and skin weights while preserving the original five clips and 19-bone rig.
The morph export preserves its new foundation base bytes; the original v0.4.1 remains archived.
Hair is bounded opaque geometry and vertex colour, with no extra materials, textures or physics;
missing morph support degrades to the original character. Hidden/reduced-motion states
reset expressions and park frame requests; the arms settle into a relaxed stance.
Rendering and hit masks use the same morphs and arm pose. Pupil gaze and velocity-driven
hair follow-through remain planned in M2.
The [desktop behavior baseline](desktop-behavior.md) now puts a small Desktop/Dock/
ActiveWindow snapshot, idle/interaction profiles and detached props in P4.2. Complex
desktop semantics and task/window grounding are later extensions, not prerequisites.

Keep two inputs distinct: authoritative backend facts (work, decisions, results)
and presentation intents (look, attend, approach, deliver). An Agent may intentionally
request an expression; it cannot manufacture an execution fact by requesting an animation.

```text
Backend adapters -> typed work/approval/result facts --+
Bot / user ------> high-level presentation intents ----+-> local Presence Director
Native desktop --> bounded context / input state -----+       |
                                                           action plan
                                                             |
                                +----------------------------+-------------------+
                                v                                                v
                      native placement executor                      character pose executor
                                +----------------------------+-------------------+
                                                             |
                                                       action receipts
```

| Boundary | Planned ownership | Must not own |
| --- | --- | --- |
| Desktop context | Native Desktop/Dock/ActiveWindow/Pointer snapshot, logical geometry, source/validity; optional explicit task associations later | Backend execution truth, default screen recording, Three.js scene graph |
| Presence Director | Local product policy; intention arbitration, cooldown, interruption, attention budget and fallback | LLM frame-by-frame control, scheduling new Agent work, approval decisions |
| Placement executor | Native host; bounded movement and live display/Space/input checks | Renderer-driven mouse chasing or moving other apps without a separate authorized capability |
| Pose executor | Three.js; clip blending, supported head/body orientation and local procedural layers | OS window ownership, durable Bot identity or native permissions |
| Prop lifecycle | Host-owned instance/surface lifetime and screen movement; renderer-owned within-surface motion, orientation and attachment transforms | Extra Agent/Thread identity, blanket desktop input interception or execution authority |
| Character profile | Versioned data; idle groups/weights/cooldown, interaction mapping, intents/clips, rig, props/anchors, facing/feet/bounds and fallback | Executable package scripts or assumptions that every body has eyes, fingers or legs |

Implementation stays within current host/backend/renderer boundaries. Establish a
small pure local policy first; do not create an engine or another Agent orchestration
protocol just to name these roles. Start with a bounded snapshot of basic desktop
entities independently of the render scene, without a generic ECS or semantic map. Native logical points and
display/Space identity are converted explicitly to local model coordinates.

Window context must record the source, observation time/revision and invalidation
conditions. OS handles can expire or be reused. Explicit user selection or an app
integration can establish a task/window association; a foreground window or a
headless shell call cannot. Environmental reactions may look toward/avoid the active
window without such an association; they must not imply work is happening there.
Unavailable geometry stays unknown; inferred Dock avoidance bounds stay estimated.
Geometry-only access, content access and computer control remain
separate capabilities; add only the permissions required by the selected scenario.

The initial internal action shape should carry a correlation handle, semantic intent,
optional valid target, deadline and interruption/fallback policy. Receipts distinguish
accepted, started, completed, interrupted, failed and unsupported; acceptance by the
MCP bridge is not proof that a visible action completed. A gesture can be suppressed
by hiding/reduced motion; that outcome must not be reported as visible completion.
Exact DTOs and tool names are designed when the first action path is implemented;
the current bot_gesture API does not already promise these receipts.

Manual dragging/input, hidden preference and reduced motion take priority over
autonomous movement. A new pending decision suppresses decorative actions; it still
uses the existing native decision surface and never gains authority through a pose.
Higher-priority actions interrupt or replace stale actions; deadlines prevent a long
backlog of old reminders/celebrations playing after restoration. Movement records
transient location separately from the user's preferred resting position.
Closing/losing a target cancels or returns to a safe location without focus theft.
Cancelling a body action cannot stop a backend turn; stopping work uses existing APIs.

Host placement, renderer projection, hit coverage and bubble anchors must agree during
movement/rotation. Preserve the single native pet panel and Window Server manual drag;
do not restore the former two-window synchronization failure. Independent props may
use a separate bounded nonactivating pass-through surface: this is not a second input
window following the pet during manual drag. Prototype prop surfaces/lifetime first;
character locomotion and task-window following come later. A full-screen overlay or different engine needs a
concrete limitation and measured benefit before replacing the existing host.

Develop finite head/body orientation using available bones before eye tracking/IK.
Unsupported poses fall back explicitly. Local layers must blend with the base clip
without overwriting each other; displacement belongs to the native placement executor,
with in-place locomotion selected only if the character supports it. Hidden/reduced
motion remains cheap; native observations are bounded and do not wake a permanent LLM.
Idle selection filters eligibility before weighted random choice, avoids immediate
repeats, and changes at safe phase boundaries. Working/hover/drag/input override active
idle. A behavior may coordinate a body clip with an independently owned prop; release
and reattach cues transform between character-local and desktop coordinates. At most
one detached plane initially, bounded lifetime, no duplicate attached mesh while flying;
hidden/Space changes/disposal reclaim it. Full receipts include suppression/interruption.
Use a second materially different body to validate these abstractions before publishing
a Character SDK; keep Codex first and Caelis later behind the current backend contract.

## P1 implementation (2026-09-19)

`internal/desktop.Service` serializes placement and lifecycle mutations. Preferences
are atomically replaced at `~/Library/Application Support/Caelis Bot/placement.json`
with mode 0600; geometry uses AppKit global logical points (bottom-left origin), not
Retina pixels. Display recovery clamps to the largest intersecting work area, or the
primary display when there is no overlap. A load/save error is reported, not silently
represented as durable success.

The host bridge was introduced on Wails beta.6; the dependency is now beta.23.
The bridge continues to supply its own non-key panel rather than adopting the new
upstream panel APIs as part of a dependency update. The original beta.6 host could not
create a nonactivating NSPanel. The narrow AppKit bridge reparents the pet WebKit
content into a non-key NSPanel while retaining Wails' hidden owner and bindings.
The same panel contains a native input view above the WebKit content. The window's
alpha-mask-controlled mouse ignoring handles transparent pass-through, while the
input view handles click/context menu and calls `performWindowDragWithEvent:`.
Window Server moves the visible rendering and input as a single surface. The former
input-parent/render-child pair regressed after Space/full-screen ordering: the input
moved but the rendered window remained at the old position until final persistence.
There is no second pet input/render window to synchronize. The independent message
bubble is another non-key NSPanel; it hides during dragging and anchors only at
completion/resize/show, never follows the native drag loop. Its material view is a
sibling behind WebKit so accessibility can reach the message and action buttons.
The bubble is hidden while quick input or foreground chat is visible. Its default 360x96 bounds
are clamped to the screen, above the character or beside it near the top edge.
Explicit approval expansion permits keyboard focus and grows up to 480 points.

Since AppKit may omit mouse-up, move notifications and a release-state timer
(active only during an actual drag, not click classification) end capture and
persist the final frame. The timer never moves windows and is invalidated on
completion/quit; there is no idle polling. The input view exposes accessible press
and context-menu actions. The panel is released on explicit quit; event monitors
and notifications are removed before deleting the Go callback handle. No private
Wails imports or runtime class mutation are used.

Three.js renders the current skinned pose into a 180x240 alpha target at most 15 times
per second. Coverage travels as a packed bit mask (coalesced, one request in flight),
and the host expands it to the original byte-mask contract. The host
uses that coverage (with two logical points of hit tolerance) to change OS mouse
ignoring before mouse-down. Native capture tracks drag until mouse-up, even outside
the initial hit region. Mouse monitors observe position only; no keyboard monitoring,
Accessibility permission or event tap is requested by the app.
This is an experiment qualified on the pinned macOS/Wails baseline, not a portable
promise for every compositor. It must be requalified if the host/rendering stack changes.

The input capsule starts at 420x64 logical points and grows for up to five editor
lines, then scrolls. It is below the visible feet, horizontally
clamped to the display work area and flipped above when there is insufficient room.
Pet clicks toggle against actual native window visibility (including outside-click
dismissal); tray Open remains idempotent. A pending native file sheet blocks toggling.
It takes focus on explicit open and restores the prior application on explicit close
when it still owns focus. Outside clicks or app deactivation dismiss without restoring the old foreground app;
explicit Escape/Cmd+W restores it only while the panel still owns focus. Native file
selection sheets are excluded from automatic dismissal. The plus popover only provides
local file selection and enabled skill/plugin-skill references returned by Codex.
Configuration is separate. AppKit owns NSStatusItem/NSMenu because the pinned Wails
menu API cannot embed NSSlider. Both the tray and pet context menu use the same compact actions; continuous sizing
lives in the shared settings window.
The status icon is an embedded, unchanged copy of the homepage favicon, with provenance
in internal/desktop/assets/README.md. There is no runtime sibling-repository dependency.
The menu-bar-only state has no Dock entry. Open chat/settings promote the application
to regular activation before ordering in; the Dock entry remains for covered or minimised
windows. Closing the last contextual window restores accessory activation. The combined pet/input panel uses
CanJoinAllSpaces + FullScreenAuxiliary, with CanJoinAllApplications on macOS 13+.
The input capsule uses MoveToActiveSpace + FullScreenAuxiliary, so explicit open
does not return to an old desktop. Space changes dismiss the capsule without focus
restoration and order the already-visible pet without activation; they neither
rewrite placement nor undo a hidden preference. Display/wake recovery remains
separate. The former FullScreenNone behavior is superseded by the user's follow
requirement. Full-screen following is now verified on the single Retina baseline;
physical multi-display/DPI/hot-plug and sleep remain pending.
Optional traces use public onActiveSpace/occlusionState and transition notifications
to distinguish ordered-in windows from currently visible windows.

The Xiaoai player targets 30 fps for visible motion, throttling frame requests
themselves; actual cadence depends on WebKit/display scheduling. Hit masks remain capped at 15 Hz. Hidden/document-hidden/reduced-motion paths stop frame
requests and discard transient gestures. WebGL loss pauses; restore redraws the model
and hit mask. Teardown disposes actions, geometries, materials, shadow/mask targets
and the renderer. Backend observation sends only idle/working/waiting facts; an
unresolved approval triggers attention once. Explicit MCP gestures play once and
return to the latest base activity even if the backend finishes during feedback.
The reviewed neutral rendering profile is ported into typed host code; no model-pack
JavaScript is executed. The static stick figure remains a loading-failure fallback.

Live size previews run directly on AppKit's menu tracking loop, preserve the feet
anchor subject to screen bounds, and never move the menu. Completed mouse, keyboard
and accessibility edits enqueue the exact final x/y/scale for Go validation and atomic
persistence. Completed native gestures are consumed in arrival order without holding
a service lock on AppKit; size -> hide -> quit cannot overtake each other. The queue
is stopped before deleting the callback handle. Renderer preview queuing is removed.
A bounded ResizeObserver projection (64–500 points) sizes the input surface and
reuses native edge avoidance. No permanent connection-state helper is rendered;
send availability comes from the real backend snapshot. Enter routes through
the same send button and its availability; Shift+Enter keeps native textarea editing.
IME composition/keyCode 229 events are left to the input method, and repeated Enter
keydown events do not submit repeatedly. No draft is cleared without a real send.

Local attachment selection uses a native file sheet. Go owns selected paths; the
renderer receives opaque local IDs, basenames and sizes only. Selection is atomic,
deduplicated by cleaned path and limited to eight regular files. No bytes are read
or uploaded by this staging path. Selection survives closing the panel and renderer
reload, but not app exit; text drafts remain renderer-local. The P2 adapter then
revalidates and copies bytes only on explicit submission.

Desktop settings are a host-only bridge, separate from `frontend/src/backend/contract.ts`.
P2 backend.Service is registered separately; it does not expand desktop.Service into an execution engine.

## Pet message projection

`PetSnapshot` supplies only the latest user/assistant/activity after the latest user
boundary, at most three items with bounded preview text, plus pending event titles.
It strips tool details, artifacts and authorization payloads from the passive bubble;
the explicit details/decision paths load the full authoritative snapshot. The bubble
polls serially every 400 ms while the pet is shown; input/history polling runs only
while those surfaces are open. The current adapter still clones history to make a
snapshot; true incremental subscriptions and long-history performance remain gates.

## Long-lived Bot and resident wakeups (2026-09-19)

See [Bot design](bot-design.md) for the product authority. `internal/bot` owns stable
identity, persisted reminder definitions/occurrences and the private MCP bridge. It
starts only after single-instance ownership, wakes the existing Bot only when idle,
and keeps unknown dispatches unresolved until a native receipt proves acceptance.
Codex explicitly uses empty runtime workspace roots and a fixed Bot instruction
prefix. The private result directory is not a user-selected workspace.

The adapter records native root/child relationships. Worker messages remain internal;
worker decisions keep their own thread/turn/request target. Explicit Stop interrupts
owned active turns before cleaning their terminals. Native worker communication stays
in Codex, without a second orchestration protocol or UI task/session manager.

The native host projects `TaskPreview` (opaque product handle and original assignment)
into nonactivating AppKit task bubbles. Hover/collapse has no backend effect; explicit
click resolves an owned `WorkTerminalProvider` target and launches the user's external
terminal. New owned Codex processes expose a private Unix socket; a native TUI attaches
to the same App Server with `--remote … resume …`. The adapter remains subscribed across
idle and human-created turns, with one resume on reconnect and no worker polling loop.
The renderer never receives a shell command or native thread ID. Caelis terminal attach
remains an optional adapter capability, not a presumed protocol equivalence. See
[task delegation](task-delegation.md) for lifecycle and verification boundaries.


Three presentation surfaces share theme tokens and one host draft. Approval expansion
changes only the bubble's explicit keyboard eligibility. Result acknowledgement is
persisted independently from history and execution. Native frontmost chat suppression
prevents duplicate bubbles without losing background observation.

## Menu, settings and click routing (updated 2026-09-23)

`BotPetInputView` owns physical input through two `NSClickGestureRecognizer`s and
one `NSPanGestureRecognizer`. Only the single-click recognizer requires double-click
failure; both clicks require pan failure. Double-click directly recalls chat and
already-open settings, without running a single-click action or opening a composer.
Single-click follows AppKit's user-configured double-click interval. There is no
manual `nextEventMatchingMask` loop or application timer deciding click count.
Pan hands the original mouse-down event to `performWindowDragWithEvent:`; Window
Server still owns movement. The small original hit region is retained between
clicks so animated silhouettes cannot turn the second click into pass-through.
Outside clicks, right-click menus, hide, deactivation, Spaces and shutdown cancel
pending recognition. Accessibility press remains an explicit single action.

Window transitions dismiss the composer without restoring the prior foreground
application; explicit composer close keeps its existing focus-return behavior.
Native preparation occurs before the AppKit window transaction, never by acquiring
the Go service mutex from inside that transaction. Recall reads `NSWindow.isVisible`
and `isMiniaturized`: Wails beta.6 `IsVisible()` measures occlusion, so it cannot decide
whether a fully covered settings window is open. Closed settings remain closed;
covered/minimized settings are recalled after chat and keep their current page.
An attached native file sheet retains focus rather than being hidden by recall.

System API references: [NSClickGestureRecognizer](https://developer.apple.com/documentation/appkit/nsclickgesturerecognizer),
[gesture failure requirements](https://developer.apple.com/documentation/appkit/nsgesturerecognizerdelegate/gesturerecognizer(_:shouldrequirefailureof:)),
[Window Server dragging](https://developer.apple.com/documentation/appkit/nswindow/performdrag(with:)).

Global chat recall uses a driver-owned Carbon hotkey on macOS, without an event tap
or keyboard monitoring permission. The portable preference uses physical key codes
and Control/Alt/Shift/Meta modifiers; the default is Control+Shift+Space.
Registration failure preserves the prior shortcut. Saving failure rolls registration
back; preferences use atomic replacement with mode 0600. A hidden pet does not disable
the shortcut. A future Windows host must implement its own driver and conflict checks.

The hotkey and settings preview both call `ToggleHistory`: only an active application
with a visible, key, non-minimised chat hides it; background/hidden/minimised chat is raised and its editor focused through the
existing window-recall preparation. An attached native sheet is recalled rather than
hidden. No remembered toggle bit or backend cancellation is involved. Menus and pet
double-click keep their unconditional recall path. Pet single-click toggles its nearby capsule;
the centered-input path is removed. Closing that capsule restores the previous
application when appropriate; outside clicks retain their chosen destination.
Busy/disconnected chat retains the draft and existing native send/approval gates.
`window_lifecycle_darwin.m` reads native ordered-in/minimised state for Dock policy,
not occlusion. Close hooks hide only the corresponding reusable webview and recompute
activation policy; they never quit or cancel backend work. The Dock reopen hook cancels
Wails' default reveal-all-windows behavior and recalls only still-open chat/settings,
so private pet/composer/prop surfaces and explicitly closed settings stay hidden.

The connection page owns installation/account/model-service management, with one
connection check and a conditional restart action. Routine model selection lives only
in ExecutionSettings. Its single save action compares conversation/work drafts against
their last accepted values, calls only changed settings, and records each receipt before
continuing. Partial failure keeps remaining changes dirty and names the saved scope;
there is no cross-file atomicity claim. The form remains mounted across navigation and
native close, preserving drafts; navigation resets scroll to the top. Worker settings
share the model catalog and are behind an optional disclosure. Untouched inherited
conversation defaults are never persisted by a work-only save.

Explicit `history-open` resets bottom-following even if the window is already active.
Native key-window reconciliation uses `history-visible` so ordinary app switching does
not disturb manual history reading. An active-only ResizeObserver tracks the transcript
content and viewport, keeping the latest message visible after composer mount, draft
restoration and window resize. Loading earlier messages retains its message anchor;
manual scrolling suspends bottom-following until the user returns to the bottom or
explicitly recalls chat. Focus requests do not remount or reload the shared draft.

Quick input remains mounted in its hidden webview, refreshing the revision-fenced
shared draft at activation. `ComposerSnapshot` excludes transcript and approval bodies
before copying; both native click routing and quick input polling use it. Character
and plane renderers load separately so text surfaces do not parse Three.js at startup.
The IM surface fills the window; individual bubbles retain relative readable widths.

Model/effort/service-tier options come from bounded `model/list` pagination, not a
hard-coded model list. `execution.json` is separate from CLI connection preferences.
Before explicit saving, native Codex defaults remain in force. Saving revalidates the
selection against the current catalog and is rejected during active/uncertain work or
pending approval. The adapter applies model, effort, service tier and approval mode to
thread start/resume and each new `turn/start`, never `turn/steer`. An explicit null tier
clears Fast. A failed native request is reported; no model/effort fallback is attempted.
Workspace-write + on-request + auto_review remains the default; user review retains
that sandbox, read-only uses never/readOnly, and explicitly selected full access uses
never/dangerFullAccess. The isolated acceptance flag always tightens back to
untrusted/user/workspace-write. Model settings cannot rewrite pending approval targets.
Native Codex requirements remain authoritative and may reject selected policies.

Delegated work has separate per-provider `work-execution.json` preferences. An empty
model reads the Runtime default when creating work; only an absent configured model
falls back to the Bot's model/effort/tier. Manual selection overrides that group.
`WorkExecutionProvider` validates and persists these preferences independently of the
resident execution settings. They never change permission policy or Runtime globals.
Codex reads `config/read` and pins the native thread receipt, including model provider,
for continuation/restart; legacy tasks first resume without model overrides. Caelis
resolves public Host model metadata and persists an explicit application worker profile.
Lookup errors are not absence. Existing tasks keep their settings. Caelis application
team configuration remains deferred to [Caelis #74](https://github.com/caelis-labs/caelis/issues/74).

Protocol references: [model/list](https://learn.chatgpt.com/docs/app-server#list-models-modellist)
and the vendored Codex 0.153.4 ModelList/ThreadStart/ThreadResume/TurnStart schemas.

The shared settings webview uses host geometry for continuous scale preview, coalesces
in-flight changes, and persists the final value on release. Runtime file selection
and diagnostic saving attach sheets to that same settings window. None of these
presentation commands cancels work or sends a prompt.

Stable bundles load the pinned Sparkle 2.10.0 framework through the narrow AppKit
bridge in `internal/desktop/updater_darwin.*`. Sparkle owns scheduling, native update
UI, signature validation, download, replacement and relaunch. The signed app embeds
the persistent Ed25519 public key and fixed HTTPS feed. Both feed signatures and
archive validation before extraction are required. Automatic checks default to daily;
downloads/installations require a user choice. Sparkle preferences are authoritative,
with no second preference store in the renderer. Development/preview bundles use
the existing bounded GitHub checker in `internal/updates` and manual installation.

Before relaunch, `Application.PrepareUpdate` fences user submission and checks active,
uncertain and delegated work. `Runtime.PauseIfIdle` serializes this check with reminder,
initialization and completion-report delivery; failed checks preserve scheduling.
The native delegate retries while work is busy. Successful admission closes owned
resources off the AppKit thread before invoking Sparkle's installation handler.
Cancellation before that point restores admission; hiding/closing panels never accepts
an update. `CFBundleVersion` now follows the numeric stable release version, rather
than the old constant `1`. Previews do not join or replace the stable feed.

Release automation signs nested Sparkle code before the app, notarizes/staples both
app and DMG, then signs a one-version appcast and artifact manifest. A separate R2 job
verifies these exact bytes, uploads under the fixed `caelis-bot/` ownership prefix,
reads them back, switches the feed and only then removes old Bot release objects.
See [release operations](release.md) for credentials, recovery and verification limits.

## Neutral appearance and native chrome (2026-09-20)

The pinned Wails beta.23 separates its WKWebView transparency bridge behind
`private_mac_apis`. `script/build.sh` explicitly uses `production,private_mac_apis`,
and native vet/tests use the same transparency option. Without it, native windows and
WebGL canvases can both be transparent while WebKit still paints an opaque rectangle
(dark in dark mode) across the pet and the larger detached prop window. The opt-in
uses Wails' guarded private `drawsBackground` operation; CSS transparency and public
`underPageBackgroundColor` do not replace it. This is a native macOS dependency to
revalidate on Wails/OS upgrades, not an asset or character-animation change. See
[Wails transparency/build contract](https://v3.wails.io/guides/build/private-macos-apis/).

Chat retains one native titlebar and standard window controls. There is no duplicate
HTML title/header. Pending response feedback lives in the transcript as an avatar
and animated dots, replaced by visible streaming text. The composer shares its
primary button between sending and explicit interruption; Enter can only submit,
never interrupt an empty draft. Backend capabilities continue to gate sending,
steering and interruption, and approvals/recovery retain their dedicated surfaces.
Shared light/dark semantic tokens use neutral grey surfaces. Window
titles/accessibility and normal native titlebar dragging remain intact.

Wails' public transparent-titlebar options are used for chat/settings. A decorative
AppKit sibling behind WebKit supplies material for quick input, the message bubble,
and the 184 pt settings sidebar. On macOS 26+, the host resolves the public
NSGlassEffectView class at runtime with Regular for text surfaces and the settings
sidebar; older systems use NSVisualEffectMaterialPopover. There is no native tint.
Reduce Transparency or Increase Contrast switches the material to a solid system
background and updates live through accessibility display notifications. The native
backdrop does not participate in hit testing or replace WebKit's responder and
accessibility hierarchy. A document-startup marker enables native-material CSS only
on these surfaces, and didFinishNavigation synchronizes it again in case initial
navigation was already in flight. Chat history and the settings form retain opaque
semantic colors, with a lighter settings background coordinated with the sidebar.
Quick input and the non-key message bubble share a translucent reading fill, keeping
the text region bright and stable even when native glass varies with activation or
backdrop. Opaque text sits above this fill; the native glass remains visible at the
edge and provides the actual blur/refraction. The message capsule is 56 pt tall by
default with a 28 pt corner radius, 14 pt medium text and a restrained renderer
shadow. A 6 pt transparent gutter on each side contains the shadow, making the native
window 68 pt tall. There is no additional NSWindow shadow; the outer 480 pt height
limit remains aligned with the renderer and expanded decisions scroll within it.

Settings defaults to 960×680 with a 760×540 minimum. Shared SettingGroup/SettingRow
components align labels and controls, with optional details disclosed on demand.
Permission scope, actionable errors and cleanup confirmation remain visible. No
settings action bypasses the existing native execution gates.


## First embodied implementation slice

Reusable humanoid production follows [资产制作边界](character-assets.md):
a shared editable body/rig/motion base, a character-specific appearance and fit profile,
and native placement independent of renderer locomotion. An editable 22-joint FK core
and four-clip mannequin prototype remains as a regression reference outside the production app;
the v0.2 authoring base described below supersedes it for fitting. Character migration is pending. Xiaoai's auxiliary knees and legacy foot-follow compensation are
a compatibility bridge; new templates use a continuous thigh/calf/foot/toe hierarchy.

The latest [资产制作边界](character-assets.md) selects official
base meshes and Rigify as the starting point for a Q-proportioned template; the homemade
mannequin remains a regression fixture. Authoring controls are separate from baked runtime
deformation bones. Neither the prototype's 22-joint count nor Xiaoai's historical bind
matrices constrain the future export rig. Wide-sleeve/hair proxies qualify the template
before appearance migration. The independent v0.2 authoring template now exports a
34-bone continuous hierarchy and 30 common clips, with source-sampled face/hand morphs.
Standard and longer-leg variants retain the same semantics. Saved artist sources can
be exported without regenerating geometry or animation. This remains an offline asset
deliverable; the production Xiaoai scheduler has not migrated to it. Third-party retarget
tools remain candidates. Paper-plane assets and performances are Xiaoai-only extensions,
not requirements of the common humanoid contract. Native drag ownership is unchanged.

`internal/desktop/behavior.go` guards prop requests against invalid coordinates,
hidden/stopped hosts and authoritative work/waiting state. AppKit owns actual geometry,
non-key surfaces and release IDs. Native snapshots use logical bottom-up points; Dock
bounds are estimated from visible-frame insets, foreground geometry is sampled at most
once per second from window metadata (no title/content/screenshots/permission request).
Window ID, observation uptime and snapshot revision bound stale observations. Missing
geometry stays null; Space/display changes invalidate it. Context events are capped at
8 Hz, and the timer is parked while hidden.

`BehaviorDirector` is local and idle: four quiet / two active behaviors, eligibility
before weighted selection, no immediate repetition, cooldown, direct-interaction
priority and two work variations. `PoseLayer` restores its clean base before each
AnimationMixer update: constant PropertyMixer tracks can skip writes, so naive additive
rotation would drift. Head/body/hair use small bounded offsets and different damping;
clip weights use a 400 ms smooth quintic transition. Character artwork and original
rig/clips remain unchanged. This does not constitute a reusable public behavior SDK.

The prop is a separate bounded 520x360-point (scaled/clamped) transparent NSPanel,
permanently non-key and click-through. It never joins the actor's Window Server drag.
A flight has one ID, 7.5 s local curve and a 10 s native deadline. Late receipts cannot
close a different flight. Native input/menu/work/visibility/Space/display changes and
renderer reduction/loss cancel it. The held plane is visible only during plane-care
and pre-release plane-play; ordinary idle, work and direct interaction stay empty-handed.
Release, cancellation and completion do not respawn the held copy; the next eligible
prop performance brings it out again, without chasing a moved character. Motion does not perform per-frame Go IPC or Agent calls.
Both WebGL surfaces stop frame requests while hidden/inactive; small cached prop
geometry/materials persist between flights and are disposed at renderer teardown.
Broader GPU-memory reclamation, refined catch animations and foreground-window-center
avoidance remain follow-ups, not verified behavior.


### Xiaoai view-dependent illustration corrections (2026-09-21)

`desktopPetViewMorphs` opts an asset into named front/quarter/side morph projection.
`ViewCorrectives` reads the head's world orientation relative to its bind orientation
and the camera; it runs after facial weights and before both colour rendering and
native silhouette readback. It changes only its own view/cross weights, never bones,
authority, conversation state or native position. Back views fade the corrections out.
Blender authors expression/view cross residuals so simultaneous blink and head turn
remain attached. Static source and GLB use the same endpoints; no runtime resculpting.

The loader removes only all-zero relative targets per primitive before GPU allocation,
remapping dictionaries by name. Facial animation accepts partial dictionaries and uses
asset-wide refined-expression availability. This opt-in format has bone-only animation
clips; future assets with morph weight animation tracks must not use this pruning path
without also remapping those tracks. Existing assets without metadata are unaffected.

### Current Caelis clothing/skin boundary (2026-09-21)

The current character exports separate body, face, hair, clothing and accessory skin layers
with `caelisLayer` extras and one shared armature. Authoring sources keep the individual
objects editable. Runtime presentation and task identity remain independent of these layers.
`desktopPetArmMotion` opts into bounded elbow/wrist movement and shoulder-driven running;
archived characters retain their previous path. See [成品合同](character-assets.md)
for linear-skinning limits and the unfinished full-body/wardrobe work.

### Relaxed arm fit (2026-09-22)

`desktopPetRelaxedArms.version=1` enables chest-relative passive wrist/pole anchors
and single-arm inquiry/thinking in the existing pose layers. The authored Blender
rest and runtime solver retain the same fixed bone lengths, lowered wrists and small
elbow flexion. Gesture damping and native drag ownership are unchanged.
The current asset is `caelis-soft-outfit-v1.glb`; archived assets retain their old pose path.
`desktopPetSoftOutfit.version=1` keeps idle stretching on the fitted near-arm solver
rather than releasing it to the old wide bind stance. Clothing uses authored skinning
and local contact shaping; no cloth simulation or task-state authority is introduced.

## Local appearance content

`internal/contentpack` owns the inert `caelis-content` v1 manifest, bounded ZIP/GLB/PNG
validation, immutable local archives, independent selection and resource serving.
The creator CLI and native installer share this implementation. Local packages are
never labelled verified publishers and cannot carry executable extensions.
`desktop.Service` exposes explicit picker/import/select/remove operations; backend
providers, Bot identity, tasks and approval authority do not depend on this store.
The native resource handler exposes only verified GLB/PNG bytes under content hashes,
without modifying the signed application bundle. Selection is persisted separately.

Renderer snapshots carry revisions; late failure reports cannot reset a newer
selection. Models preload before replacement; old geometry, animation and GPU
resources are disposed on commit. Community geometry receives the basic clip/profile
path, not character-specific procedural metadata. The built-in character remains
the recovery path. The current developer workflow and strict supported capability
limits are in [content packs](content-packs.md). Early releases provide bundled content
and local third-party imports. Any additional capability requires a separately defined
versioned contract before it can be exposed to creators.
