# Native roaming deployment inspection

This checklist describes a sustained deployment; it does not launch one. The
user reviews the exact native plan before persistent execution. In this role
layout Ubuntu is the dedicated coordinator/cache, the local Mac is preferred,
and Fedora is a standby. Names below are placeholders for existing enrolled
Node IDs and trusted SSH associations. No account, key, forwarding authorization
or authenticated model is created by this plan.

## Inspect the source and sealed plan

Build the verified host companion from the frozen source revision, record its
SHA256, and use the same artifact manifest for native review and deployment.
The plan binds source revision's packaged helper bytes, enrolled identities,
original enable operation, target-local Runtime settings and the exact sockets.
A different helper, enrollment or native setting requires a new review before
retiring the source. A failed existing outbound authorization is a blocker;
it cannot be replaced by a guessed route or newly provisioned account.

A deterministic contained inspection fixture accepts an explicitly checksummed
prebuilt helper and writes a 0600 JSON plan. It runs a synthetic SSH architecture
shim and synthetic catalog metadata, without a deployment, Runtime or model:

```sh
source script/env.sh
CAELIS_BOT_TEST_HOST_BINARY=/absolute/verified/caelis-node \
CAELIS_BOT_TEST_HOST_SHA256=EXACT_SHA256 \
CAELIS_BOT_TEST_NATIVE_PLAN_OUTPUT=/absolute/private/inspection.json \
go test ./internal/app -run '^TestNativeRemoteCoordinatorDoesNotRequireOrDeployRuntime$' -count=1
```

The output is explicitly marked `fixture: true` and `liveDeployment: false`.
For a given helper path/bytes and platform it contains a stable native plan and
checksum. Its Linux account metadata is synthetic. It verifies planner behavior
and path derivation; it cannot attest real SSH authorization, account readiness
or live ownership. Never deploy its `/fixture` paths or Bot identity.

## Exact native layout

Let `K` be the first 16 hexadecimal characters of SHA256(original enable
operation ID), and `N` the same hash projection of a managed Node ID. For standard
SSH enrollment, `E = <target HOME>/.local/share/caelis-bot/node-agent`,
`D = E/roaming-K` and `I = <target HOME>/.caelis-bot-joins/r-K`.
For the local Mac, `L = <canonical /tmp>/caelis-roaming-<uid>-<root/op hash>`.
Every directory is same-user 0700; JSON and sockets are 0600. Existing unsafe
permissions or symlinks cause refusal; the product does not repair them.

| Owner | Durable files and directories | Private IPC |
| --- | --- | --- |
| Ubuntu coordinator | `D/supervisor.json`, `D/peers.json`, `D/bootstrap-peers.json`, `D/broker/`, supervisor log/lock and deployment state | `I/broker.sock`; temporary source proof at `I/bootstrap/agent.sock`; Mac/Fedora reverse peers at `I/joins/N/agent.sock` |
| Fedora standby | `D/supervisor.json`, `D/workers.json`, `D/agent/node.json`, `D/agent/execution.json`, `D/generations/`, `D/product.token`, supervisor log/lock and deployment state | `I/agent.sock`; outgoing route terminates in Ubuntu's exact `I/joins/N/agent.sock` |
| Local preferred Mac | The same managed files under `L`; actual cold bundles and deployment/recovery journal remain in the original APP profile | `L/agent/agent.sock`; temporary proof at `L/source.sock`; outgoing route terminates in Ubuntu's exact join slot |

The already enrolled target helper names are `E/caelis-agent` for management,
proxy and fixed directory verification, and `E/caelis-node` for the independent
supervisor/broker/managed owner. Each active reverse join additionally records
its nonsecret exact route as `outgoing-route.json` beside its local socket.

