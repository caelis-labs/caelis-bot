# Consumed Codex native schema

These 56 schema files are unchanged output from **Codex CLI 0.153.4**:

```sh
codex app-server generate-json-schema --out .cache/codex-json-schema/0.153.4
```

The original initialize/account files come from stable generation. Other consumed
files use `--experimental`; `manifest.json.experimentalFiles` records their source.
Both generations are required because experimental fields can change existing types.
`manifest.json` records the byte hashes; `go test` checks
them against the Go adapter's TestedVersion and `toolchain.json`. `make schema` also regenerates
from the installed pinned binary and compares the consumed files. Version upgrades
require explicit schema diff review, projection changes and lifecycle/contract tests;
do not update only the version or hashes to bypass a mismatch.

This pin is only a reproducible schema/test baseline. Runtime connections do not
check CLI release numbers. Both stdio and Unix sockets use the standard handshake
and validate the wire fields actually consumed by the adapter. App Server does not
currently negotiate a protocolVersion; schema directories named v1/v2 are not a
negotiated version range. See [compatibility policy](../../../../docs/codex-compatibility.md).

These are native wire schemas, not a second frontend contract. The Go client projects
initialize, auth, threads, turns, items, input, approvals and cleanup. In the JSON schema `account` may be omitted
(equivalent to null), even though the generated TypeScript represents it as required
and nullable. Auth projection omits email, plan, identifiers, paths and credentials.

Wire envelope is newline-delimited JSON, without a `jsonrpc` header. Request IDs are
native numbers/strings. Go tests cover exact server request targets and native errors.
The separate host contract lives in internal/backend/api and generates the UI types.
Unknown native methods are explicitly rejected; enabling experimentalApi is not blanket support.

The two experimental ItemGuardianApprovalReview notifications map the wire methods
item/autoApprovalReview/started and /completed. A native review status is a display
fact, not a client approval request; timeout/denial never creates an Allow option.

Reference: [official App Server documentation](https://developers.openai.com/codex/app-server/).
The local pinned schemas take precedence over examples for newer versions.

ThreadTurnsList params/response cover optional descending full-item history pages.
Unsupported optional pagination falls back to legacy resume, without a CLI version gate.
