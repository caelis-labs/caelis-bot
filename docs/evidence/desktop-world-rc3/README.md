# Desktop World rc.3 Bot integration — 2026-10-06

Scope: macOS arm64, the public rc.3 SDK and release helper, an Apple
Development signed local Caelis Bot Dev bundle, disposable fixtures and one
isolated model-driven Bot turn. This is a delta acceptance over the preserved
[rc.2 matrix](../desktop-world-rc2/README.md). It does not qualify Windows
GUI, the public Bot release or Developer ID / notarization. Raw screenshots,
browser profiles, request receipts and model session data remain in ignored
local cache; `live-acceptance.json` has the bounded, sanitized results.

## Public release and pin

Desktop World [issue #22](https://github.com/caelis-labs/desktop-world/issues/22)
was addressed by [PR #23](https://github.com/caelis-labs/desktop-world/pull/23)
and published as `v0.1.0-rc.3` at
`43172bb1f3dd26b90a88c346bc9978cbabab5785`. All six public assets were
downloaded into an ignored directory; every `SHA256SUMS` entry matched. The
Darwin archive SHA-256 is
`9bf246b352c37f4e7f719fc222ab528221abc8e0fcba83ab61b03ba715b9fbd9`,
source tar SHA-256 is
`359fa2215cbead13cdc516551a4e56488d3e2847f3fcbf097cd829b8724b593d`,
and `ACCEPTANCE.json` SHA-256 is
`16ee01ca8a065681d5ce7c79733d7808d482fa7b9903067d58545020b2ba5eee`.
All 396 packaged source files matched both the source tar and tracked objects
at that public Git tag byte for byte. The Go module origin names the same tag
and commit with sum `h1:uo6BBK0m1YjC2ep7LNCRyFt3cP+4gtINGfzUqWKgDO0=`.
The public API used by Bot did not change between rc.2 and rc.3; the native
Darwin provider changed. Only the Desktop World dependency and its release pin
changed in Bot; no sibling checkout, replace directive or unrelated module
was used.

The published, unpacked helper SHA-256 is
`51424f2fd5cc3c24750225383b88cd16eae981868478d47d501f25e83995287c`.
The bundled helper was re-signed with the selected local Apple Development
identity and has SHA-256
`c93da60367f79d0e43374b937bd8972f889b3305252265adfab0f3bb9c1226bc`.
The Dev app and helper share Team ID `64KZ67PM5J` and hardened runtime;
`verify-signature.sh` checked the nested Sparkle code and exact rc.3 manifest.
The bundle source marker is Bot commit `1610d60dde5c2a505520afc2a7a944a5e6e51029`.
A local development DMG was mounted read-only; mounted signatures, helper
manifest, Finder layout and app executable bytes matched. Its SHA-256 is
`c6aaa197454c5725c294d37583f9ff7929cceeff8e03d9137dd359f6294675ce`.
This development DMG is not a public or notarized artifact.

## rc.3 live integration delta

| Path | Independent Bot result |
| --- | --- |
| Chrome semantic checkbox | A fresh synthetic file page in a new private Chrome profile was observed and granted through `bot_desktop_inspect` / `bot_desktop_authorize`. Four one-shot `bot_desktop_act` requests produced false→true (`complete/verified`), true→true (`not_applicable/verified`), true→false (`complete/verified`), then disabled-checkbox refusal (`stopped`, `capability_unavailable`, `delivery:none`). Each original `bot_desktop_result` reconciled the same run. The page's own DOM ended false with exactly two `input/change` pairs, one for each actual toggle. No pointer fallback or replay. |
| AppKit | A new exact-title fixture ran `set_checked`, `set_value` and `invoke` through the packaged rc.3 helper. Its own file recorded one submit and the exact text, identical request deduped, and the original receipt survived turn end. This reused a fixture whose literal test text says `rc.2`; the helper version, signature and new run were rc.3. |
| WebKit | A new standard scroll fixture advertised `scroll_into_view=supported/available` and known offscreen=true. One action completed; its own scroll callback fired and fresh AX readback showed offscreen=false. |
| Electron | A new standalone cached runtime with a fresh profile completed one `invoke`; its own log recorded exactly one action. A separate Bot compact `bind` for an absent context command returned `ambiguous_target` before delivery, skipped the following invoke, reconciled the same run and left the log unchanged. |
| Cancellation, EOF and restart | A fresh pending wait was cancelled and reconciled under its original ID as unknown; the app log had no later menu effect and it was not replayed. A separate EOF fixture produced a clean helper exit, matching original/same-ID stopped receipt, skipped invoke and zero app effects; a new session did not inherit that request. |
| Signed capture | The actual Dev process reported Screen Capture `authorized`. One capture on the AppKit fixture returned a 502×480 PNG through Bot's guarded image path. PNG dimensions matched the target-local tile; no desktop transform was exposed. Visual inspection showed the synthetic form and `Submitted: 1`. Its private PNG SHA-256 is `b8a8513f73846741d34695ed383b4c5aa8e2607fa8af14e4489757d243efb0fe`. No TCC setting or keychain ACL was changed. |
| Model entry | A fresh Bot data directory, owned App Server and new synthetic window used the existing native Codex login without copying credentials. The model task completed; the native `bot_desktop_authorize` review was recorded as approved, no approval remained pending, and the app-owned file recorded exactly one `RC3_MODEL_DESKTOP_OK` submission. The harness did not retain per-tool counts for this turn. |

The upstream rc.3 Electron full-operation script stopped after 11 operations
at a context-menu `ambiguous_target` binding and remains **unaccepted**.
The released engine returns that fault when a bounded `bind` query yields
anything other than exactly one object; it creates no binding before that
check. The Bot schema forwards `bind` and its `require_unique` selector, and
the controlled Electron zero-match test above confirms fail-closed behavior
through Bot. The upstream release does not retain the exact candidate set for
its stopped context menu, so this evidence cannot say whether that query saw
zero or multiple objects, or turn the stopped script into a pass. A fresh
observation and unique target would be needed for any later task.

The rc.2 matrix remains evidence for the other existing Bot operations and
entrypoints, including bounded observation/read/sync, dynamic grants,
cooperative focus/pointer/keyboard, native menu, full receipt fencing and
restart behavior. It is labeled as rc.2 evidence; this run did not repeat
every action against rc.3. The rc.3 public Go API and Bot adapter paths are
unchanged, while the affected Chrome semantic path was exercised through all
four Bot compact entries. This does not promote the upstream Electron script,
Windows GUI, or a human approval panel interaction to accepted status.

`GOWORK=off make check`, `make smoke`, `make build`, focused Bot/DesktopControl/
BotSkills race tests and the local mounted-DMG validation passed. The live
tests above are explicit opt-ins with new fixture instances and durable
once markers. The Bot skill's browser checkbox and ambiguous-bind guidance
was updated in English; no default UI or extra model-facing desktop tool was
added.
