# Optional nodes and Bot roaming

The Go contracts in `internal/backend/api/nodeplane.go` and
`internal/nodeplane/` freeze the management projection and host boundaries for
this feature. `internal/app/node_management*.go` and
`internal/backend/nodeplane.go` implement the explicit management facade and
native assembly. Concrete owners implement transport, native admission,
Notebook transfer and presentation. Installed binaries, fixture tests and a
generated frontend contract are not evidence of remote execution or safe roaming.

## Identity and management state

A Node is a machine. Its Codex and Caelis installations are separate Runtimes;
Bot and Worker are separately negotiated roles. A Node may have either Runtime
or both. Installation version, authentication, health and role eligibility are
independent facts. Windows Codex may expose Worker capability; it cannot own Bot.

`NodeCatalog.SelectedNodeID` controls only the settings view.
`ActiveBotNodeID` reports the execution owner. `WorkerTarget` retains the exact
machine/backend/Worker role used for delegation. Selecting a Node never starts a
Runtime, transfers Notebook, changes either execution target or claims a lease.
A catalog can retain unavailable targets and unknown ownership; it must not
infer authority from a selected label, Runtime version or network connectivity.

Local discovery runs through native assembly. SSH enrollment and outgoing/NAT
join are explicit additional paths. A node agent receives its machine identity,
OS, transport pairing and local private profile from trusted bootstrap assembly.
Incoming requests and renderer fields cannot establish or overwrite that identity.
User-selected SSH destinations are transport information, not Node identity.
Authentication stays at each Runtime; catalog and receipts expose no credentials,
machine paths or native conversation identifiers.

`NodeManagementController` is the user-only facade. `NodeRuntimeConfiguration`
returns both the exact Node/backend edit guard and the existing configuration
projection. For a configuration edit, the guard revision is its native
configuration revision. Installation uses its displayed installation/catalog
revision. The global catalog revision used by `SelectNode` is a separate view
compare-and-swap. A completion applies only if `MatchesEdit` still matches the
captured Node, backend and revision; otherwise the receipt is retained without
changing the newly selected view.

`NodeRuntimeConfiguration.installation` is the native managed installer state,
independent of a Runtime discovered on PATH. A null state is unavailable or
unknown and disables installation writes. A known uninstalled state selects
`install` with an empty expected version; a known installed state selects
`update` with its exact managed version. Reviewed versions remain authoritative.

Every mutation has an original `NodeOperationRef`: Node, backend, operation ID
and semantic request digest. Persist it and the exact intent before dispatch.
The journal is scoped to the exact Node/backend and rejects a reused ID with a
different digest. A committed receipt proves the operation, not a different
operation that happens to produce the same configuration. Unknown outcomes may
only call `ReconcileNodeOperation` with the original reference. A read or view
change does not authorize resubmission, a new ID or another Runtime.

`NodeManagementRequest` allows exactly one configuration or installation
payload. Configuration uses existing semantic actions plus explicitly scoped
`conversation-model` and `worker-model` preferences; installation uses
`install`, `update`, `check-update` or `detect`. The contract has no arbitrary
method, shell command, credential transfer or LLM cluster parameter.
Concrete providers advertise only actions whose whole path works.

`ManagementDigest` hashes a versioned ordered list of strings. Each string is
encoded as decimal UTF-8 byte length, `:`, then its exact bytes, with no additional
separator. The order is `node-management-v1`, guard Node ID, backend, revision,
then payload kind. `configuration` is followed by action, ID, name, description,
selection model, effort, service tier and expected revision. `installation` is
followed by action, version and expected version. Empty fields remain present.
The operation ID and digest itself are excluded; Ref Node/backend must match the
guard separately. Cross-language Unicode coverage lives in the source tests.

## Explicit node connection setup

The existing connection/model workspace uses the original local Runtime client
when viewing its active local profile. Other enrolled targets retain an exact
Node/backend/reference through the same connection wizard. Viewing or switching
a Node never starts a setup Host. Unsaved drafts and late native results retain
their original scope; changed local ownership or profile blocks global writes.

An explicit Caelis connection Begin validates the displayed native guard and
verified companion before preparing the fixed target-private Store. A cold
setup owns a bounded foreground Host using the existing public SDK; it needs no
configured model, application enrollment, Bot activation or model request.
Warm setup borrows only the exact live owned Host/generation and detaches on
wizard close, leaving the Bot running. Shared Hosts are never adopted.

The original Begin is journaled before dispatch. Unknown Begin, SDK flow start
or cleanup cannot launch a replacement under a new ID. Closed paired methods
accept the original reference and no endpoint, Store path or Runtime settings
override. Authentication inputs are entered by the user in the existing password
controls and delivered to that target's native SDK. No source-store secret copy,
autofill, credential receipt or generic credential-transfer method is provided.
SDK failures expose safe typed uncertainty; cleanup errors remain visible.
Transient challenge/flow references are released after cleanup.

