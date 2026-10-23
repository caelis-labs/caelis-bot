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
| M1 | B shows actual listener and checked reachable address for A; external network prerequisites are actionable. Accurate unreachable/expired/revoked/incompatible/not-ready guidance names the machine to repair. Rename without pairing, address repair restarts original subscriptions, empty profiles have freshness, revoke is not stop, delete/reauthorize preserves recovery proof. | Implemented and focused/race fixtures pass; native product UX remains M4 |
| M2 | A's uploaded CSV/PDF/image bytes and original names reach B on create and continue; same-name/count/size boundaries hold. A Agent can read/verify output; user can preview/save; retrieved copies work offline and identify cached versus refreshed bytes. Output survives realtime-to-history transitions and history-read failure. | Implemented with account-free byte/HTTP/native-adapter/reader fixtures; packaged UX and real Runtime/model use remain M4 |
| M3 | Ordinary/sensitive/native-option/opaque/URL interactions close through A and B's original native receipt/continued execution. Check current public Core contract first; missing kinds are explicit minimal upstream dependencies, not private protocol extensions. Network reconnect, App restart and native exit have separate recovery paths; lost requests stay unknown, new work has a bounded explicit recovery route. | Partly implemented: public native approval subjects/choices are preserved. B-local URL continuation, generic Core form/URL and native pending restart remain blocked/open; capability flags do not satisfy this exit |
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

Official Core main was rechecked at `c882776c01c6116f1918d796c54ed66dccebc61a`.
Its public OpenAPI is byte-for-byte identical to Bot's pinned `744b6f4` schema;
the generated wire differs only in package name. The intervening changes do not
add an interaction/continuation API. Refreshing the pin alone will not close M3.

