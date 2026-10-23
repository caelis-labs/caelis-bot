# Bot-to-Bot review and completion gates

Tracking for Draft [#221](https://github.com/caelis-labs/caelis-bot/pull/221), related to [#217](https://github.com/caelis-labs/caelis-bot/issues/217). Review input: the user-authorized “审查发布路线图” conversation, checked against source and independent reproductions. External conclusions are inputs, not proof. Baseline: `28d2394b` → `b86c0bda`.

The PR stays draft. A owns product tasks/interactions; native adapters own execution and receipts. `workerpeer` stays a transport and source-binding boundary. Declaring a capability false does not complete the first-version goal. No new execution ledger, approval authority, scheduler, discovery/SSH/Tailscale management, task-wait optimization or release work belongs here.

## Findings

| ID | Priority | Trigger and independent evidence | State | Regression / remaining evidence |
| --- | --- | --- | --- | --- |
| F1 | P1 | Fixed foreign private Codex endpoint also gates empty readiness and new work after the original process exits | Fixed; native subprocess and real App-factory fixtures pass, including race | Existing native owner recovery must archive original bindings/unknown receipts and restore only new work, after PID/birth confirmation; shared daemon is separate |
| F2 | P1 | Any Connect error becomes owner-unavailable, so A frees a still-running task's capacity | Reproduced before fix; max-one HTTP/native fixture and race pass | Temporary failure must reserve unknown capacity; only native evidence can prove owner loss |
| F3 | P2 | Approvals consume prompt/total receipt quotas and eventually prevent continue/stop | Reproduced; 255 native approvals then continue/stop pass | Control history must not block stop; original IDs/digests/unknown dedup remain in native receipts |
| F4 | P2 | Failed preparation persists B binding; A ignores failed unpair and discards its only proof | Reproduced; prepare/remove/re-pair and lost-proof regression pass | Undispatched preparation differs from dispatched/unknown; unconfirmed removal retains proof |
| F5 | P2 | Task-keyed streams keep old address; old errors invalidate new connection | Reproduced; address-switch SSE regression and race pass | Cancel/rebuild on connection generation; fence both success and error application |
| F6 | P2 | `items:null` throws before releasing polling guard | Reproduced on original hook; production reader regression passes | Normalize empty arrays, finally-release guard, retry while retaining output |
| F7 | P2 | Web opens Snapshot URL directly, bypassing original owner checks | Reproduced on original transport; shared host/Web tests pass | Shared host validation for desktop/Web; this alone does not finish remote native challenge |
| F8 | P2 | Unordered grants are labeled by positional Bot numbers | Reproduced; named-source metadata/order test passes | Stable source ID plus recognizable name/pair time; deterministic sorting, no device-manager authority |

## Milestones and exit criteria

| Stage | Required exit | Status |
| --- | --- | --- |
| M0 | F1–F8 reproductions pass after fixes; local and remote create/continue/stop/approvals/files/history do not regress. Unknown work reserves capacity. Long approval history cannot prevent control. Owner loss and unreadable history remain distinct facts. | Defects fixed and account-free regression gates pass; real-installation parity remains M4 |
| M1 | B shows actual listener and checked reachable address for A; external network prerequisites are actionable. Accurate unreachable/expired/revoked/incompatible/not-ready guidance names the machine to repair. Rename without pairing, address repair restarts original subscriptions, empty profiles have freshness, revoke is not stop, delete/reauthorize preserves recovery proof. | Open |
| M2 | A's uploaded CSV/PDF/image bytes and original names reach B on create and continue; same-name/count/size boundaries hold. A Agent can read/verify output; user can preview/save; retrieved copies work offline and identify cached versus refreshed bytes. Output survives realtime-to-history transitions and history-read failure. | Open |
| M3 | Ordinary/sensitive/native-option/opaque/URL interactions close through A and B's original native receipt/continued execution. Check current public Core contract first; missing kinds are explicit minimal upstream dependencies, not private protocol extensions. Network reconnect, App restart and native exit have separate recovery paths; lost requests stay unknown, new work has a bounded explicit recovery route. | Open |
| M4 | Representative real installations: macOS A→Linux headless B with real runtime/account/model, macOS A→macOS desktop B, Web A approval/files/output; supported Windows roles need actual artifacts. Bind evidence to SHA/platform/runtime build and visible outcomes. | Not run; real account/model/deployment/system-restart authorization required |
| M5 | Only after M0–M4 exits: candidate install/start/window-close/restart/settings and upgrades preserve tasks/history/pairing/routes/listener; final distribution signing/notarization/staple/Gatekeeper and update acceptance. | Not authorized; no ready/merge/release |

## M4 fault matrix

- Pair/create/continue/stop/decide response loss: query the same original operation and digest; each native effect occurs at most once.
- Network loss while B continues: retain unknown capacity; reconnect to the original task.
- A/B App restart with native owner surviving: restore original tasks/requests/completion notices; do not confuse a vanished native response channel with surviving requests.
- Native exit or machine restart: retain old results/uncertain receipts; explicit recovery admits new work without migrating old execution.
- Address changes, sharing off, revocation/reauthorization: repaired connection is used and stale streams cannot overwrite it.
- Multiple sources/equal native IDs: tasks/files/history/decisions remain isolated and revocation labels identify the source.
- Local and old helper owners: keep original reading/repair behavior.

## Evidence policy

Record each batch's exact commit and test commands/results below. Protocol, native-process, synthetic two-instance, component-browser, real-account and packaged/native-GUI evidence are separate levels. CI success is not full feature acceptance. Do not put credentials, real conversation bodies or machine-specific absolute report paths in PR evidence. Use current repository fixtures/docs and existing dependency caches; avoid release downloads.

### Batch log

- M0 baseline: original source and authorized review independently checked; main still `28d2394b`, PR head still `b86c0bda`, original CI green. Targeted negative reproductions failed before fixes and are retained as regression tests.

## M3 public Core dependency (source-verified, not runtime acceptance)

Official Core main `744b6f421c9b19ac905c654c0e6afae1126bd297` matches Bot's pinned
OpenAPI byte-for-byte (generated wire differs only in package name). A pin refresh
will not supply the missing interaction kinds.

- Native option resolution already carries original request ID, full TurnTarget,
  option ID and operation ID. Reuse [ResolveApproval](https://github.com/caelis-labs/caelis/blob/744b6f421c9b19ac905c654c0e6afae1126bd297/control/appserver/command.go#L215).
- Existing [approval subjects](https://github.com/caelis-labs/caelis/blob/744b6f421c9b19ac905c654c0e6afae1126bd297/docs/acp-projection-architecture.md#L115)
  include redacted sensitive/opaque context. Bot still needs to consume this
  public projection fully; it must not reconstruct hidden values.
- Generic question/form answers and URL challenges have no native public
  request/response owner in this pin. MCP [elicitation is not installed](https://github.com/caelis-labs/caelis/blob/744b6f421c9b19ac905c654c0e6afae1126bd297/agent-sdk/tool/mcp/client.go#L66).
  The minimal upstream dependency is typed live/pending interactions under the
  existing approval owner, original targets and original operation receipts,
  including native validation and continuation. Do not encode answers in reason
  or metadata as a private compatibility protocol.
- A persisted mirror is not an executable continuation. Core's [live attach](https://github.com/caelis-labs/caelis/blob/744b6f421c9b19ac905c654c0e6afae1126bd297/agent-sdk/runtime/durable_approval.go#L65)
  is process-local; [startup recovery](https://github.com/caelis-labs/caelis/blob/744b6f421c9b19ac905c654c0e6afae1126bd297/control/appserver/approval_recovery.go#L13)
  closes pending mirrors whose live waiter was lost. Cross-native-restart answer
  parity needs a proven native continuation contract, not another Bot ledger.

These are source findings, not a new Core runtime/real-model fixture or an
upstream implementation/release. M3 remains open.

### M0 implementation batch

- F1: explicit new-work recovery reuses native lost-owner archives and verifies
  private PID/birth. Normal reconnect never replaces an owner. The production App
  factory is exercised with a real controlled Unix-WebSocket child and endpoint
  cache; no model-serving methods exist in that fixture.
- F2/F4/F5: [Manager + HTTP/SSE regressions](../internal/machines/peer_m0_test.go)
  reproduce max-one capacity, failed preparation/proof retention and address
  replacement. Both ordinary and race runs pass.
- F3: [native receipt-limit fixture](../internal/backend/codex/work_receipt_limits_test.go)
  executes 255 original approval decisions, continues after 100, and observes one
  guarded interrupt. Native ID/digest dedup and unknown receipts remain intact.
- F6/F7: production output-reader and actual Web-transport tests cover null items,
  released polling guards, retained text/history and host-refused/validated URLs.
  Both are part of `make check`. Host validation is shared with desktop.
- F8: named grants preserve source identity and pairing time across state reads,
  with deterministic sorting. Position and display name never authorize a source.

Validation: `make check` passed (297 Node tests, TypeScript/Vite/i18n/generated
contracts and all Go packages); `make smoke` initialized the installed native CLI
without creating a conversation or model request and checked offline assets.
`go test -race -count=1 ./internal/workerpeer ./internal/machines` passed, plus
focused native Codex receipt/recovery and App-factory race tests. macOS
`production,private_mac_apis`, Linux amd64 headless and Windows amd64 CGO-free
compilation passed. Full packaging, release dependency staging, real accounts,
real cross-machine execution and native GUI acceptance were not run. The build
checks reuse existing dependencies and download no release payloads.
