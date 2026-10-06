# Electron transient context-menu bind — Bot rc.3 acceptance

The released Desktop World full script uses `poc/background-input/accept-full.py`
and `Fixture.html`. After eleven successful operations, it right-clicks
`POC Canvas`, binds `POC menu commit` as a `menu_item` within the window
at depth 12, then invokes the binding in one cooperative plan. Its published
rc.3 acceptance stopped at `ambiguous_target`.

We rebuilt that menu markup and the preceding form/canvas event path in
`experiments/desktop-control/electron-rc3-menu/`. Electron 44.5.1 ran with
fresh private profiles and app-owned JSONL logs. The Bot opt-in live tests in
`internal/bot/desktop_rc3_electron_*_native_test.go` use the actual packaged,
Apple Development signed rc.3 helper (SHA-256
`c93da60367f79d0e43374b937bd8972f889b3305252265adfab0f3bb9c1226bc`)
through `bot_desktop_inspect/authorize/act/result`. Each input stage has a
new durable once marker and request ID. The raw receipts, logs and profiles
remain in ignored local cache; the rows below contain no private paths or
desktop contents.

After observing and authorizing the exact synthetic window and canvas, the
stopped request has this shape (Refs and ID are new per fixture):

```json
{
  "requestId": "new-fixture-context-menu-001",
  "steps": [
    {"id":"open","op":"pointer.click","target":{"anchor":{"target":"OBSERVED_CANVAS_REF","u":0.5,"v":0.5}},"click":{"button":"right","count":1}},
    {"id":"bind","op":"bind","bind":{"name":"menu","require_unique":true,"locator":{"within":"OBSERVED_WINDOW_REF","name_equals":"POC menu commit","role":"menu_item","max_depth":12}}},
    {"id":"invoke","op":"invoke","target":{"bound":"menu"},"completion":"dispatch"}
  ]
}
```

| Fresh scenario | Receipt and independent application result |
| --- | --- |
| Menu opened separately | Right-click completed; same-ID result returned the same run. Original window/name/`menu_item`/depth-12 query had complete coverage and **one** candidate with `invoke=supported/available`. A broader same-name query also had one. |
| Immediate three-step control | A separate fresh page completed right-click, original bind and invoke in one plan. Its app log recorded one `menu_commit`. |
| Full pre-menu state | All eleven original operation types completed on another fresh page. App logs showed move, click, double, middle, 215,0 drag/drop, separate vertical and horizontal scroll, Unicode submit, shortcut replacement, multiline text and Tab focus on `POC Canvas`. Every request reconciled its original run. The Bot plan used the documented `bind_focus` after the shortcut's click: a prior independent run targeting the old field Ref was refused before the keypress with `background_target_not_focused`; it was not replayed. |
| Immediate menu bind after eleven actions | A further fresh run reproduced the published stop: right-click `delivery=complete`; bind `ambiguous_target` with `delivery=not_applicable`; invoke `skipped/none`; overall `partial`; same-ID result matched. The app log showed the menu visible and no commit. |
| Fresh query and explicit completion | The original exact selector then had complete coverage and **one** menu item; same-name broader query also had one. A **new menu-only** bind/invoke on that already-open item completed; the app log had exactly one `menu_commit` and one close. The opening click and partial plan were never resent. |

The released engine rejects a bind when its bounded query returns anything
other than exactly one candidate. The Bot adapter forwarded the original
selector and failed closed. The fresh one-candidate observations exclude a
stable duplicate or permanently wrong role/scope. The difference between
immediate and fresh queries is consistent with renderer/accessibility
visibility timing after right-click. The rc.3 receipt does not expose the
candidate count at the failed instant, so **zero versus multiple at that
instant remains unmeasured**. This is a bounded upstream provider behavior
to investigate, with a verified safe Bot workflow; it is not evidence that
the skipped command ran. The original upstream full-script run remains
historical; the Bot task's visible menu effect is independently verified.

To repeat on a new synthetic profile, create a private JSON config containing
`title`, `profile` and `log`; launch the isolated Electron runtime with
`Electron experiments/desktop-control/electron-rc3-menu CONFIG.json`.
Set `CAELIS_BOT_TEST_DESKTOP_WORLD` to the packaged helper and the
`CAELIS_BOT_TEST_RC3_ELECTRON_FULL_*` title/log/marker/evidence/run
variables, then run the opt-in
`TestPackagedRC3ElectronFullPreMenu`. Its immediate bind may complete or
return the measured partial refusal. If it refuses, keep the same fixture
open, set the matching `CAELIS_BOT_TEST_RC3_ELECTRON_MENU_*` variables
with a **new** invoke marker/request run suffix, and run
`TestPackagedRC3ElectronContextMenuInvoke`. Never delete a once marker or
repeat an uncertain input to make a test green.
