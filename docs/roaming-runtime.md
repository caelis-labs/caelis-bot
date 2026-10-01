# Managed Bot runtime lifecycle

The optional `roaming.Runner` composes a broker, paired native proof port and
`app.ManagedNodeFactory`. Default `app.New` retains its original in-process
local assembly and has no broker, SSH or node-service prerequisite.

`Prepare` installs the latest complete Notebook in an absent generation, creates
an empty local Memory scope and assembles a fresh native session. `TryClaim`
retains that same safely idle candidate across broker conflicts so trusted local
preference can establish stability. A newer snapshot retires the unused candidate.
The broker checks the exact installed descriptor and native generation before
claim. A successful grant is installed before `Start` can dispatch work.

The execution guard checks user inputs, resident wakeups, MCP tools, task starts,
continuations and native Codex admission. It measures authority conservatively
from locally monotonic request start, reserves fifteen seconds for complete
shutdown before expiry, and fails closed on wall/monotonic discontinuity. Renewal
runs every ten seconds. A failed confirmation closes all new admission and
cancels admitted operations without replaying the original request.

Managed Codex always launches an owned stdio process under a pinned, short-lived
native watchdog and never attaches to a shared App Server. Native deadline
confirmation precedes execution admission; renewal also updates the watchdog.
The watchdog owns the native process independently, fences on owner descriptor
EOF, and binds its own native sleep observer/inhibitor. Hard fencing freezes the original process tree, kills only
captured descendants and the original owner, and confirms exit before reporting
proof. Darwin uses PID plus captured birth identity; Linux uses retained pidfds.
The immediate process fence has a four-second suspend budget. Longer application
and local Memory quiescence follows separately. Unconfirmed termination or
unresolved work cannot produce safe idle or release proof.

Managed Darwin requires `Host.BindLeasePower`, backed by the opt-in IOKit native
power observer. The observer fences before acknowledging system sleep; waking
cannot renew or restart the old generation. Linux also requires a native power
binder/inhibitor. Binder unavailability disables automatic eligibility. These
source and process fixtures do not constitute live OS sleep acceptance.

A sixty-second publication interval gates every owned writer with the same safe
idle check, exports the whole Notebook, publishes against the exact current epoch
and updates the installed descriptor. Publication and trusted heartbeat proof
reads are serialized; publication or marker failures revoke admission and remain
visible to the caller. Busy work retains the last complete cold
snapshot. Old history, task ledgers, receipts, wakes, native bindings, Memory DB
and HANDOFF work are never imported or replayed.

Managed Caelis uses the public foreground `serve` command under the same independent
watchdog. It requires an explicitly designated private node store, exclusive
owner lock, exact foreground discovery, and a target-side authenticated model.
Existing default or shared stores are never adopted or killed. Authentication
stays on the target; no credentials or old application/session bindings are
transported. The foreground host can serve explicit setup while prepared, but
native execution requests require the installed lease. Default shared Caelis
continues to detach observers and remains ineligible for automatic takeover.
Windows has no eligible native owner.

Remote Worker admission requires a pinned broker identity and the actual adapter's
negotiated native lease capability. The native source retains its binding and
operation and adds the actual managed node and live epoch. Catalog claims and
renderer fields cannot grant that capability. Unknown external effects are not
replayed. Process proof covers the original launched root and retained descendants;
an unobserved deliberately orphaned process cannot be retroactively adopted or
killed by PID/name, and must not be represented as a confirmed external outcome.

The broker's native enrollment can be pinned with `serve-broker --node-id` and
`DialUnixForBroker` or `NewSSHClient`. Worker lease reads require this configured
identity, an exact raw Bot/source-node/backend/epoch tuple, and a fresh read from
the paired native owner. `SnapshotState` is historical metadata and never grants
Worker authority. Remaining TTL is recalculated after the native read; pending
or unknown work may retain a controlled lease but cannot authorize replacement.
A changed native owner generation cannot renew the old grant.

