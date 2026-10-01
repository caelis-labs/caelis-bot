# Foreground single broker

`serve-broker` is optional. Without an explicit broker selection the APP remains
on its existing in-process local path. This command does not install a service,
change SSH/network configuration, generate credentials, or start a Runtime.

Use a canonical absolute private state directory and a socket inside a same-user
0700 directory. Socket permissions are 0600. Peers are explicit existing native
agent sockets, including sockets delivered by an already authorized SSH reverse
forward. A private `--peers-file` has this shape:

```json
{
  "version": 1,
  "botId": "stable-notebook-bot-id",
  "peers": [
    {"nodeId": "node-inspected", "backend": "codex", "socket": "/absolute/private/agent.sock"}
  ]
}
```

The Bot ID is the stable Notebook identity, not the product transport's hash.
Each peer is contacted at its exact node/backend scope. Its trusted native owner
must attest complete snapshot installation, controlled autonomous admission,
lease-deadline fencing and safe idle for a claim. Installed executables or a
self-reported `Controllable` request field do not provide eligibility. Without
paired native owners the broker offers a cold cache and rejects claims.

Initial publication is a local, explicit configuration operation. The source
native owner must first close admission and all Notebook writers, export a full
validated payload with epoch `0`, and attest that exact prepared reference with
safe idle and no pending or unknown work. Then use `--seed-snapshot PRIVATE`
and `--seed-source NODE/BACKEND` with the configured paired source. Bootstrap
works only when there is no committed lease epoch or latest snapshot. There is
no network bootstrap/reset RPC. Failure does not authorize moving the original
Bot to the broker.

Claims compare the prior durable epoch and exact latest epoch/version/digest.
Only the current owner can publish; complete file lists preserve deletions.
The cache retains one verified complete payload (at most 16 MiB). Staging and
state replacement are synced before the new pointer becomes authoritative.
The prior payload is removed only after that commit. Native bindings, sessions,
credentials and unknown work are not transferred or replayed.

Leases expire after 60 seconds; renewal normally occurs every 10 seconds. The
client must use remaining TTL from local monotonic request start, fence before
broker expiry, and revoke on sleep/wake or failed confirmation. UTC timestamps
are diagnostic metadata. Restart waits a full lease interval before granting;
clock reversal, corrupt/missing durable state, or unavailable latest snapshots
fail closed. Wire release disables renewal but preserves the old deadline so
it cannot create overlapping owners.

The broker is a single point of failure. Availability requires a reachable
broker and an eligible ready node. This does not promise availability from any
machine or a 120-second recovery bound. Actual SSH/NAT deployment, authenticated
model execution, macOS GUI acceptance and measured recovery remain separate
acceptance gates.

Remote standby imports use a private client check before and after installing an
absent offline generation. A publication race or lost confirmation returns an
error and the generation must not be adopted or started. This is deliberately
not a remote rename transaction. Even a successful import is cold data: the
native owner must attest its exact installed reference, and Claim performs the
final authoritative latest-snapshot/epoch comparison before execution.