- [ResolveApproval](https://github.com/caelis-labs/caelis/blob/c882776c01c6116f1918d796c54ed66dccebc61a/control/appserver/command.go#L215)
  already carries original request ID, complete TurnTarget, native option ID and
  operation ID. Bot now consumes the public Subject (title/description/operation,
  targets, grants, sensitivity/opacity/redaction states) without reconstructing
  hidden input. Native permission bytes and decision eligibility are unchanged.
- Generic question/form answers and URL challenges lack a public native
  request/response owner. The [Codex bridge](https://github.com/caelis-labs/caelis/blob/c882776c01c6116f1918d796c54ed66dccebc61a/adapters/codex/mcp_approval.go#L11)
  handles only the supported empty form; general form/URL is cancelled. Core's
  [MCP client](https://github.com/caelis-labs/caelis/blob/c882776c01c6116f1918d796c54ed66dccebc61a/agent-sdk/tool/mcp/client.go#L72)
  does not install elicitation handling. The minimum upstream work is typed
  live/pending questions/forms/URL challenges, original targets/options/operation
  receipts and native validation/continuation under the existing approval owner.
  Answers must not be encoded into reason or metadata as a private protocol.
- A saved approval mirror is not an executable continuation. Core's
  [live waiter](https://github.com/caelis-labs/caelis/blob/c882776c01c6116f1918d796c54ed66dccebc61a/agent-sdk/runtime/durable_approval.go#L306)
  is process-local; [startup recovery](https://github.com/caelis-labs/caelis/blob/c882776c01c6116f1918d796c54ed66dccebc61a/control/appserver/approval_recovery.go#L349)
  closes a pending mirror after losing that waiter. Native restart parity needs
  a provable native continuation, not a second Bot ledger. Codex's vanished
  inbound response channel is likewise retained as unknown, never recreated.
- A reachable Codex URL can close the original native request in the protocol
  fixture. A B-local URL is still blocked. Opening B's localhost on A, rewriting
  arbitrary callback URLs or relaying generic browser cookies is not a working
  challenge. A portable, owner-confirmed challenge path remains implementation
  work; the host URL-check fix and `url=false` do not finish it.

These are current source findings, not real-model acceptance, upstream changes
or a release request. M3 remains open, and the PR cannot become ready on this basis.

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

### M1 connection-maintenance batch

M0 predecessor: `7e42ddfe194355a14ce67eddc77ff5dca25139c4` (all F1–F8 fixes).

- Separate actual B listener and published A address. Desktop supports an explicitly
  configured reachable bind, with bind-before-replace and conflict preservation;
  headless only publishes an address and retains its existing listener. Tests cover
  port persistence/reopen and mandatory authentication after replacing the listener.
- A renames offline; address repair authenticates the original peer without another
  code. Fixed connection diagnostics avoid exposing arbitrary HTTP/native bodies.
  Empty profiles have bounded readiness and timestamped freshness checks.
- Removal with retained tasks archives the environment for new delegation and keeps
  original proof/routes. The existing panel restores it, including after revocation
  with a fresh code. No new device manager, task transfer or execution authority.

Focused/race fixtures: `TestWorkerEndpoint*`, `TestHeadlessWorkerEndpoint*`,
`TestWorkerListener*`; `internal/machines/peer_m1_test.go` and
`internal/workerpeer/client_m1_test.go`. TypeScript and bilingual catalogs pass.
This is account-free listener/HTTP/native-adapter fixture evidence, not real
cross-machine, packaged desktop or browser GUI acceptance. M4 remains open.

### M2 files/output and partial M3 integration batch

M1 predecessor: `701171043eebd3a1f1f1e721b8d874a941ebed04`. Its separate
exact-commit `make check` passed; the previous “in progress” check is complete.

- A retains user-selected bytes privately before resident dispatch. Opaque upload
  references bind original submission and ordered names/digests, without saving
  chat text or executing work. The existing Agent delegation and Manager input
  resolver carry CSV/PDF/PNG bytes through the same local/peer Worker contract on
  both create and continue. Native limits are checked read-only before recording
  a new product task/message, and again by the original native dispatch boundary.
  A rejected 5-file/9-MiB Caelis input leaves neither a task nor an execution binding.
  Reconciliation of an already recorded operation skips a new admission preflight.
- `bot_tasks file` materializes an owned checked A-local path with SHA-256, byte
  count and freshness metadata; native file/image tools can verify the bytes.
  Immutable retrieved versions remain accessible offline and after App cache
  reopen. Explicit refresh can return the last checked cache on owner failure;
  its cached flag and original fetch time never imply remote-latest content.
  Metadata preserves B's name independently of a portable A cache filename.
- Desktop uses its existing open/preview/save path. Web can preview authenticated
  PDF/raster bytes; HTML/SVG stay downloads. Task/file ownership is checked before
  cached as well as remote reads. No B filesystem path is interpreted on A.
- Both native adapters expose exact live/history message joins. The bounded
  current Worker projection and UI reader retain completed output until that
  native history message is read; history failure never deletes observed output.
  Repeated prose with different native IDs remains distinct. Old projections
  lacking native message evidence are not given fabricated join IDs.
- Caelis public approval Subjects now preserve ordinary/native options and safe
  sensitive/opaque presence through A's existing UI, using original native IDs,
  targets and receipts. Agent permission remains limited; hidden input is not
  reconstructed. This is partial M3, not generic form/URL/restart parity.
- The previous M1 CI exposed an actual `ConfigureWorkerInteractions`/Web snapshot
  data race. The shared host now synchronizes that pointer. The original failing
  log and subsequent race run remain separate evidence. Windows listener conflict
  classification now uses Winsock's actual address-in-use code (10048), not Go's
  synthetic `syscall.EADDRINUSE`. Ambiguous 10013 remains unavailable. The Windows
  fixture inspects the native errno, preserves the old listener and tests exclusive
  binding. [Microsoft documents why 10013 alone cannot prove the cause](https://learn.microsoft.com/en-us/windows/win32/winsock/so-exclusiveaddruse).
  Cross-build success is not Windows execution or GUI proof.

Executable evidence is in `peer_upload_test.go`, `peer_file_validation_test.go`,
`workerfiles/*_test.go`, `tasks/file_copies_test.go`, `bot/task_file_copy_test.go`,
`websurface/artifact_preview_test.go`, both native `work_input_validation_test.go`
and output identity tests, `caelis/approval_subject_test.go`, and the production
`script/worker-output.test.mjs` reader tests. These use synthetic native owners,
real HTTP/SSE routing and bounded bytes; no live user account/model is used.

### Minimum additional M4 environment scope

This is a concrete validation dependency, not authorization to deploy or run models:

1. An isolated macOS A build/profile and Linux B headless build/profile with an
   unused listener and externally prepared reachable path. B's real account/model
   must be configured by its user; do not copy credentials or touch Fedora's current
   service, data or tunnels. Agree a bounded representative model-call budget.
2. A separate macOS B isolated desktop profile for real listener/window-close/App
   restart evidence, and a browser against isolated Web A for file/output/approvals.
3. Fault injection and A/B App/native restart only within those isolated profiles.
   System restart or production service mutation is separate authorization. Windows
   product claims require a real corresponding Windows artifact/host.

Evidence must name the exact commit, platform/runtime build, steps and visible/native
receipt outcomes. No M4 result is inferred from the fixtures, and M5 remains unauthorized.

Integration validation: `make check` passed (299 Node tests, generated contracts,
public/protocol/assets/i18n, TypeScript/Vite and all Go packages). The shared-layer
race command passed for `backend`, `bot`, `tasks`, `workerfiles`, `websurface`,
`workerpeer`, and `machines`, including the previously exposed Web pointer race.
Both native input-validator and output/approval identity focused race suites pass.
`make smoke` initializes the installed CLI without a conversation/model request and
checks offline assets. Native GUI and real-account evidence remain unverified.

Direct compilation passed for macOS `production,private_mac_apis`, Linux amd64
headless and Windows amd64 CGO-free using existing dependencies. Full `make build`
packaging/runtime staging was not run: it can fetch release payloads and is not
needed to establish the changed Worker contracts. Cross-compilation does not
replace Windows/native GUI or installation acceptance.

### Windows CI follow-up

M2/partial-M3 batch: `107f0c3b9ed6b194934863c77f1c4c2b492e160a`.
Its exact local check/smoke/direct builds and remote macOS/Linux checks passed.
Windows exposed a real validation gap in the retained same-size/same-mtime path
replacement test: Go's Windows Lstat defers the file ID lookup until SameFile,
so two path snapshots can resolve to the replacement object. The pre-read identity
now comes from an actual file handle (`File.Stat`), closed before the digest read;
the post-read path must still identify that original object. The original test is
unchanged, not skipped or weakened. Native dispatch remains authoritative.

This is a scoped input validation correction. Source basis:
[Windows Lstat](https://go.dev/src/os/stat_windows.go),
[lazy file ID / SameFile](https://go.dev/src/os/types_windows.go).
The final head's Windows CI must pass before claiming native platform coverage.
