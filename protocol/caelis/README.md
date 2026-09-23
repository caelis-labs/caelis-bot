# Caelis Control v1 baseline

`manifest.json` pins the public Caelis OpenAPI schema and generated wire declarations
from commit `6ede951f3d383e131cf71b54b3573df401407e38`. This was a local,
unpublished commit at integration time; installed 0.60.1 is not equivalent.

`internal/backend/caelis/wire/control_v1.gen.go` is copied from the public
`control/appserver/wirev1/generated/control_v1.gen.go`, with only its package name
changed to `wire`. Source license: Apache-2.0 (`LICENSE`). No sibling Go import,
module replace or go.work is used. `script/check-caelis-protocol.mjs` checks hashes.
Review the consumed semantics and update both pins intentionally when upgrading.

The adapter preserves accepted/committed/rejected/conflicted/unknown outcomes even
when HTTP status is non-2xx. `SessionState.approval.active.permission` is an opaque
JSON object containing native `tool_call` and options with `id`, not ACP event
`toolCall` / `optionId`. Its native structure is decoded separately in approval.go.

The pinned Host appends Bot configuration as canonical `user_message_chunk`
events with an `event_id` beginning `bot-config-` and no turn/activity identity.
The adapter keeps them out of the chat presentation using that native provenance;
it never classifies user content by its wording. A projection-cache revision forces
old derived transcripts to replay while retaining identity and command journals.
Recheck this source convention when upgrading the pin; it is not a new wire enum.
User chunks with typed `agent_communication_source` are internal context as well,
including Control work-completion evidence. Their assistant responses remain in
the chat; the adapter does not mistake the evidence for a new user request.

Runtime support depends on initialize capability negotiation, not a release
allowlist. See `docs/caelis-integration.md` for mappings, limits and live fixtures.
