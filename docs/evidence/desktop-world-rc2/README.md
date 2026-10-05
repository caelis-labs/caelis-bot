# Desktop World rc.2 Bot integration — 2026-10-06

Scope: macOS arm64, isolated native fixtures, one disposable Chrome profile,
the actual rc.2 release helper bundled in a local **ad-hoc signed** Dev app,
and Bot contract/Runtime fixtures. This is not a model-driven Dev Bot session,
a public release, notarization acceptance, or Windows GUI acceptance. No
personal Bot/Codex app, browser profile, account, message or TCC setting was
used. The upstream release's own Mac acceptance says its final commit was not
interactively retested; none of its results are counted as Bot acceptance here.

## Release identity and packaging

| Item | Verified value |
| --- | --- |
| Go module/tag | `v0.1.0-rc.2`, origin revision `e7b53a1812fe3892cf8e4b903479d208dfaa0104`, module sum `h1:1SBDpPOKGu1fNzEgeOTiV3/d8vuXTIOEFmfMkFAawFM=` |
| Official Darwin arm64 archive SHA-256 | `7ff6a44ec7cd8cba63f6f799a3729141d384afd8d2e5154dee69cb9d546e95cc` |
| Official source archive SHA-256 | `289360b6bd4d15dc30ebfb23dbe1dc7573433454efefd1335ffba3e20309c8c4` |
| Official `ACCEPTANCE.json` SHA-256 | `d9c200561a42eb41b46fee450d483ccaf67196101821982d7bdb18aad8a7e345` |
| Unpacked release binary SHA-256 | `35df50077d34a4e97516bf53d4502fc69c39cafa0b2734ccb58902cd327dbe0c` |
| Bundled, locally signed binary SHA-256 | `6778fbd814b1d99164daeb97154f2838219e3f6c12954a580d36770f12818329` |

The tag's module origin, release manifest, helper `version`, Info.plist marker,
archive SHA-256 and source archive SHA-256 agree. All 393 packaged
corresponding-source files matched the source archive byte for byte. The bundle
contains MPL-2.0 LICENSE, NOTICE, third-party notices and source. `codesign
--verify --strict` passed on the bundled helper and Dev app. This is local
ad-hoc signing; Developer ID, notarization, stapling and Gatekeeper remain
release gates.

`dtw hello`, `version`, `schema` and `doctor` were checked from the published
archive. Schema lists observe/read/sync/act/capture/get/cancel. Doctor reported
one display and Accessibility/Input permission granted on this host; Screen
Capture and user-input observation were `not_requested`. No permission prompt
was triggered.

## Integration matrix

| Path | Result and independent evidence |
| --- | --- |
| Four compact Bot tools, schema, original IDs, turn ownership and review routing | Contract, transport, Runtime and skill-loader fixtures passed under `make check` and targeted race. The added `grants` inspect and reviewed grant/declare/revoke share existing compact entrypoints. No model-driven Bot run was made. |
| Observe app/window/AX; bounded incomplete scan and continuation; read and sync | Published helper found exact isolated fixture windows, controls and zero-match incomplete scan; continuation remained usable. Native text read and cursor sync passed. |
| Observed-app grant, future-app declaration, dynamic revocation and turn end | Published helper granted the observed AppKit app and bound an exact window-title declaration to the running fixture. Future-app pending/revoke, deny-after-revoke and post-turn receipt passed. Current-turn `grants` reports helper state; declaration acceptance is not represented as active authority. Time-based expiration was fixture-tested only. |
| `set_checked`, `set_value`, `invoke` | One AppKit plan completed. The fixture's own file recorded exactly one submission and the Unicode value; a repeated identical request did not submit again. |
| `focus`, `bind_focus`, `keyboard.press`, `keyboard.type_text`, `pointer.click`, `pointer.scroll` | One cooperative plan completed and the fixture's own file recorded exactly two submissions with the new Unicode text. The native AX window showed the new value, unchecked state and scrolled label. Original keyboard receipt remained readable after turn end. |
| `bind`, `wait`, `pointer.move`, `pointer.drag` | A fresh unique AppKit fixture completed exact checkbox binding and false-state wait, then one cooperative slider move/drag. Independent native AX readback changed `Slider: 0` to `Slider: 82` (float value `81.8333`); original pointer receipt remained available after turn. A durable once marker prevents accidental replay. |
| `set_expanded`, `set_selected` | Corrected one-shot state test passed on a fresh, visually inspected fixture. Its own event log recorded true/false for both states; window returned collapsed. |
| `scroll_into_view` | The custom AppKit row returned `capability_unavailable`, `delivery:none`, `outcome:stopped`. The original receipt was retained; this provider/action pair is unqualified. |
| Synthetic Chrome browser provider | Exact isolated page/window and AX checkbox/button were discovered with the published helper. Its first `set_checked` + `invoke` plan returned `verification_timeout`, retry class `never_automatically`. The test queried that original receipt and did not replay. Independent local DOM readback remained `checked=false`, `Clicks: 0`; it cannot prove no transient delivery. Browser input is **not accepted**. |
| Screenshot/capture, image path and geometry | Contract fixture covers image ownership, bounded geometry, output and status redaction. Real capture was not run because Screen Capture permission was `not_requested`; no real image claim. |
| Menu interaction | No explicit menu op exists in the catalog; menu navigation uses observed targets with invoke/pointer/keyboard. No independent rc.2 menu-effect readback; **not accepted**. |
| Native instance exit, EOF, restart, cancellation, original request conflict and unknown recovery | Transport/Controller fixtures cover fencing, cleanup and original-receipt behavior; published helper exercised duplicate-step rejection, immutable request conflict and post-turn receipt. No actual in-flight OS input was cancelled/restarted, so that live path remains unqualified. |
| Electron provider and model-driven Bot pipe | Not run on an isolated Electron target or real isolated Dev Bot profile; not accepted. Bot compact routing, Codex/Caelis skill discovery and loading were fixture-tested only. |

The first state fixture run stopped after `scroll_into_view` was refused. Its
second run completed the remaining state writes but failed only because the
test matched JSON fields in a fixed order. The app-owned log and window proved
all four effects. The test now parses JSONL events and skips completed fixtures;
it passed on a new uniquely titled fixture. No unknown input was replayed.
The Chrome page similarly has a durable once marker for its single failed input
attempt; do not rerun that plan on this page under a new request ID.

## Local gates and evidence

`GOWORK=off make check`, `make smoke`, `make build`, targeted
`go test -race ./internal/desktopcontrol ./internal/bot ./internal/botskills`,
manifest validator tests, app signature verification and the published helper
live tests passed. The final `make check` includes the new browser and pointer
tests in their skipped default mode; all native input tests are explicit
opt-ins. Build cross
compiles Windows and other platform binaries but does not qualify Windows GUI.

`native-fixture-result.json`, `state-events.jsonl`, `pointer-readback.json`
and `browser-after.json`
contain only isolated business-state readbacks. `browser-after.json` came from
the synthetic page's own loopback DevTools endpoint in its private profile,
independent of Desktop World. Raw desktop inventory, window screenshots,
personal paths, account data and conversations are not published. Native
fixture windows were also inspected visually through their exact app paths.
