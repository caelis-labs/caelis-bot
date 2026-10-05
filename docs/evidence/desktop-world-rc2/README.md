# Desktop World rc.2 Bot integration — 2026-10-06

Scope: macOS arm64, isolated native and browser/Electron fixtures, the actual
rc.2 release helper bundled in a local **ad-hoc signed** Dev app, and Bot
contract/Runtime fixtures. An isolated Dev Bot with fresh Bot data and empty
Codex home reached its actual first-use window; that Codex home reported `Not
logged in`, so no model request was sent. This is not a public release,
notarization acceptance, or Windows GUI acceptance. No personal Bot/Codex
application, message, account or TCC setting was operated. An attempted VS Code
fixture with a private profile still accessed its user-level shared storage at
startup; it was immediately stopped and excluded from Electron evidence. A
standalone cached Electron runtime with an independent temporary profile was
used instead. The upstream release's own Mac acceptance says its final commit
was not interactively retested; none of its results are counted as Bot acceptance
here.

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
| Observed-app grant, future-app declaration, dynamic revocation and turn end | Published helper granted the observed AppKit app and bound an exact window-title declaration to the running fixture. On a separate fresh fixture, the Bot first observed the desktop, declared an exact **unlaunched** window, and saw pending with no app Ref; one standard native launch bound the same grant ID to one observed app/window, and exit of its self-reported PID changed that ID to expired/application_exited. The earlier live revoke, deny-after-revoke and post-turn receipt checks passed. Current-turn `grants` reports helper state; declaration acceptance alone is not active authority. Receipt/history time-based expiry remains fixture-tested only. |
| `set_checked`, `set_value`, `invoke` | One AppKit plan completed. The fixture's own file recorded exactly one submission and the Unicode value; a repeated identical request did not submit again. |
| `focus`, `bind_focus`, `keyboard.press`, `keyboard.type_text`, `pointer.click`, `pointer.scroll` | One cooperative plan completed and the fixture's own file recorded exactly two submissions with the new Unicode text. The native AX window showed the new value, unchecked state and scrolled label. Original keyboard receipt remained readable after turn end. |
| `bind`, `wait`, `pointer.move`, `pointer.drag` | A fresh unique AppKit fixture completed exact checkbox binding and false-state wait, then one cooperative slider move/drag. Independent native AX readback changed `Slider: 0` to `Slider: 82` (float value `81.8333`); original pointer receipt remained available after turn. A durable once marker prevents accidental replay. |
| `set_expanded`, `set_selected` | Corrected one-shot state test passed on a fresh, visually inspected fixture. Its own event log recorded true/false for both states; window returned collapsed. |
| `scroll_into_view` | The custom AppKit row returned `capability_unavailable`, `delivery:none`, `outcome:stopped`. A separate standard WebKit target advertised `supported/available` and known offscreen=true. One packaged-helper scroll completed; the page's own `scrolled:true` callback fired and fresh AX reported offscreen=false. |
| Synthetic Chrome browser provider | Initial Bot `set_checked` + `invoke` returned `verification_timeout`; the original receipt was queried, with no replay. On a new isolated page, one Bot semantic write again timed out. On a third independent page, **direct published host SDK without Bot** reproduced `outcome:partial`, semantic `delivery:complete`, `verification:not_met`, fault `verification_timeout/never_automatically`; same-ID reconcile matched the run. Its private-page DOM stayed unchecked with zero input/change events. A fourth, distinct pointer-only page had a unique enabled, onscreen button and known bounds; one cooperative Bot `pointer.click` completed and independent page DOM changed `Clicks: 0`→`Clicks: 1`. This isolates the semantic checkbox failure to the rc.2 helper's Chrome path on macOS 27.0.1 / Chrome 154.0.8037.98; it does not prove all Chrome versions fail. Browser observation, grant and pointer click are accepted; semantic checkbox input is **not accepted**. |
| Screenshot/capture, image path and geometry | Contract fixture covers image ownership, bounded geometry, output and status redaction. Real packaged-helper `capture_windows` for the isolated Electron app returned `permission_denied` before pixels. `doctor` reported Screen Capture `not_requested`; no real image, image path or geometry success claim. |
| Menu interaction | No explicit menu op exists in the catalog. A unique isolated AppKit menu item advertised `invoke=supported/available`; one `dispatch` invoke completed, and the app-owned log recorded `menu_count:1`. An earlier `verify` plan was rejected during helper argument validation before delivery; it retained its original ID and caused no event. |
| Native instance exit, EOF, restart, cancellation, original request conflict and unknown recovery | Transport/Controller fixtures cover fencing and conflict. With the published helper, a pending `wait` before a would-be menu invoke was cancelled: its same-ID receipt was `stopped`, with no app effect. Owner-pipe EOF exited cleanly; a new helper session did not inherit the request, and the app log remained unchanged. Terminating the exact disposable app PID during another pending wait produced `outcome:unknown` on the original and same-ID reconcile, with no later app event; it was not replayed. The EOF call's returned receipt fields were not saved, so EOF receipt classification is not claimed. |
| Electron provider | A standalone Electron 44.5.1 runtime, fresh temporary profile and synthetic page exposed a unique `invoke=supported/available` button. The packaged helper completed exactly one invocation; Electron's main-process event log independently recorded `action_count:1`, and the original receipt remained available after turn end. |
| Model-driven Bot pipe | The final Dev bundle launched through `script/build_and_run.sh --restart` with a private Bot data directory and empty Codex home; its actual first-use settings window was inspected and native host logged ready. `codex login status` under that same empty home said `Not logged in`. No account login or model request was made, so model-driven desktop routing and live native approval remain **unaccepted**. |

