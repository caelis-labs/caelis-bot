# Worker peer final independent audit

The audit covered official main `28d2394b17dcd1c6fd465e6f9036ca3d1b831691`
through fixed scope checkpoint `733b359c` (plus docs-only `4996a7b3`). Two
independent read-only reviewers split authority/runtime and surfaces/storage;
the integrator reproduced the reported failures, made the following repairs,
and submitted the changed boundaries for independent recheck. This is the whole
PR review, distinct from the earlier 34-file URL review.

## Closure and regression map

| Finding | Repair in the existing authority | Negative/positive regression |
| --- | --- | --- |
| A1 P2: original general receipts hidden behind live Connect/current model | Original authenticated task/source binding reads bypass current preparation; both native readers reject unreadable/partial bindings | `workerpeer.TestAuditOriginalOperationReceiptWithoutNativeConnect`, `app.TestForeignReceiptOwnerLoadIsIndependentOfResidentReadiness`, retained-reader and native binding-load tests |
| A2 P2: remote Stop waits behind the write it needs to cancel | Manager in-flight Codex control and machine route lookup do not wait on network submission locks; peer goes directly to the original guarded native stopper, preserving op ID and native generation checks | `machines.TestRemoteInflightStopReachesOriginalNativeWithoutWriteLocks`: real Manager/routing/authenticated peer with controlled native capacity; original code stops 0 times, repaired code stops once with no new input effect |
| A3 P1: distinct Caelis sources share principal-global native operation keys | New foreign worker freezes a source-qualified random `NativeScope` before any native admission; one helper covers create/prompt/send/stop/decision and original receipt lookup. Original A IDs stay in host intent | `caelis.TestForeignSourcesFreezeDistinctNativeOperationsAndOriginalReceipts`: actual WorkOwner/adapters with shared Core-principal HTTP operation dedup/body conflicts, equal caller IDs/shared workspace, same source through different B bindings, reopen and offline receipts |
| S1 P2: B transport input bytes leak and escape inventory | Per-call independent temporary bytes are removed after synchronous native ownership transfer on accepted/rejected/unknown/cancelled returns. Existing attachment inventory/30-day cleanup covers interrupted transport leftovers under a maintenance fence | `workerpeer.TestAuditFailedStageDoesNotRetainUndeliveredBytes`, filesystem partial failure, concurrent same-ID calls, active cleanup cancellation and all native-return tests |
| S2 P1: accepted pair with lost B→A reply traps Web ingress unknown | Host-captured original profile/proof/target/configuration/pairing-intent references let the existing Web receipt query the machines owner. No pair replay, credential copy or replacement operation | `websurface.TestAuditWebUnknownPairRecoversOriginalProof`: real TS client + Web handler + machines + peer; one pair effect, original receipt settles, later explicit action works |
| S2d: no-grant unknown pair blocks all future Web connection intents | Explicit fresh-code authorization reuses the original profile/source/proof; the existing ingress receipt retains the old unknown and links a separately confirmed authorization for subsequent intents. Salted code fingerprints prevent replay; proof is retained before native admission and pending host invocations become unknown on restart | `websurface.TestUnknownNoGrantExplicitFreshAuthorizationRecoversWeb` normal/pre-B loss/post-B response loss; actual TS client/HTTP owners; archive/restart/source tests; real A/B React recovery |
| S2b/S2c: reconciliation overwrites a newer route or certifies an old unknown from a different newly paired B | Keep original/current peer pins; configuration CAS cannot restore old routes. Explicit PairNamed persists a fresh profile provenance epoch; read-only info does not change it, and a later epoch cannot certify an earlier unknown | `machines.TestAuditOriginalPairReceiptCannotRollBackNewReachableAddress`, same-address B1→B2 replacement and unknown-first-pair tests, forged-proof/revoked-grant/pin tests |

Independent recheck closed A1–A3 and S1–S2 (including S2b/S2c and the in-flight
pairing-epoch race) at the source/targeted-regression layer. This does not close
final exact-source automated gates, or claim live acceptance.

The failure probes use isolated overlays or newly tracked regressions. Native
fixtures replace only the consumed native boundary; they are not real-account or
hardware acceptance. Review/recheck logs and source-qualified candidate evidence
are retained by the task; the Draft PR records exact commit and CI links.

## Compatibility and limits

