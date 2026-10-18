# Onboarding preparation and performance

Model confirmation, optional features and the introduction share one setup lifecycle. Preparing a runtime must not blank an already usable page or discard the user's choices.

## Current behavior

- Read saved model choices before slow runtime discovery; show a loading state until the actual catalog is available.
- Reuse a still-valid preparation snapshot for the same runtime, executable, configuration and authenticated model. Invalidate it after a relevant change or expiry.
- Prepare the resident connection while the user edits optional features and the introduction. Starting the first conversation still requires confirmed readiness.
- Retain the original activation/configuration receipt after an uncertain result. Returning to an earlier step or choosing another runtime invalidates stale UI completions, not the original operation's authority.
- Keep model selection responsive while its desired value is applied. Temporary catalog failure does not erase an explicit choice.

## Measure changes

Use a fresh isolated profile for first-run tests and an existing authenticated test profile for return-navigation tests. Measure both cold and warm paths, keeping backend phase duration separate from click-to-paint timing. Record the source, executable, OS, model/runtime configuration and cache state without logging credentials.

For Windows runtime inspection, set `CAELIS_BOT_PERF_PROFILE` to the disposable profile and run:

```powershell
go test ./internal/app -run '^TestWindowsSetupInspectionTiming$' -v -count=1
```

This opt-in test probes local runtimes; it is not part of ordinary CI. Inspect the native UI separately for loading, Back/Next, failed activation, unchanged model reuse and first-message admission. A short local timing sample is not a latency guarantee across machines.

Use [interface design](ui-design.md#first-run-choices-and-progress) for presentation and [development](development.md#isolated-first-run-review) for isolation. Synthetic-provider tests, native navigation and real-account inference are separate scopes.
