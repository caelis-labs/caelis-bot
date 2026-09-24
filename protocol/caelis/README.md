# Caelis Control v1 baseline

`manifest.json` pins the public Caelis OpenAPI schema and generated wire declarations
at the exact commit recorded in that manifest. The baseline includes shared native
Workers, steering and interactive Host settings. Runtime support requires the
advertised capabilities, including `shared-native-workers-v1` and
`turn-steering-receipts-v1`. Team settings explicitly request
`/agents/binding-status?include=eligible_profile_ids`; the default response remains
compatible with strict clients that predate this extension.

`internal/backend/caelis/wire/control_v1.gen.go` is copied from the public
`control/appserver/wirev1/generated/control_v1.gen.go`, with only its package name
changed to `wire`. Source license: Apache-2.0 (`LICENSE`). No sibling Go import,
module replace or go.work is used. `script/check-caelis-protocol.mjs` checks hashes.
Review the consumed semantics and update both pins intentionally when upgrading.

The adapter preserves accepted/committed/rejected/conflicted/unknown outcomes even
when HTTP status is non-2xx. `SessionState.approval.active.permission` is an opaque
JSON object containing native `tool_call` and options with `id`, not ACP event
`toolCall` / `optionId`. Its native structure is decoded separately in approval.go.

Application configuration uses the generic configuration API, not legacy Bot Mode
messages. User chunks with typed `agent_communication_source` are internal context,
including Control work-completion evidence. Their assistant responses remain in
the chat; the adapter does not mistake the evidence for a new user request.
Native `caelis/error` and failed lifecycle reasons are preserved for the current
request and shown in chat. A projection-cache revision rebuilds derived views
without replacing identity or command journals.

Runtime support depends on initialize capability negotiation, not a release
allowlist. See `docs/caelis-integration.md` for mappings, limits and live fixtures.