- New foreign Caelis scopes extend the existing binding, not another task or
  execution ledger. Local/legacy helper derivation is unchanged. Experimental
  older foreign bindings without a frozen scope keep their original receipt
  keys/results; new native writes and automatic start advancement are refused.
  No old task is migrated, replayed or assigned another owner.
- B transport cleanup never deletes A's selected inputs, Codex's native-owned
  `.attachments`, Core resources, native receipts or retrieved A output copies.
  Older Draft-only `sources/*/inputs` directories are conservatively retained;
  no migration or cleanup of existing user data is claimed.
- If the original pair response is unknown and its original proof cannot be
  authenticated (no surviving grant, revoked proof, unknown original target or
  changed explicit pairing intent), the old operation remains unknown and Web
  retains it. A 401 is not proof the old write never happened. Web now offers
  explicit **new authorization** with a newly generated B code, retaining the
  same profile/source/proof. Original unknown receipts and browser pending IDs
  stay intact. A separately confirmed authorization permits later connection
  edits; it never certifies the earlier grant. Owner-certified wrong/expired
  codes are terminal refusals and remain correctable. An uncertain new
  authorization is read by its own original ID, never resent with an edited code.
  Draft-era unknowns lacking a stored original-code fingerprint remain unknown;
  they cannot safely certify a distinct code and are not silently migrated.
  A host interruption between machines persistence and the Web result can leave
  that original invocation unknown even if B accepted; current same-proof fresh
  authorization remains available. A separately confirmed same-proof/current-epoch
  authorization permits managed restart without marking the old pair terminal;
  generic/native unknowns, pending children and mismatched owners still block.
- [First-release URL scope](worker-peer-url-contract.md) retains reachable normal
  browser HTTP(S), including scripts/OAuth with reachable callbacks, and the
  verified bounded static form portal. Named complex B-only browser flows are
  explicitly deferred follow-up, not repaired or secretly included in parity.

## Candidate evidence and remaining gates

Baseline `d04aa56c` CI39994365443 passed every selected platform including Windows.
Scope checkpoint checks passed 303 JS cases, TypeScript/protocol/assets/all-Go;
its product code was unchanged from that baseline. Final candidate check/smoke/
build, affected race and actual A/B browser evidence must name the exact frozen
source and are linked in the PR/task handoff. Pending/cancelled CI is never a pass.

Earlier physical macOS headless A → Fedora headless B and separate native macOS
GUI → released Core v0.73.2 controlled-model records remain at `8f203370`. A
subsequent clean `3bed7bd9` macOS headless A → Fedora B journey used real Codex
0.162.1, isolated Mock MCP/local model and React: original URL task/request/owner,
web/native/continuation effects 1/1/1, B product isolation and no account claim.
Newer candidate records must still name their own source/artifact; none relabels
those earlier runs. Later production differences include offline original receipt
load validation, stricter browser control rejection and the five audit repair
boundaries above; test-only Windows timing diagnostics are separate. Final-head
A/B evidence cannot relabel those old hardware/GUI records.

The latest review scope makes real-account, physical-machine and native-GUI
acceptance **nonblocking follow-up**, not merge gates. Q22 is unanswered; its
dependent real-account steps are still unperformed, with no inferred permission
or credential copying. Do not relabel controlled/native-fixture evidence as
account/GUI acceptance. Ready still requires the fixed-head automated gates and
independent review below. Merge/release remain unauthorized.

## Subscription cancellation follow-up

`3bed7bd9` CI40092637391 passed macOS/Windows but Linux failed
`TestPeerHistoricalQuestionsDoNotStarveOnlyActiveApproval`: server handler max17.
Inspection and independent deterministic regressions found cancellation released
an A subscription slot before its Watch/body/callback lifetime had exited.
Canceled Watches now retain their slot until `done`; observation fallback keeps
running. A resumed same-task/same-connection target also reaps an exited Watch
before replacement, rather than retaining a dead slot. `Observe` joins its two
owned background loops on cancellation; native Workers are not stopped.

The integration assertion still enforces **16**, now measured at the actual
A HTTP request/response-body lifetime, including connection errors and final
Close. Remote canceled handler scheduling is a separate teardown diagnostic;
remote convergence to one active stream remains checked. The new deterministic
cancellation/generation/resumption tests failed before their respective fixes;
17 approvals, 16 historical questions plus one active task and failed-SSE
compensation remain covered. This is a lifecycle repair, not a higher budget,
longer deadline, weakened durability or a generic task-wait change.

