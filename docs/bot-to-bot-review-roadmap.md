> URL form checkpoint: the bounded original-request HTTP form path now has a real
> Codex/Mock-MCP A/B/React journey on clean exact SHA `de4b791c` (tree `cc9e1111`)
> with native continuation and at-most-once counts.
> See [scope and executable checks](worker-peer-url-forms.md). This does not close
> final-head account/GUI and final review gates. Named B-only browser flows are
> explicitly deferred for the first release; see the URL support decision.

# Bot-to-Bot convergence and acceptance

Draft [#221](https://github.com/caelis-labs/caelis-bot/pull/221), related to [#217](https://github.com/caelis-labs/caelis-bot/issues/217). Review origin: `790cf625b8b6e8ce1677931e2e18218d436b8034`. Delivered application checkpoint: `78fbaaa273cf87739f00e8d078fad97b147f6394`; its CI39940207811 completed successfully on every selected platform, including Windows. Core pin checkpoint: `b7629d1700ecd43d64c8e757d252b05d425aeeb0` (v0.73.2 / `c882776c01c6116f1918d796c54ed66dccebc61a`). Official Bot main remains `28d2394b17dcd1c6fd465e6f9036ca3d1b831691`. The 13:37 review remains an input to independently reproduced repairs, not the current implementation state.

## R0: scope and authority

The contract is equivalent local/remote behavior for the **same Runtime and configuration**. A owns product tasks, output, completion, questions and user approval UI. B owns isolated native execution and original receipts. B foreign work never enters B resident chat, normal tasks, notifications or Agent context. Connectivity is external and never authorization.

| Native fact | Required behavior |
| --- | --- |
| Network/App connection lost; original owner and request still valid | Reconnect the original binding; restore the original request at A. |
| Original owner/request confirmed invalid | Keep task/result and show accurate invalidation; never send its old decision to a replacement owner. |
| Earlier effect still unknown | Keep original operation ID/digest/receipt and reconcile it; never replay, change owner or substitute a task. This can coexist with request invalidation. |
| Confirmed private owner exit; user starts new work | Existing native lifecycle can prepare a new owner for new work; old tasks do not migrate. |

This supersedes the former unconditional requirement to revive all native continuations. Core's existing `interrupted` / `startup_recovery` facts must be consumed. No new execution/approval authority, continuation resurrection system, scheduler, discovery, SSH/Tailscale management or general upload session is in scope.

| Capability | Codex | Caelis | Evidence required |
| --- | --- | --- | --- |
| Create/continue/guarded stop, output/history/files | Same shared adapters and original routes | Same shared adapters and original routes | Actual A/B entry, native receipt and byte verification. |
| Existing ordinary/sensitive/native option/opaque interactions | All existing local kinds return to A; Agent remains limited | Existing local permissions/questions/options return to A | Original IDs/options/scope reach B and native execution continues. |
| Native URL challenge within the first-release scope | Reachable normal-browser URLs and supported static loopback forms close through A; original native continuation required. Named B-only browser flows are explicitly deferred, not repaired | Match existing local support; no upstream general form/URL expansion for this PR | Original native owner confirms the challenge and continues. Opening a page alone is insufficient. |
| Surviving-owner reconnect/App restart | Restore original state and valid requests | Restore original state and valid requests | Fault injection with original binding/receipt readback. |
| Native exit/startup recovery | Accurate request invalidation plus retained unknown effects | Consume existing native invalidation facts | No old decision reaches a new owner; new work remains usable. |

## Reading and retention responsibilities

Environment health answers whether the environment can accept new work. Complete task observation owns current task and pending interaction completeness. SSE improves timeliness within its budget; it cannot be the only way an approval arrives. Native history remains authoritative history. A bounded presentation cache retains received content and never invents execution or decision outcomes. Old output is removed only with a reliable native replacement, never because a new projection merely exists.

Pairing checks B's selected existing configuration without keeping a fixed empty readiness owner. Source-isolated native owners are prepared at actual connection/work boundaries. Input preflight uses actual Runtime limits and metadata; dispatch sends bytes once and validates their digests. Files join existing storage accounting/cleanup with protected references. Ordinary UI shows environments/status/connections/necessary source grants; network diagnostics live in details with one relevant recovery action.

## R1: independent findings and convergence

Each finding needs a failing baseline test and a passing repaired test. No string-based guess may turn transport uncertainty into rejection.

| ID | Review finding | State / evidence |
| --- | --- | --- |
| R2-1 P1 | Deterministic Web pairing/listener rejection persists as unconfirmed and blocks corrected input | Code repair committed in `ebdff02e`; real client/handler regression and 4f186371 application wrong-code→correction journey pass. Genuine response loss keeps the original operation/proof. |
| R2-2 P2 | Sixteen SSE slots determine approval visibility; historical questions retain subscriptions | Code repair committed in `4ce45cc`; 17 active approvals, 16 completed question histories plus one active task, and failed SSE regressions pass. The 16-stream budget remains independent of full-observation visibility. |
| R2-3 P2 | Definitely rejected first pairing leaves an undeletable unauthorised profile | Code repair committed in `9c1b9e4b`; definitely refused no-binding profiles can be deleted, while uncertain attempts preserve original proof/token across reopen. No new removal state machine. |
| R2-4 P2 | Old activity crowds out latest Caelis text; empty projection hides legacy text | Code repair committed in `ebdff02e`; failing-before/passing-after coverage retains newest text, legacy Items and content when native history is unreadable. |
| R2-5 P2 | Safe cache basename becomes download/preview name | Code repair committed in `ebdff02e`; real cache/resolver/authenticated HTTP preserve original names over safe paths. 4f186371 application preview/download checks actual bytes and names; explicit desktop SaveArtifactAs was retained. |

Previous F1–F8 remain regression gates: private owner new-work recovery, unknown capacity, control/prompt receipt quotas, preparation/proof preservation, configuration-fenced subscriptions, null reader recovery, original-owner URL validation and stable source labels. Their existing repository fixtures are retained; passing them is not a substitute for the application journeys below.

## R2: reproducible actual-application E2E

The repository entry `make e2e-worker-peer` runs actual isolated A/B applications, real HTTP/React/browser UI, Bot MCP and persisted product state. The controlled runner replaces only the native protocol, not the product API. The 4f186371 journey covers CSV/PDF/PNG create/continue, ordinary/opaque interactions, Agent file verification, preview/download, B offline copies, create-response loss and upload-period UI Stop. The historical 78fbaaa URL probe confirmed a remote gap. The clean de4b791c six-case URL journey now adds real Codex/Mock MCP and actual A/B/React HTTP-form completion. It does not establish account/physical-machine/native-GUI acceptance. Evidence identifies SHA, platform, native build, steps/assertions and sanitized original-operation effect counts. Cleanup owns only its processes/listeners/data.

| Journey | Required assertions |
| --- | --- |
| Wrong address/code/expired code, correction, deletion and re-add | Draft input remains, corrected attempt actually reaches the backend; no stuck failure row; unknown proof is preserved. |
| CSV plus PDF/image create and original-task continuation | Original names/bytes/digests and B binding; limits reject without fake execution. |
| Native ordinary/sensitive/opaque/options/URL | A makes decisions; original request and choices reach original native owner; native confirmation and continued execution. |
| Historical questions, long tool activity, history failure | Every new approval appears without opening task details; newest/received output survives transitions and read errors. |
| Agent result verification, preview/download/save, B offline | Agent reads actual bytes, names stay original, retrieved copies stay usable; cached differs from refreshed. |
| Pair/create/send/stop/decision response loss | Query original operations; effects at most once; same ID/different content rejected; no uncertain replay. |
| Network/address/App/native changes | Original routes survive; stale results are fenced; invalid requests stay invalid; new work does not migrate old work. |
| Two A sources with equal native IDs; revoke/reauthorize/archive | No cross-source task/file/history/decision access; revoke never means stop/approve; B product remains isolated. |
| Upgrade cursors, local and legacy helper | Retain original results/routes/proof; no silent migration or local regression. |

## R3: fixed-SHA candidate and representative acceptance

Core v0.73.2 preserves the exact public schema/wire. The released native CLI suite passes Worker/resource/approval/restart/image paths, and the released 0.73.1→0.73.2 interop suite preserves an active old Session, Bot identity and callback receipts without replay. These use isolated Stores and synthetic model endpoints; they are not Bot GUI/account or cross-machine acceptance. Runtime/protocol pins now share one source check. No release archive was downloaded; existing cache bytes match current official Release API/checksums.

The [original-request HTTP form path](worker-peer-url-forms.md) is implemented.
Clean same-SHA `de4b791c` passes actual Apps/React with real Codex 0.162.1 and
isolated Mock MCP/local model, retaining original IDs and independent web/native
confirmation/continuation counters. Decline/expiry/owner exit have no success;
unknown effects do not replay. CI39983328004 passes every platform/product gate.
This is neither third-party OAuth nor arbitrary webpage equivalence.

The bounded independent 34-file review found U1 (original URL receipt blocked by
native Connect/current resident model) and U2 (readonly/disabled-fieldset/template
controls silently rewritten). Each has a failing-before/passing-after regression;
U1 retains source/binding authentication and loads the existing original receipt
without native recovery, while new work retains its normal model/readiness path.
U2 refuses unimplemented browser semantics. The host regression exercises actual
authenticated HTTP, App assembly and saved Codex binding. Review closure is limited
to this slice; a final whole-PR review remains required.

[First-release URL support decision](worker-peer-url-contract.md) retains public
reachable HTTP(S), including scripts/OAuth with reachable callbacks, and the
verified bounded static HTTP form portal. The user explicitly deferred B-only
localhost multistep/GET/JS/redirect/different-form-action pages, public login
callbacks to B-loopback, and B-only certificates/browser state. These are real
remaining limitations, not closed findings. No external-forward opener, automatic
tunnels, general proxy or certificate/credential migration is added. This named
scope decision does not waive other interaction, account, GUI or review gates.

Candidate `8f203370` now also has two separate real-Runtime acceptance records:
macOS **headless** A → Fedora x86_64 headless B with actual React and real Codex
0.162.1 on both ends (controlled model/Mock MCP), original task/request identities,
web/native/continuation effects each once and original A task completed; and macOS
native Dev GUI → released Core v0.73.2 with a controlled model, one completed chat
exchange, close/reopen and App restart retaining output/native owner with exactly
one model request. These do not combine into a native-GUI remote-Worker journey,
real-account journey or the full fault/interaction matrix. Only owned temporary
services/forwards were used; Fedora's daily launcher PID remained unchanged.

CI39987269322 at this candidate passed macOS/Linux but failed Windows in
`TestNewPreparationRecoversOnlyNewWorkAndKeepsOriginalReceipt`, initial start,
2.35 seconds / `context deadline exceeded`. Workerpeer's package took 5.228 seconds;
Codex/chatlog/eventplan passed. The specific original `peers.json` durable-write
stage is not logged. The next diagnostic records the **original full-suite**
workerpeer process with opt-in test-only Go trace, per-test JSON times, syscall and
synchronization profiles plus a named original-start region. No second test attempt,
production persistence change, relaxed assertion, 2-second/180-second change or
new durability authority. Sampling failure is reported unavailable; the original
full-suite result is preserved. A trace profile is not Windows ACL/driver/error
attribution, nor proof a timed-out writer finished after the trace ended.

Minimum roles: macOS desktop A → Linux headless B; macOS desktop A → macOS desktop B lifecycle; browser → headless A → B. At least one complete real Codex journey and one real Caelis journey, with interaction kinds matched to their existing local support. No Windows desktop expansion belongs in this PR; any advertised Windows role needs its own actual artifact evidence.

Only isolated profiles/listeners may be modified. Existing authorised Fedora SSH can carry an isolated test after exact path/PID/config identification; no daily service/data/tunnel is replaced and credentials are never copied. Missing accounts/environments become specific blockers after all independent work is complete. A fixture is not a real Runtime/account or GUI result.

Merge readiness requires: all five new findings closed with negative/positive evidence; prior eight regressions; exact-source check/smoke/build/CI; application E2E; same-SHA candidate startup and representative real journeys; no local-to-remote degradation in the stated matrix; PR/docs matching evidence. Only then may the draft become ready. Merge and release remain unauthorised. Distribution signing/notarization and production upgrade remain release gates, not permission to modify daily installations.

## Batch/source evidence

Prior implementation commits: `b86c0bda8fa904d75237f518530c4385b9a9ca57`; M0 `7e42ddfe194355a14ce67eddc77ff5dca25139c4`; M1 `701171043eebd3a1f1f1e721b8d874a941ebed04`; M2/partial M3 `107f0c3b9ed6b194934863c77f1c4c2b492e160a`; Windows identity `7d6320fa127b989906a4d5e95a6d926626f94ff7`; strict exit fixture `790cf625b8b6e8ce1677931e2e18218d436b8034`. Their CI/fixture successes remain historical evidence, not closure of this review.

## Final audit checkpoint

Baseline `d04aa56c` CI39994365443 passed scope/macOS/Linux/Windows/product. Its
original Windows full-suite trace records the target recovery test passing in
0.19s; original start region 69.443ms, two Fsync calls (max 25.325ms), two
MoveFileEx calls (max 16.096ms), six SetSecurityInfo calls (max 0.376ms). These
measurements do not explain or repair the older 2.35s failure. Persistence
semantics/deadlines/assertions remain unchanged.

The next independent audit covers the complete official `main` → fixed candidate,
not just the earlier 34-file URL slice. Record every finding, reproduction,
fix/recheck and exact-source validation here. The older physical-machine/GUI
record is at `8f203370`; later changes include offline receipt load validation,
readonly/disabled-optgroup/inert control rejection and test-only CI diagnostics.
They need their own final-head affected-path evidence, not relabelled old evidence.
Q22 isolated account/model input is unanswered; dependent real-account steps remain
blocked without a repeated question. Keep Draft until remaining gates are met.

The whole-PR audit and newly reproduced A1–A3/S1–S2 boundaries are tracked in
[the final audit closure map](worker-peer-final-audit.md). Repairs retain the
existing native binding/receipt authority; no deferred URL flow is counted as a
fixed finding. The no-grant unknown Web pairing trap is covered by explicit
fresh-code authorization on the original profile/proof, with old unknown and
pending references retained, response-loss reconciliation and archive/restart
coverage; it is separate from known bad-code correction. Exact frozen-head checks and remaining account/GUI gates are
reported separately from the earlier `8f203370` hardware record.