Native setup metadata persists separately from admitted Worker bindings so an
unconfigured alternate Caelis installation retains its designated Store across
generations without granting Worker authority. The APP's first roaming bootstrap
currently requires its live owned Codex source. Managed targets can use either
Codex or Caelis; the ordinary shared Caelis source remains ineligible.

## Ownership and safe admission

`AttachNodeManagement` creates the in-process local catalog agent and loads a
bounded private `nodeplane/config.json`. It does not start a broker or socket.
The document preserves enrolled Node IDs, existing SSH associations, private
bootstrap paths, outgoing gateway pairing and designated coordinator across APP
restart. Temporary view selection is not persisted in that document.

SSH Add probes the selected Linux architecture, resolves the APP-owned artifact
manifest, verifies source revision/checksum/ELF architecture, prepares the target
user's private directory and installs the verified agent bytes. It verifies the
foreground agent's exact identity before publishing enrollment. The temporary
management agent holds no Bot execution authority. Its observer closes on APP
detach; node lifecycle owns any separately started Runtime.

Each Add records its original `OperationID` and immutable request in a private
enrollment journal before bootstrap can mutate a target. Read-only architecture,
SSH authorization/host-key, and artifact checks can return a confirmed failure
with a safe reason; raw SSH diagnostics stay native. Once bootstrap is admitted,
lost delivery remains unknown until `ReconcileNodeEnrollment` can prove the
original result from its exact journal and published pairing. This query never
repeats bootstrap. Cancel keeps the original ID; Refresh and Check original
receipt query it, and `NodeCatalog.PendingEnrollments` restores it after settings
remount or APP restart. Only a confirmed terminal result permits a new explicit
Add. A still-unknown bootstrap requires original-receipt recovery or manual
inspection of that target. The private journal retains at most 128 receipts;
capacity or unavailable private storage rejects new enrollment before mutation.

Outgoing Add requires a designated coordinator. An enrolled SSH coordinator
gets a separate private join slot rather than replacing its existing agent
socket. The outgoing client subsequently reaches the joined Node through that
coordinator's exact SSH/helper/socket pairing. A local coordinator requires the
user's existing authorized SSH destination in native configuration
(`CAELIS_BOT_NODE_OUTGOING_SSH_TARGET`) and the verified packaged native join
helper. No route is guessed or account provisioned. Without that route Add
returns an explicit unavailable result and creates no permanent waiting entry.
Join instructions give two foreground commands on the target; both remain
explicitly running until the user ends them. Detect reads the actual joined
agent. This implementation has source/fixture coverage, not live SSH/NAT
qualification.

`NodeCatalog.PendingOperations` projects original unresolved journal references
so a remounted settings surface can recover them. The facade prevents duplicate
in-flight dispatch, rehydrates native pending references before fresh mutation
and scopes its barrier by Node/backend. Native journals own the final atomic
check and receipt recovery. A failed or mismatched response becomes unknown with
the original reference; another selected view never consumes it.

Local Codex settings use the existing Backend preference mutex for a true
displayed-revision CAS. Conversation and Worker remain separate scopes;
model/effort changes retain approval policy and do not expand service-tier
support. A missing/inactive Runtime returns its exact installation guard and
availability metadata rather than a different backend's settings. Configuration
and installer availability are independently projected.

A thin APP's Backend represents the remote Bot. Its local catalog therefore
uses independent native SDK/configuration ports and a private local agent
profile, never that Backend's model or preference methods. Local Caelis access
requires an explicitly supplied local native profile. The existing product
pairing remains read-only machine metadata until exact agent enrollment; its
opaque current management binding is projected once as `PairedRuntime`.
Offline bindings are empty and cannot mutate. The existing remote Runtime/model
surface keeps that original product scope; local node selection cannot redirect
it. A reconnect changes the binding and catalog revision. Machine OS/Worker
routes absent from the product-only protocol remain unknown.

`CloseNodeManagement` cancels optional management observers. Native runtime stop,
lease withdrawal and Notebook staging remain separate owners. Aggregate catalog
sorting copies source slices, so a read cannot corrupt a cached local catalog.


The broker is optional, designated by this single user, and defaults to the local
machine when explicitly enabled. It is a single point of availability rather
than a high-availability control plane. `AutomaticRoaming` is true only while
that broker is reachable and at least one Node has eligible Bot capability.
All nodes can initiate outgoing standard SSH connections, including reverse
forwarding to private same-user Unix sockets for NAT join. No public listener,
Tailscale dependency, SSH policy change or always-on service is required by the
default local product. An absent broker preserves direct in-process local APP
plus Runtime, with zero broker startup, enrollment or probe prerequisites.