The preserved old isolated macOS GUI profile booted under `3bed7bd9` with released
Core0.73.2 and unchanged runtime/initialization/model-configuration identity.
`cua.getApp` twice failed with `Sky Computer Use native pipe startup failed`;
no visual interaction/history/GUI upgrade pass is claimed. This tooling blocker
is independent of missing Q22 account input. Only the owned App/private Core were
stopped and the old profile/history retained. Fixed-head follow-up evidence and
current CI are recorded in the PR; pending or cancelled runs are never success.

## 22:05 follow-up: retained authority and local availability

Review baseline: `0f1fd2cabe50410d9f84ffb15c48d6ed26d9c5da`;
its Product checks CI40094111741 passed all selected platforms. These repairs
extend the same Manager, machines binding and Web ingress journal; no new task,
approval, execution or recovery authority is introduced.

| Finding | Existing-boundary repair | Failing-before / passing-after evidence |
| --- | --- | --- |
| F1 P2 retained read/observe/decision still uses current B resident/default | `workerpeer.Server.owner` derives original-binding-only at the durable binding boundary, not an action whitelist. New preparations alone resolve current admission. | `app.TestBoundPeerReadObserveDecisionIgnoreResidentConfiguration` uses real Application factory, durable source/runtime binding and authenticated HTTP; controlled original native owner serves read/observe/history/decision while resident configuration is offline. |
| F2 P2 ordinary first pair loses recoverable Web reference on host exit; later valid pairing epoch revives historical update blocker | Machines persists original profile/proof/intent before Web admission; Web consumes its captured reference. Historical uncertain grants remain unknown and cannot globally block updates. | `websurface.TestFirstPairHostExitRestoresOriginalWebReceipt`: real TS+HTTP and reopened journal before B grant, after dropped B reply, after machines result but before Web result. `TestUnknownNoGrantExplicitFreshAuthorizationRecoversWeb` covers fresh authorization, revoke, later fresh pair and reload without reactivating old blockers. |
| F3 P2 preparation cleanup holds Service.mu across network | Snapshot route/runtime/connection/reservation generation under lock, remote call outside, CAS only the same undispatched reservation. Preparation itself also releases Service.mu. | `machines.TestHungPeerCleanupDoesNotDelayLocalOrOtherPeerStop`, `TestLatePeerCleanupPreservesDispatchedOrReplacedReservation`: hung cleanup, native stop reachability, late dispatch/replacement and unknown preservation. |
| F4 P2 old foreign Caelis unknown adopts another source's equal operation ID | Shared `recoverOperation` refuses reconciliation without owned NativeScope correlation; known history/results remain readable; local/helper behavior unchanged. | `caelis.TestLegacyForeignUnknownCannotAdoptAnotherSourceReceipt`: explicit read and background recovery collide with another source's accepted receipt, remain unknown and issue no foreign lookup; known/local controls remain intact. |
| I1/I2 local Manager I/O lock and global capacity couple remote faults into local controls | Short global ledger fence; live per-environment admission mutexes; remote I/O outside global fence. Unknown keeps capacity only in its frozen environment. Original stop generation/operation IDs stay authoritative. | `tasks.TestReviewRemoteReadCannotBlockExistingLocalStop`, `TestReviewUnknownRemoteCapacityCannotRejectLocalCreate`; held prepare/start/send/history allow local read/continue/stop/create and another peer, cancellation releases live locks, update pause prevents a new dispatch. |
| I3/I4/I5 shared Web pending scope, historical remote decisions and remote journal quota affect local writes/update | Pending keys use original interaction/profile identity. Host-owned remote routing distinguishes effect certainty from historical quiescence; local and remote have bounded independent admission accounting in the same journal. | Actual TS scoped-pending tests; `websurface.TestReviewUnknownRemoteDecisionCannotGateLocalUpdate`, remote4096-receipt/local-submit regression. No unknown receipt is removed or marked successful. |
| I6 definitely undispatched Web admission burns unused pairing code | Pairing domain CAS-aborts only the prepared intent after ingress save failed before invocation; old uncertain proof and newer intents survive. | `websurface.TestReviewKnownUndispatchedPairCanUseItsStillUnusedCode`, `machines.TestAbortingUndispatchedPairPreservesNewIntentAndOldUnknown`. |
| I7 read-only POST observations unnecessarily retain HTTP write drain | Authenticated fixed `Read` actions release the existing host drain before I/O. Arbitrary paths/caller flags and writes cannot do so. Actual mutable calls still drain within bounded owner deadlines; remote decision uses existing8s control deadline. Remote asynchronous no-file answers use the existing25s Worker admission budget, preserving earlier caller cancellation and local behavior. | `websurface.TestReadAdmissionReleasesOnlyAuthenticatedFixedReads`, headless admission lifecycle coverage; local/native unresolved writes retain their existing guard. |

