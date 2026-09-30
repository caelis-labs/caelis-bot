# Optional product connection (F2)

The native client connects to one existing resident Bot over a loopback HTTP
listener or a private Unix listener forwarded by separately owned SSH. This
package never selects a Runtime, creates a local Bot, transfers native bindings,
or reads credentials. Default desktop assembly remains local.

The host supplies an opaque node ID and Bot ID, a private bounded bearer token,
a receipt journal, and the composed product Service port. Authentication is
native-only; browser origins and redirects are refused. A server generation
changes at startup. All structured turn/item/approval/artifact/reference IDs are
generation-bound HMAC handles. Exact approval fingerprints retain every native
choice and target; interrupt dispatch must atomically validate the native turn.

The closed command union supports submit, decide, exact interrupt, initialize,
retry introduction, load earlier, save draft and explicit stop Bot. Snapshot,
cursor long poll, original receipt lookup and bounded resource bytes are separate
endpoints. Runtime management is an optional future port and is not advertised.
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

Closing the native client, observer socket or listener never stops the Bot.
Explicit stop fences later commands/uploads and calls only the host-owned Bot
shutdown. Signal/supervisor shutdown is a separate native owner boundary.

Fixture tests cover authentication/schema bounds, projection and stale approval,
response loss/detach, restart/reset/unknown intent, stable-ID conflicts, draft CAS,
receipt privacy, exact interrupt admission and resource integrity. They do not
establish remote SSH, native Linux runtime, model, renderer or distribution
acceptance. Bot skill guidance is unchanged: this transport preserves existing
Bot capabilities and native authority; it adds no model-facing tools/workflow.
