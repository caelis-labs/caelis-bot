# Caelis Control contract

Bot consumes the single public Core OpenAPI document and its generated Go wire
(package name changed to `wire`). There is no private Application schema, overlay,
registration, separate credential or lease. Bot and TUI use the same Host identity.
No sibling Go import, module replacement or go.work is used. Source license:
Apache-2.0 (`LICENSE`).

`manifest.json` records the exact delivered Core commit and schema/wire hashes.
After integration, update the pin and both hashes together. The protocol checker
validates the artifacts and the Session profile assembly fields.

Runtime admission requires the protocol/envelope versions and consumed capability
names in `internal/backend/caelis/session.go`. Optional model-image and terminal
observation support is negotiated independently. Distribution versions and Host
replacement are lifecycle concerns; Store compatibility is a separate contract.
Pre-unification Application clients are outside the supported protocol baseline.

`POST /sessions` creates either a normal native Session or one with an explicit
profile. Profile selection carries Bot instructions, tools, MCP and Skills; it does
not create another permission principal. Session configuration, resource uploads
and background grants return the same `CommandResult` envelope as other mutations.
`GET /operations/{operation_id}` returns the original receipt, including its typed
payload. A current configuration read cannot substitute for a historical receipt.

The adapter preserves accepted/committed/rejected/conflicted/unknown outcomes,
including non-2xx responses. A lost response is observed by original receipt ID,
never replayed under a new ID. Callback claims keep the original Session, Turn,
item and tool-version provenance. Native approval options remain unchanged.

The v1 Bot-state importer is isolated in `statemigration`; remove it when the
minimum supported saved format reaches v2. Imported receipt addresses match Core
Store v3's isolated importer. The original effect IDs, input source, unknown state
and immutable request bytes are retained. See [integration](../../docs/caelis-integration.md)
for migration, extensions and verification boundaries.
