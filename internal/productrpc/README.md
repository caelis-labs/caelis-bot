# Optional product connection (F2)

The native client connects to one existing resident Bot over a loopback HTTP
listener or a private Unix listener forwarded by separately owned SSH. This
package never selects a Runtime, creates a local Bot, transfers native bindings,
or reads credentials. Default desktop assembly remains local.

The thin native APP may instead use `NewStdioClient(StdioOptions, stream)` over
an owned SSH process. `caelis-node proxy-product` attaches a target-local product
token to a fixed loopback endpoint; no token is sent to the APP. Length-prefixed
frames use bounded closed method/path/header fields and concurrent request IDs.
Canceled queued writes are rejected; cancellation after write admission detaches
the stream, preventing partial frames from being continued or retried. SSH
host-key checking, user pairing and process ownership belong to the native APP.

The host supplies an opaque node ID and Bot ID, a private bounded bearer token,
a receipt journal, and the composed product Service port. Authentication is
native-only; browser origins and redirects are refused. A server generation
changes at startup. All structured turn/item/approval/artifact/reference IDs are
generation-bound HMAC handles. Exact approval fingerprints retain every native
choice and target; interrupt dispatch must atomically validate the native turn.

The closed command union supports submit, decide, exact interrupt, initialize,
retry introduction, load earlier, save draft and explicit stop Bot. Snapshot,
cursor long poll, original receipt lookup and bounded resource bytes are separate
endpoints. An optional typed management port exposes reviewed releases, managed
installation status, install/update with an original receipt, and the existing
Host main/Team configuration controller. Capabilities explicitly report when the
port or native configuration receipt lookup is unavailable. Reads that happen to
match a requested setting do not confirm a lost original configuration receipt.
The port accepts no credentials, shell command, arbitrary URL or destination path.
There is no generic method invocation, raw ToolConnection, provider token,
terminal, native session navigation, multi-client merge or offline outbox.

Business commands persist their stable ID and digest before dispatch. HTTP/SSH
disconnect cancels observation only. A lost response is unknown and is recovered
by querying the original ID; restart never replays an uncertain command. Journal
payloads contain receipt metadata, not prompts, answers or drafts. Duplicate
commands return receipt metadata; callers refresh State for current product data.
The journal permits 4096 business commands and then rejects new admission; no
entry is silently evicted. The native supervisor remains the shutdown authority.
Draft replacement uses the existing persistent native revision CAS and is
reconciled by reading State, without consuming business receipts.

Limits: JSON command 512 KiB, snapshot 8 MiB, user text 256 KiB, eight input files,
64 references, resource 20 MiB. Resource callbacks must enforce Bot/task ownership
and return size/hash metadata. Bytes are verified before publication; client
paths cannot select server files. Unsupported artifacts return unavailable.
Issued uploads use generation-bound handles; unmatched native resource IDs are
refused. The private upload catalog is limited to 256 files and 512 MiB, and rejects
linked/public roots, catalogs and files. A native host resolves issued upload IDs;
composed Worker artifact downloads use `Service.ReadProductArtifact` ownership.

Closing the native client, observer socket or listener never stops the Bot.
Explicit stop fences later commands/uploads and calls only the host-owned Bot
shutdown. Signal/supervisor shutdown is a separate native owner boundary.

State carries at most 512 watchlisted task summaries with generation-scoped
opaque IDs. Native terminal task facts and the original request outcome remain
separate, so an interrupted task can retain an unknown original cancel receipt.
Task summaries are presentation data, not a new task/session navigation API.

Fixture tests cover authentication/schema bounds, projection and stale approval,
response loss/detach, restart/reset/unknown intent, stable-ID conflicts, draft CAS,
receipt privacy, exact interrupt admission and resource integrity. They do not
establish remote SSH, native Linux runtime, model, renderer or distribution
acceptance. Bot skill guidance is unchanged: this transport preserves existing
Bot capabilities and native authority; it adds no model-facing tools/workflow.

## Explicit headless owner

`caelis-node serve-bot --profile ABS --listen 127.0.0.1:0 --auth-file PRIVATE`
assembles the existing `app.New`, `PreparePersonal`, `Start` and `Close` services.
The explicit private profile owns `runtime.json`, execution settings, identity,
Notebook, schedules and receipts. A profile lock prevents two native owners.
This executable supports the private `--bot-tools` bridge used by both adapters;
it imports no Wails, renderer assets or desktop application assembly.

The application-local product token is a bounded private regular file supplied
by the native owner, scoped to this product listener/profile. It is unrelated to
model credentials and Core Host tokens. No credential discovery, copying,
automatic remote configuration or persistent service installation occurs.
Startup emits only endpoint and opaque identity. Explicit RPC stop publishes and
flushes its receipt, then exits the owner and releases its listener; SIGTERM also
ends the native process. A restarted owner can read the original persisted receipt.
Observer EOF, stdin closure and client detachment never stop the resident owner.

`caelis-node proxy-product --endpoint http://127.0.0.1:PORT --auth-file PRIVATE`
is the target-local forwarding helper. Its stdin/stdout carries only framed
product traffic. It cannot forward arbitrary URLs or attach native bindings.
The application owner must provide the target's existing runtime configuration
and credentials through its normal native/human setup. Headless fixtures and
cross-compilation do not establish actual Linux runtime/model acceptance.

## Optional native management

`serve-bot --runtime-directory ABS` binds the reviewed Linux installer to an
explicit private directory inside the target user's HOME. It advertises typed
management only after that native factory succeeds; omission preserves the
existing default owner path. No installation or credential setup runs on startup.
The configuration capability uses only the existing Caelis shared Host model,
role, team and removal settings. Bot conversation preferences and connection,
account, OAuth, API-key or custom URL setup have no commands on this transport.

Native clients read management capabilities, reviewed releases, runtime status
and public configuration through fixed endpoints. Installation/configuration
mutations use the same durable original command ID/digest reservation and owner
context as product actions. Native `committed` maps to outer `accepted`,
`conflicted` to outer `rejected`; the exact native outcome remains in the typed
receipt. Native operation IDs remain target-local. Configuration receipts are
queried through the original outer product ID, never inferred from current model
values; `configurationReceiptLookup=false` explicitly denies native lookup.

Installation `resolve` is a separate lookup of the original durable intent, with
matching runtime/version/expectedVersion. It cannot create a missing command or
retry install/update. While the original operation is pending it returns unknown.
Known outer outcomes return the existing receipt. A completed unknown intent can
query its original native installer receipt in the same service generation; an
unknown earlier generation remains unknown because that native scoped ID cannot
be adopted by a restarted service. No automatic retry or new-ID replacement runs.