`BootstrapSnapshot` is a closed one-time genesis operation. It accepts only the
exact complete epoch-zero snapshot independently attested by a previously paired,
actually stopped and idle native source. It cannot overwrite an initialized
cache or reset an epoch. A raw file or request boolean is never bootstrap proof.
The SSH broker transport invokes only the existing fixed `proxy-broker` helper
and pins the inspected native broker identity; it installs no SSH configuration.

`serve-roaming` is the optional foreground headless composition. It requires an
existing exact enrollment, pinned broker identity, target-private product token,
and an owned Codex or Caelis installation. The APP’s first bootstrap still
requires its live owned Codex source; an ordinary shared Caelis source cannot
establish native stop authority. It combines the real managed application,
request-start deadline guard, dynamic paired proof agent, Notebook Runner and
existing product protocol. Standby keeps the same prepared native generation
across ordinary claim conflicts, waits for an absent genesis snapshot, and
imports a fresh generation if latest changes. Product readiness is emitted only
after actual lease installation and native start. Each generation has its own
product receipt journal; old operation receipts are never adopted or replayed.
Linux and Darwin power fencing use `internal/leasepower`, with no Wails dependency
in the headless command.

`--managed-agent` uses the fixed separate `managed.sock` and managed owner lock
alongside an existing catalog agent. `--runtime-directory` resolves the actual
native managed installer executable. An optional `--workers-file` carries
approved nonsecret exact Worker deployment bindings, independently of Notebook
contents. The application saves and connects those exact targets through the
existing native setup API; unsupported lease-aware targets remain ineligible.

An independently running private Unix `serve-agent --managed-config` may own a
configured managed starter. Its private plan pins the verified helper checksum,
exact Bot/broker enrollment, existing outbound SSH pairing and broker private
slot, and existing target-private product token file. The closed start request
contains only native identities and an original operation ID. Repeating that ID
returns its original receipt; an uncertain start is never replaced or replayed.
The target-owned child and outbound proof channel do not depend on an observing
APP stream. Managed starts are rejected on transient `--stdio` agent lifetimes.
No service installation, new key, SSH configuration or real persistent deployment
is performed by these source fixtures.

Native enable also supports an already paired outgoing Linux agent without
incoming SSH to that candidate. `/v1/node/roaming-deployment` is a closed,
native-only port for metadata, preflight, fixed companion staging, preparation,
start, original disable control and receipt lookup. Metadata reports the
agent's own private directory, architecture, installed companion hash and its
existing explicit outbound pairing; the coordinator-side join directory is not
used as a target filesystem path. The APP verifies packaged release bytes and
freezes them into the same reviewed supervisor plan as the SSH path. A missing
companion may be uploaded in bounded verified chunks to one fixed absent helper;
installed helpers, arbitrary filenames, shells and Runtime credentials cannot be
sent or replaced through this port.

The original pairing is persisted only after `join-agent` verifies the existing
outward authorization. Deployment uses that pairing to the designated enrolled
SSH coordinator. Candidate Runtime and Worker executables must match native
metadata from that candidate; no Runtime is silently installed and unsupported
ownership, including Windows, remains ineligible. Both transport paths use the
same supervisor schema, directory validation and original disable marker.
Preparation creates only the fixed operation slot, nonsecret reviewed files and
a target-generated product token. Start persists its original dispatch intent
before launching the detached production supervisor. A lost reply remains
unknown; repeating start never launches a second supervisor. Observation is
reopened through the persisted paired route after the old management observer
closes, and does not own the serving agent or supervisor lifetime.

Contained real duplex and Unix-stream fixtures exercise metadata scope, fixed
artifact chunks, target-local executable checks, preparation, subprocess start,
APP observer detach and original disable receipts. They do not prove live NAT
routing, model execution, Linux sleep fencing or long-running remote acceptance.

