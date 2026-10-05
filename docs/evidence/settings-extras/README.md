# Settings, Telegram and Extras acceptance

## Baseline observed before implementation

The isolated native WKWebView fixture captured the existing Telegram page in
unconfigured, pairing, candidate, connected, paused and webhook states, plus
first-run setup. Its screenshots are in
`.cache/settings-extras-fixture/baseline/telegram-*.png` and
`setup-empty-zh-CN-dark.png`. Telegram was part of the Runtime connections
destination. Waiting for pairing carried a generic “Not connected” status and
both opening the Bot and creating a new link competed in the same row. Paused
setup showed a token input beside resume. The webhook decision was mixed into
the initial token form. Screenshot context was under permissions, while F1/F3
shortcuts were under General. The Runtime fixture itself was captured later as
`baseline/runtime-empty-en-light-860.png` and is a reference for the separate
AI account page, not a pre-change comparison.

## Isolated native fixture after implementation

`bash script/settings-extras-native-fixture.sh <folder> <page> <state> <lang>
<theme> <width>` mounts production React components with synthetic Wails
responses in a temporary signed WKWebView app. It does not access the daily Bot
profile, Telegram account or macOS Screen Recording permission. Each listed PNG
was opened and visually checked for clipping and legibility:

| Surface | Native captures in `.cache/settings-extras-fixture/` |
| --- | --- |
| Telegram: empty, pairing, candidate, connected, paused, webhook | `after/telegram-*.png` |
| Telegram: connecting, retryable network, expired pairing, existing Bot route, forget confirmation | `final/telegram-*.png` |
| Extras enabled and disabled | `after/extras-*.png` |
| First run, enabled and disabled; save failure | `after/setup-*.png`, `final/setup-*.png` |

The captures cover English and Chinese, light and dark, and 860 and 640 point
windows. The first-run off capture has no Screen Recording request row. The
on/off controls and Continue button remain available in the narrow dark view.
The fixture interaction log
`after/setup-permission-on-en-light-860.png.json` records `screenBefore=true`,
`screenAfter=false`, `SetCaptureEnabled` before `FinishPermissionGuide`.
`after/setup-permission-fail-zh-CN-dark-640.png.json` records no
`FinishPermissionGuide` after a failed save.

## Behavior and limits

`make check`, `make smoke`, `make build`, and focused native and race tests
cover the release preparation paths. Capture tests exercise default and legacy
preferences, native entry points while disabled, re-enable and restart,
rollback on persistence/shortcut registration failure, and a native selection
gate. Frontend tests cover Telegram phase projection, settings repair links and
the first-run save ordering. `make smoke` performs only the installed Codex
App Server handshake; it does not send a model or Telegram request. The build
is ad-hoc signed.

The fixture confirms rendering and synthetic interaction order. Live Telegram
pairing, Keychain ownership, webhook takeover with a real account, physical
F1/F3 delivery, actual TCC prompts, and a full app window launched with an
isolated Bot profile remain unverified. The running user Bot was not touched.

The follow-up `node script/settings-extras-parent-regression.mjs` mounts the
real BotSetup parent. It waits through multiple BotInitialization polls after
turning capture off, then checks the unsaved choice, save-before-finish order
and failed-save retention. It also verifies that a later Telegram status poll
clears a transient load error while retaining an explicit open-action error.