The independent reviewer reproduced I1–I6 against the then-current candidate
using isolated overlays, not mocked product APIs, and rechecks changed boundaries.
Final-source `make check`, smoke, build, affected race, `make e2e-worker-peer`,
`make e2e-worker-peer-real-url` and all Product checks must pass before Ready.
Exact commit/evidence links belong in PR#221; pending CI is not a pass.

The final deadline probe `app.TestWorkerAsyncAnswerUsesExistingRemoteAdmissionBudget`
reproduced a remote async answer inheriting almost10minutes, then passed with the
existing25s Worker budget. The earlier-parent and local paths remain unchanged;
timeout retains the original answer operation as unknown and repeated reads do
not send another native answer. This is a bounded dispatch fix, not a new budget.

Cancelled environment admission uses the existing `rejectMessage` boundary:
a never-admitted answer is typed rejected so the original question remains
answerable; an already-recorded original request remains unknown. The same
App/Manager test failed before this distinction and verifies no native replay.

## Remote machine settings and interaction-budget follow-up

The settings increment replaces the default sharing dashboard with a compact
mixed Bot/SSH list and a secondary inbound-authorization tab. It restores adding
system SSH aliases through the existing helper. Effective SSH routing/trust stays
with OpenSSH; original task bindings and directional Bot proofs remain unchanged.
See [UI behavior and synthetic native screenshot](remote-machines-ui.md).

Independent negative/positive review additionally found and closed same-profile
SSH pin-publication races, Bot address matching against SSH profiles, and a
certified unsupported-platform refusal leaving a changed public pin/credential.
Per-profile ephemeral serialization and restoration publish only that connection;
network work is outside the shared route lock. Known non-dispatch SSH refusals
use the existing typed rejection/receipt path; true effect uncertainty stays
unknown, scoped to its original profile. Local and another peer control remain
usable while SSH settings calls are held. No new execution or approval authority.

The separate [interaction output-budget repair](interaction-output-budget.md)
filters terminal todo entries, pages pending originals, and supplies lossless
original-ID reads for large requests. It preserves sent/dispatching/unknown,
original offers, receipt lookup and required historical display/de-duplication.
Only redundant terminal closure-index display copies are compacted at save/load.
Neither this repair nor a missing/expired question authorizes any pending request.
The active user Dev instance must be updated through the coordinated normal flow
before the budget repair takes effect; it was not restarted by these checks.

New exact-head automated/CI evidence is reported in PR #221. Old green CI or dirty
candidate evidence is not substituted for those gates. Real accounts and GUI
business journeys remain separately labeled, nonblocking follow-up; bounded URL
exclusions are unchanged. No merge, release or daily-service changes are made.

The final follow-up review also reproduced SSH ProxyCommand children retaining
stderr after their parent was cancelled. Owned local transport groups and a
bounded drain now close that path; the same native probe returns promptly,
children exit and an unrelated sentinel survives. Process-status verification
fails explicitly on permission errors rather than treating empty output as exit.
Bad SSH output is retained within the existing 8 MiB response limit while read.

Windows run `40161104688` at `aa701a88` reached the Codex **package** 180-second
watchdog after 179.30 seconds of completed top-level tests. The named last case
had run only about one second; its cleanup stack was not proof of a 180-second
request or persistence deadlock. The 255-native-decision receipt regression alone
used 56.01 seconds. Windows now runs that complete stress case and the remaining
Codex cases in two disjoint invocations, each retaining the original 180-second
limit. All cases, actual native traffic, durable writes and assertions remain;
no business timeout or persistence guarantee is relaxed. Other packages and the
original workerpeer diagnostic trace retain their previous contract. Final CI
must still pass; this diagnosis is not a substitute for its result.
