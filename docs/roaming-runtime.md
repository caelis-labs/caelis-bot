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
