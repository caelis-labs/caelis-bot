# Onboarding preparation performance

Measured on Windows on 2026-10-17/18, against the PR #183 working tree based on
`4419f657`. Both runs used separate fresh Bot data, existing local authentication,
the same installed Codex and bundled Caelis v0.72.2, and real native UI navigation.
No model turns were submitted. These are small local samples, not latency targets.

## Results

Except for the explicitly marked screenshot observations, these values are backend
phase durations. They do not include WebView paint or input automation overhead.

| Operation | Before | After |
| --- | ---: | ---: |
| Codex: first model-page preparation | 1,940–3,716 ms | 1,733 ms |
| Codex: reenter model page | 4,808 ms | 1.1 ms, valid preparation snapshot |
| Caelis: first model-page preparation | 2,526 ms | 2,689 ms |
| Caelis: reenter model page | 658 ms | 132 ms cached; 283 ms after expiry |
| Caelis: activate unchanged ready runtime/model again | 1,025 ms | 211 ms, including expired snapshot refresh |
| Codex: activate unchanged ready runtime/model again | no separate baseline sample | 84 ms, including expired snapshot refresh |
| First activation, Codex | 2,572–3,462 ms, blocks Next | 3,228 ms, runs behind later pages |
| First activation, Caelis | 1,283 ms, blocks Next | 1,439 ms, runs behind later pages |

Native screenshots already showed the Extras page **214 ms (Codex)** and
**242 ms (Caelis)** after the model-page Next action started, while native activation
was still running. These are observation upper bounds, not precise click-to-paint
benchmarks. Initial model selection was unchanged; choosing a different model still
waits for its configuration receipt before advancing.

The first-visit improvement is limited. Codex still spends 1,354 ms in its first
`account/read`; Caelis spends 2,419 ms resolving/checking the bundled CLI and its
first `service start`. First-run variability includes process startup, executable
checking and upstream authentication; the smaller Codex sample is not evidence of
a guaranteed cold-start speedup. Full Caelis status observations cost approximately
100–170 ms locally; disabling its optional diagnostics did not materially change
that cost. Snapshot validation is bounded to 500 ms and falls back to inspection.

The important reductions are repeated work and serial waiting. Returning through
setup no longer reconstructs an unchanged resident. Setup and resident connection
share the bundled service preparation receipt: the subsequent `PrepareBundled`
took 1.05 ms with no second `service start` in that preparation interval. After
the interval expires, normal Core reconciliation remains permitted.

## Verification and limits

- `script/check-windows.ps1`: passed, including 141 Node tests and the full Go suite.
- `script/smoke-windows.ps1`: passed, Codex 0.153.4 standard handshake and asset checks.
- Shared application and both backend adapter test binaries cross-compiled for
  macOS arm64 with `CGO_ENABLED=0`; this is not AppKit or macOS live acceptance.
- Windows UI checked both authenticated model paths, centered loading, same-model
  return navigation, concurrent Extras, and the introduction form.
- Regression tests cover first-message waiting, failed activation without replay,
  stale completion after Back/new selection, configuration/file invalidation and
  reuse only of the same ready resident. Native switch/admission/rollback tests pass.
- Temporary phase-only timing instrumentation was removed before the final DevBot
  build. It recorded stage names and durations, never credentials or message bodies.

Local detailed traces live under `.cache/onboarding-perf-20261017` and
`.cache/onboarding-perf-20261018`. The independent acceptance build is
`dist/windows-dev-onboarding-fast-20261018/Launch-DevBot.ps1`; its data directory is
`%LOCALAPPDATA%/Caelis Bot Fast Onboarding Acceptance 20261018`.

This change affects setup preparation and navigation only. It adds no Bot tools or
agent workflow, so no additional Bot skill changes are required for this optimization.