The paired agent exposes a read-only managed product locator and a closed
product proxy. The proxy confirms the exact live owner and generation, uses the
target's existing bearer locally, and relays the existing product command and
receipt protocol over an outgoing-only connection. It has no arbitrary URL,
command, filesystem path or credential response. Private framed proxy requests
are bounded to 256 KiB and responses to 4 MiB, including bounded file transfers;
larger transfers fail explicitly. `PrepareManagedDisable` first cancels standby
claim intent. The active owner additionally checks safe idle, publishes a final
complete Notebook, and proves native stop before acknowledging release. A
failed or unknown outcome cannot authorize restoring local execution. The native
controller must disable all standbys before disabling the active owner.

`--bootstrap-peers-file` separates temporary exact stopped-source genesis proof
from the independently joined normal owner peers. Bootstrap proof does not become
an ongoing APP-owned lease authority. Live OS sleep and long-running independent deployment acceptance remain separate
native gates; abrupt-owner watchdog fixtures cover isolated processes only, and
temporary loopback/private-IPC and synthetic owned-process tests do not prove
those hardware or deployment conditions. Bot skill guidance is unchanged because
these native pairing and lifecycle controls add no model-facing tool surface.

Owned Caelis Workers use `NewLeasedWorker` with a separate private foreground
Host and bounded application protocol. Native assembly pins the raw profile Bot
identity, paired broker, actual source node/backend and Worker target. The private
dispatch Source retains its exact lease before projection into public Control
requests. Every new target mutation rechecks the paired broker and renews the
independent watchdog before sending bytes; a queued or resumed start keeps its
original Source. Lease expiry, broker loss, suspend revocation or changed epoch
cancels admission and hard-stops only that owned Host tree. Read-only receipt
reconciliation does not replay unknown operations. Ordinary shared Host Workers
report no lease-aware admission capability.

An owned leased Worker starts with read-only native Host/model readiness. Its
paired wire hello does not enroll an application, write an application secret,
or create an activation. The first authenticated mutation supplies the exact
private dispatch Source, installs the broker-validated ticket, and then performs
lazy application enrollment before the original task/continuation/workspace
mutation. Admission metadata checks also remain free of enrollment side effects.
Original recorded task receipts can reconcile without a new Source or enrollment.

Paired cancellation and approval frames retain exact task, turn, approval and
choice IDs. If a current native Source exists, the client rechecks it after
queueing and the target requires its grant to match the original task generation.
An idle UI control carries the authenticated server's private paired principal,
without inventing an activation. The target then validates only the exact stored
task Source.Lease against the live pinned broker before control bytes. Unpaired
source-less calls fail. Original unknown cancellation is not reissued against a
changed native turn or a new lease epoch; read-only receipts remain available.

Resident drivers classify connected inactivity explicitly. Transport failures,
authority faults and source annotation errors remain visible and cannot grant an
idle paired-control fallback. Codex persists the exact attempted cancellation
thread/turn before native control; an uncertain receipt fences later-turn replay.

The private managed Worker manifest freezes approved source node/backend sets
separately from the target's Codex/Caelis binary, store and execution bindings.
An approved same-node alternate backend uses an independently owned Worker;
the main backend continues through its existing source engine. A target binding
still needs its live native ownership/authentication handshake. An unavailable
optional Worker stays unavailable without stopping a healthy primary Bot.

When the same source returns with a broker-confirmed new lease epoch, the target
first verifies the previous exact owned process stop, then creates a fresh opaque
Worker directory, socket and receipt namespace. A persistent private generation
ledger retains original task/request IDs; those IDs cannot dispatch through the
new owner. Unconfirmed stop or missing original generation evidence blocks
replacement. Original task outcomes and old native receipts remain unchanged.

Native stop confirmation waits for the original launched root to be reaped. On
Linux, the dedicated watchdog is a child subreaper and reaps only captured, exited
pidfds; it never adopts processes by name or scans with unrestricted `waitpid`.
A native stop fault stays visible even when exact process cleanup succeeds. An
uncertain or non-waitable descendant cannot produce a confirmed stop receipt.
Darwin owned-runtime support requires the native cgo lifecycle helper; unsupported
builds report that capability before launching or preparing state.
