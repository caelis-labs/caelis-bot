# Draft upstream issue: Chrome checkbox advertises `set_checked`, but rc.2 does not change it

**Status:** local draft only. Do not publish without separate authorization.

## Environment

- macOS 27.0.1 (26A434), arm64
- Chrome 154.0.8037.98, launched with a fresh temporary profile and one synthetic local page
- Desktop World `v0.1.0-rc.2`, revision `e7b53a1812fe3892cf8e4b903479d208dfaa0104`
- Published Darwin arm64 helper archive SHA-256: `7ff6a44ec7cd8cba63f6f799a3729141d384afd8d2e5154dee69cb9d546e95cc`
- Helper hello: `desktop-world/helper-v0.1`; `input_mode=cooperative`, `input_policy=shared_input`. Accessibility/Input were granted; Screen Capture was not needed.

## Minimal reproduction

1. Open a fresh local page containing a native HTML checkbox with `aria-label="Direct host checkbox"` and separate `input`/`change` event counters. The exact standalone page is [`experiments/desktop-control/browser-rc2-direct.html`](../../../experiments/desktop-control/browser-rc2-direct.html).
2. Start the **published rc.2** helper through the Go `host` SDK, begin one turn, observe the unique Chrome window and its document, and grant exactly the observed Chrome application Ref. Confirm the grant is `active`.
3. Observe the unique checkbox with `checked=false`. Its `set_checked` capability is `supported/available`.
4. Submit exactly one `act` plan: `[{"id":"set","op":"set_checked","target":{"ref":"<observed checkbox Ref>"},"set_checked":{"checked":true},"completion":"verify"}]`. Query only that original request ID with `Reconcile`; do not replay.
5. Read the page's own DOM state and event counters independently through its private DevTools endpoint.

`internal/desktopcontrol/native_rc2_direct_browser_test.go` in the Bot integration worktree contains the precise host calls, once marker, and receipt capture. The local, redacted result is `live-acceptance.json` in this directory.

## Expected and observed

Expected: the checkbox becomes checked, with a completed verified receipt.

Observed: the original receipt is `outcome=partial`, step `state=failed`, `delivery=complete`, `verification=not_met`, fault `verification_timeout`, retry class `never_automatically`. Same-ID reconciliation returned the same run/outcome. Before and after the call, independent DOM readback was `checked=false`, `input events=0`, `change events=0`. Only one semantic action was submitted on this page.

The same failure occurred through Bot's compact adapter on separate isolated pages; this direct host SDK reproduction removes that adapter from the call path. A distinct Chrome page accepted a cooperative `pointer.click` on a visible button and its DOM counter changed from 0 to 1. This does **not** justify a pointer fallback for the failed checkbox, because the original semantic delivery is already uncertain. The observation is specific to this OS/Chrome/helper combination; other versions were not tested.

Please investigate the Chrome AX checkbox delivery and desired-state readback boundary. The helper advertises `set_checked` and reports complete delivery even though verification fails and the page's DOM remains unchanged. The independent DOM result cannot rule out a transient AX state before the readback.