The first state fixture run stopped after `scroll_into_view` was refused. Its
second run completed the remaining state writes but failed only because the
test matched JSON fields in a fixed order. The app-owned log and window proved
all four effects. The test now parses JSONL events and skips completed fixtures;
it passed on a new uniquely titled fixture. No unknown input was replayed.
Each failed Chrome semantic page has a durable once marker; do not rerun those
effects under new IDs. The separate pointer page also has its own once marker.
The direct-host comparison is scoped
to this published helper and host/browser version. The Bot diagnostic test's
first run had a test-harness mistake: it treated an error result from the
original `verification_timeout` as if the receipt were missing. That test was
corrected without repeating its input; the direct-host original receipt and
independent DOM evidence were saved separately.
In the released Darwin bridge, checkbox support is advertised from a settable
AX value or AXPress; the semantic action treats successful AX delivery as
complete, then the engine polls the desired `checked=true` state. The direct
receipt shows that delivery/verification split precisely. The independent DOM
readback proves the synthetic page's checkbox and event counters did not
change, but cannot exclude an unobserved transient AX delivery. There is no
Bot-side retry or fallback that can safely repair this unknown effect.

## macOS capture gate

The code that calls `CGPreflightScreenCaptureAccess` and ScreenCaptureKit runs
inside the bundled `Contents/Resources/DesktopWorld/bin/dtw` process. This
exact ad-hoc helper has identifier
`dtw-55554944de2e0864f3dc333afc04f8aafbe56d1b`, CDHash
`f63e7145a1eb956dfba33ec3a1800d20bf8480bc`, and no Team ID. Its parent
Dev app has bundle identifier `dev.caelis.bot.dev`, CDHash
`246f77dad92d9d5a7c000d0e955aaaec49c629cb`, and no Team ID. The direct
managed-helper test returned `permission_denied` at capture-window enumeration;
that launched process chain did not have Screen Capture access. To finish real capture
acceptance, the user must open **System Settings → Privacy & Security → Screen &
System Audio Recording**, enable the Caelis Bot Dev or `dtw` entry that macOS
shows for this exact build (or approve its first OS prompt), restart only this
Dev build, then repeat the isolated-window capture and image readback. TCC's
responsible-app label cannot be confirmed before the OS prompt; no TCC reset,
implicit approval, or permission request was performed.

## Local gates and evidence

`GOWORK=off make check`, `make smoke`, `make build`, targeted
`go test -race ./internal/desktopcontrol ./internal/bot ./internal/botskills`,
manifest validator tests, app signature verification and the published helper
live tests passed. The first final `make check` in the restricted sandbox failed
an unrelated synthetic LAContext assertion; the isolated test and full check
passed with native macOS access. The final `make check` includes the new native
tests in skipped default mode; all native input tests are explicit opt-ins. Build cross
compiles Windows and other platform binaries but does not qualify Windows GUI.

`native-fixture-result.json`, `state-events.jsonl`, `pointer-readback.json`,
`browser-after.json` and `live-acceptance.json`
contain only isolated business-state readbacks. `browser-after.json` came from
the synthetic page's own loopback DevTools endpoint in its private profile,
independent of Desktop World. Raw desktop inventory, window screenshots,
personal paths, account data and conversations are not published. Native
fixture windows were also inspected visually through their exact app paths.
