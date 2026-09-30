# Remote Bot model settings

The optional product execution port exposes the connected Bot's conversation model/effort,
its current public model catalog and an optional work model override. It uses the existing
backend execution providers and persistence callbacks, including Codex's native model catalog;
it is independent of Host configuration and `--runtime-directory`. Binding the port observes
an already composed provider and never starts or changes a Runtime. Local settings keep their
existing APIs and behavior.

`Identity.capabilities.execution` and management `capabilities.execution` advertise this port.
The scoped `POST /v1/management/execution` returns a typed `ExecutionView`; its `revision` is a
SHA256 digest of the current conversation/work settings, including preserved policy fields.
`conversationDefault` advertises the native provider's existing inheritance choice. Work is
absent when the provider does not support a separate override. Catalog entries contain only
the existing public model DTO, never extensible provider configuration or credential objects.

The closed `configure-execution` command contains the inspected Bot/generation, a stable ID,
target `conversation` or `work`, the expected revision and only `{model, effort}`. Comparing
the revision, reading current preferences and saving the change share the backend settings
lock with local saves. Model/effort must satisfy the current catalog and native policy.
Conversation changes preserve approval mode and service tier. Work changes preserve tier;
explicit empty work selection clears the existing whole override to inherit defaults.
Empty conversation selection requires advertised inheritance and an empty preserved tier.
No permissions, service-tier controls, paths, login, API keys or generic native RPC are exposed.

The product journal durably reserves the original ID/digest before dispatch. It stores only
the digest, scoped outcome and settings category, without the model selection or catalog.
Repeated IDs return their original receipt; changing the intent under the same ID is rejected.
An observer cancellation detaches observation while the admitted operation continues.
Original receipts retain their generation across service restart and cannot authorize a new
settings dispatch. Unknown effects, interrupted pending receipts or failed receipt publication
fence fresh settings mutations. Reading matching current values cannot clear that fence.
The existing generic execution-provider error interface cannot prove every failed save had
no native effect; such outcomes remain unknown without replay, including across restart.
Shape/catalog/read failures and revision conflicts detected before the mutation are rejected.

Regressions cover concurrent revision updates, local save serialization, preserved policy,
work inheritance, malformed/stale wire input, stdio transport, canceled observation, durable
restart fencing and a real Codex adapter protocol fixture. They use temporary profiles and
synthetic native responses; they do not establish GUI or authenticated live model acceptance.
