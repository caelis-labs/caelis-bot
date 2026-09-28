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

`application-model-capabilities-v1` is optional and gates Ask Bot only. Its scoped
`GET /application/sessions/{session_id}/model-capabilities` observes the current
desired application model and decimal-string configuration revision. An explicit
`image_input: true` enables image input; false or omission disables it. Neither
the immutable creation profile nor Host-wide union capabilities can substitute
for this observation. Older Hosts continue ordinary workflows without this read.

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

`execution-configuration-v1` is required for Bot's native execution contract. The
profile sends `execution_config.environment.inherit: true` and
`execution_config.shell.login: false`. No user environment values or credentials
are copied into the profile. Core's normal Worker defaults have the same behavior.
Existing Session profiles and pending creation bytes remain authoritative on resume;
missing configuration uses the corrected Core defaults. Configuration is not hot
patched during tool rebind. A version upgrade creates a replacement context with current
host assembly defaults after a verified handoff; same-version renewal retains them. See the pinned Core
[execution contract](https://github.com/caelis-labs/caelis/blob/bdebd8d2d4bfe6b1bec455fca0f19da49b673c8f/docs/execution-configuration.md).

`application-guardian-review-v1` is required. New Bot Runtime profiles explicitly
select `auto-review` and a pinned `guardian` model, then verify scoped reviewer
readiness. Callback policy is `required` except for product-owned direct tools.
Approval review observations never confer execution or manual-resolution authority.
See [acceptance and release conditions](../../docs/caelis-integration.md),
including the Runtime item identity repair in Core #91.