`Coordinator` owns compare-and-swap claim, heartbeat and release. A lease binds
stable Bot ID, Node, backend and broker epoch. The broker epoch is independent of
any native Runtime controller epoch. Internal defaults are a 10-second heartbeat
and 60-second expiry. `Lease.TTLMs` is bounded to 60,000 milliseconds; compute the
safe local monotonic deadline from request start and subtract the time needed to
stop work. Response arrival and informational wall-clock `ExpiresAt` cannot
extend authority. Expired or uncertain authority closes new admission.

The current owner must reach safe idle before orderly release or handoff. Claims
require the complete latest Notebook and a native controllability proof verified
by the host owner. A partitioned owner must stop autonomous/background admission
and existing owned execution before another owner can safely claim. Grace time
alone is not proof that old Runtime work stopped. Stable local reclaim follows
the same claim and idle checks; it cannot create a second identity or run merely
because the local settings view was selected. Unknown native outcomes remain
fenced until their original receipts are resolved.

`AdmissionFence` is implemented at actual Runtime admission. Its proof covers
ordinary prompts, schedules, care, Dream, new Worker grants, native background
admission and old live work. `RuntimeProof.Controllable` is trusted host output,
not a renderer assertion or an authenticated wire Boolean. The broker must verify
it against the actual concrete runtime owner. A shared unmanaged Caelis Host is
not eligible simply because an APP connection can be detached.

The existing Caelis wire schema has `ExpectedControllerEpoch` fields, while the
current adapter does not populate them. Existing `Close` detaches observation
without revoking a lease or stopping the shared Host. These facts prevent
treating schema availability or connection loss as a distributed admission
fence. A future eligible adapter must demonstrate actual epoch checks and native
quiescence, or use an owned Runtime process whose stop is independently verified.
This document makes no claim about uninspected Core internals or live remote
ownership acceptance.

## Notebook publication and transfer

`SnapshotRef` binds stable Bot ID, publisher epoch, monotonically increasing
version and lowercase SHA-256 bundle digest. Epochs are opaque strings. Versions
use canonical positive uint64 decimal strings in public/wire references to avoid
JavaScript numeric rounding; owners compare the integer value internally.
Version order continues across epochs. Reusing a version with changed epoch or
digest is a conflict; replaying an identical reference is receipt recovery.

Only the active leased owner publishes. Changed Notebook content receives a full
snapshot within at most 60 seconds by the internal default; unchanged content
does not require a new version. The broker validates publisher authority and
version atomically. Publication and read ports pass bounded verified bundles;
the transport and codec owners define their concrete byte limits and manifest.

Installation stages the complete bundle, validates its checksum and atomically
replaces the complete Markdown tree, including deletions. Deleted files cannot
be restored from an old snapshot, runtime history or a partial merge. Runtime
authentication, native conversation history and machine-local paths are excluded.
Retain the last complete cold bundle when staging, checksum, lease or activation
fails. Stop both source publication and destination admission as required by the
codec/lifecycle owner before an explicit transfer. A verified download is not
proof that destination Runtime activation succeeded.

## Module ownership and verification

| Module | Responsibility |
| --- | --- |
| `internal/backend/api/nodeplane.go` | Credential-free management projection and explicit user facade |
| `internal/nodeplane` | Shared host ports, semantic digest and identity/deadline validation |
| Node agent / transport | Native discovery, trusted bootstrap, installation and exact per-node receipts |
| Coordinator / broker | Lease authority, publisher CAS and the last complete snapshot |
| Notebook transfer | Manifest/checksum, complete atomic replacement and retained cold bundle |
| Runtime lifecycle | Actual admission fence, safe idle and verified owned-runtime stop |
| APP/backend management | Trusted port injection and user-only selected-node facade |
| Frontend | Temporary selection/edit state and exact original receipt recovery |

Dependencies flow from concrete owners to `nodeplane` and `api`; `api` does not
import a concrete owner or `nodeplane`. Existing task routes and the earlier
remote APP stream retain their own identities; this is a new feature contract.

The focused source tests exercise stale asynchronous edit rejection, mutated
management intents, Unicode digest interoperability, delayed lease responses,
large monotonic snapshot counters, old-snapshot rejection, Worker-only local
defaults and unavailable broker/Windows Bot capability refusal. They do not
qualify actual SSH/NAT execution, native epoch fencing or visual acceptance.

Native management fixtures additionally cover verified enrollment before
persistence, exact receipt routing after APP restart, corrupted pairing
preservation, actual outgoing gateway identity, stale local preference CAS and
thin APP reads/updates that make zero remote execution calls. Linux and Windows
CGO-free APP test binaries compile; native Windows lifecycle is not qualified.

The packaged Bot memory, memory reference and task guide were reviewed. These
contracts add no model tool or changed Bot workflow, so they require no Bot skill
edit in this slice. The Notebook owner updates guidance if transfer changes the
Bot's recovery instructions. The available GUI tool catalog has no native CUA
driver; no screenshot or native visual inspection is claimed here.