Ubuntu has no managed agent state, generations, Worker manifest or product token.
Its broker roster lists only the real Mac/Fedora managed proof peers. The
bootstrap roster separately identifies the actual stopped local source; it is
not a seeded or fabricated Runtime proof. Ubuntu's temporary management observer
uses its original enrolled `E` metadata slot and never becomes a lease claimant.
The actual journal contains the full paths; templates above do not authorize new
paths. Every derived socket must be absolute, canonical and shorter than 100
bytes before source retirement. The former standard nested reverse socket could exceed this limit; the
separate IPC slot keeps these inspected deployment paths below the limit.

The Mac and Fedora product tokens are generated on their own target only at
approved provisioning, stored privately, and never returned in the inspection
or transferred with the Notebook. Runtime authentication remains on each target;
there is no Ubuntu model login and no credential copying between machines.

## Processes and network exposure

Each enrolled owner launches the verified `caelis-node supervise-roaming
--plan-file <exact supervisor.json>` after approval. Ubuntu's supervisor starts
only `serve-broker` with exact `--profile`, `--node-id`, `--bot-id`, `--socket`,
`--peers-file`, `--bootstrap-peers-file` and `--preferred-node local`.
Mac/Fedora supervisors start `serve-roaming` with exact `--agent-directory`,
`--generations`, `--auth-file`, `--workers-file`, `--broker-node-id` and native
Runtime bindings. Fedora receives the frozen `--agent-socket I/agent.sock`;
the Mac's short socket remains in its private local agent directory.

Mac/Fedora use their existing Ubuntu association for `--broker-ssh-target` plus
`--broker-helper` and `--broker-socket`, and for `--join-target`, `--join-helper`
and `--join-directory`. Broker proxy and agent joins remain ordinary same-user
SSH, with batch authentication, strict known-host checking, no agent/X11
forwarding, no local commands or shared control connection, and bounded connect
timeout. The reverse join adds `ExitOnForwardFailure=yes`, `GatewayPorts=no`,
`StreamLocalBindMask=0177`, `StreamLocalBindUnlink=no` and the exact Unix `-R`
binding; it never removes an existing socket. Broker proxy/verification calls
clear configured forwards. Reverse forwarding exposes only the private
destination Unix socket.
No new public listener, host trust, SSH identity or service privilege is created.
A leased product owner uses literal `127.0.0.1:0`; its Codex App Server and
independent watchdog are target-owned children. An unleased standby does not
receive a product execution lease from mere installation/authentication.

## Original-operation reconciliation and rollback

The APP writes the original retirement intent before fencing its source, then
persists exact deployment phases: preparing, provisioned, bootstrap-confirmed
and owners-ready. Missing/unknown responses retain that original operation;
reconciliation observes its receipt and frozen paths without replaying prepare,
launch or bootstrap under a new ID. APP quit closes observation clients only;
it does not terminate the sustained supervisors, broker or Runtime owners.

Explicit Disable first verifies the exact active lease owner is safely idle
with no pending or unknown execution. It records one original disable ID/lease,
marks every supervisor `disabling` to prohibit restart, stops only managed owners
and confirms their publication/stop. The broker stays live while the latest
complete cold Notebook is read and installed into a fresh local generation.
Only after that confirmation are all supervisors marked `disabled` and verified
unable to restart. A failed stop, source retirement, snapshot, restore or marker
is visible as unresolved; do not invent a replacement operation or restore old
receipts. Cache/profile records remain available for exact reconciliation.

Socket cleanup follows the confirmed owner: listeners unlink their Unix sockets,
and reverse joins remove only the socket identity they captured. The small
0700 per-operation IPC directories and nonsecret route metadata remain for
reconciliation. Neither Disable nor APP detach recursively deletes the shared
`.caelis-bot-joins` namespace. Any separately authorized cleanup must address
only the exact stopped original operation, leave other operation slots intact,
and remove the shared parent only if empty. Durable cache, Notebook bundles,
generations and original receipts are retained unless data deletion is expressly
authorized; a process rollback does not imply that authorization.
