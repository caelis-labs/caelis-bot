# Settings and feedback presentation contract

## Shared UI and Web migration

The shared renderer changes are in `Panel.tsx`, `ChatFeedback.tsx`,
`SurfaceBoundary.tsx`, `PluginSettings.tsx`, `Settings.tsx`, `ContextWindow.tsx`,
`context-navigation.ts`, `surface.css`, and `settings/runtime/`. Contextual
`Recovery` call sites also changed in `BotSetup`, `RuntimeSettings`,
`AppearanceSettings`, `TaskSettings`, and `ArtifactPreviewWindow`.
Standalone unsaved channel fields in `TelegramSettings` and `WeixinSettings`
also participate in the return guard; task preference edits stay guarded until
their serialized save is confirmed.
The Web branch reconciles these files against merged main
`e8827b27138af998bb399d701bc037cfee8e9443`, preserving the included #204 worker
recovery and original-receipt contracts.

- `Recovery` requires `message` and accepts an optional, explicitly safe
  `onRetry`. It renders a short operation-local error. It never reloads a surface,
  copies diagnostics or invents a retry when the callback is absent.
- `SurfaceBoundary` alone owns the fatal renderer recovery panel. Its bounded
  remount and explicit reopen do not replay backend operations.
- `ChatFeedback` renders a temporary assistant-styled bubble with the existing
  `BotAvatar`, retry and dismiss. Mount it only while a local error exists, keyed
  by its error identity. It is not an `Item`, never enters the transcript, and
  dismissal never changes native receipts or messages. `Panel` portals composer
  feedback into `.chat-content`; the composer remains mounted while hidden.
  Unknown submissions retain their request IDs. Retry reads the draft/receipt;
  it does not call `Submit` again.
- `context-navigation` uses the cancelable `settings-navigate` guard and checks
  visible dialogs and `data-settings-dirty="true"` forms. The default HTML control
  is a fixed top-right gear/close button without an extra toolbar row.
  Only the macOS host supplies `chrome=native` and a standard AppKit titlebar
  accessory. Web ignores that query flag and always supplies HTML controls.
  Onboarding never exposes either navigation control. Its native entry points
  also route chat/settings/recall back to setup until introduction is resolved.
- `loadConnections` publishes saved selection fields independently, then inspects
  runtime configuration in the background. Page entry does not activate or
  reconnect the resident engine. Failed observations retain previous values,
  report at the affected runtime, and do not grant readiness. Cached status during
  refresh is labeled as last checked. Native save still validates current state.
  Request generations and model-delivery epochs prevent old observations from
  overwriting a newer selection.
- Plugin `installed` / `enabled` are persisted package facts, not proof of an
  MCP connection. Only the runtime server detail can confirm `connected`.
  `syncState` describes global reconciliation and must not be mapped to every
  item's status. The backend overlays progress/failure only for acknowledged
  package changes newer than the projected revision. `PluginAction("retry")`
  retries reconciliation of existing adapter receipts, not installation/OAuth.
  Renderer reads are ordered against mutations, including equal revisions.

## Native menu and acceptance

macOS `NSStatusItem` and Windows Wails tray menus expose only `capture.start`,
with the localized title Screenshot / 截图, while the feature is enabled.
The macOS installed menu updates its tagged item immediately; Windows replaces
the tray menu after a committed preference change. Paste/pin internals, capture
commands, saved F1/F3 shortcuts and permission checks remain unchanged.
Windows retains the supported HTML title-right navigation; this change does not
claim Windows native GUI acceptance.

Run `script/build_and_run.sh --settings-preview` for a separate AppKit/WKWebView
fixture using synthetic transport, the production React surfaces and the
production native titlebar accessory. Its menu supports slow/failed runtime
reads, plugin installation, chat read failure/recovery, unknown send receipts,
and navigation retention. It never starts a backend or accesses accounts.
Save evidence writes snapshots and call counts to `.cache/settings-acceptance/`.
The navigation regression records before/after scroll, draft and attachment
presence. This fixture complements `make check`, `make smoke`, `make build` and
inspection of the actual isolated Dev package; it is not live account evidence.
`node script/desktop-settings-evidence.mjs` checks recorded navigation, feedback
dismissal, message preservation and the single submission in the unknown-receipt
scenario. The Capture menu fixture uses the production AppKit menu builder and
gate updater, with native assertions for one command and hidden separator.

Bot skill guidance is unchanged: this adjusts presentation, observation and
navigation of existing capabilities, not Bot tools, permissions or workflows.

## Web surface acceptance

Web uses the same components and state, with fixed content-area gear/task icons
and the shared close-settings control. It does not emulate AppKit attachment
APIs. The Web navigation adapter honors dialogs/dirty forms before changing a
route; every entry also checks durable setup/introduction state. A retained,
invisible chat keeps its layout dimensions so returning does not collapse scroll
to the bottom. Its draft, staged files and composer DOM survive the switch.

Only `ChatFeedback` offers the human-readable feedback summary. Local settings
and setup errors stay inline; fatal renderer recovery keeps bounded remount and
explicit reopen, without a generic copy card. Copies include allowlisted issue,
time, page and connection scope, never raw diagnostics or arbitrary private URLs.
Dismissal does not change transcripts, approvals or uncertain operations.

`script/web-settings-browser.cjs` drives the production WebApp/transport against
the same synthetic data as the native AppKit fixture. It covers retained
draft/files/scroll, modal URL guards, local plugin progress, saved values during
slow/failed observations, read-only task results and unknown-submission readback.
`script/web-feedback-browser.cjs` drives the isolated Rocky service: actual
first-run guards and read-only Magpie model listing, injected HTTP failures,
and seven settings pages at 390/900/1600 pixels using a browser-only completed
setup projection. Neither fixture is evidence of completed account login.

## Installation cancellation and chat continuity

Cancelling a Codex installation dismisses the shared progress dialog immediately.
The host operation and its original cancellation receipt remain authoritative;
progress reads continue, and a running/unknown installation cannot start another
write. A settled `cancelled` state is not a failed install or a retry dialog,
including after remount. Actual failure still has an operation-local recovery.

When the Runtime echoes an already-visible user input, the shared backend
publishes that exact outbox row to the local IM before retiring it. The
independent application observer may arrive later. This applies to both first
introduction and ordinary input, preserves the original request ID, and does not
promote or replay an execution receipt. Accepted local input keeps the waiting
indicator until its matching transcript observation arrives.

Web edit-lease recovery refreshes only the composer, retaining the mounted
conversation and its optimistic rows. `web-message-stability-browser.cjs` checks
DOM identity and every rendered frame across delayed observations, stale SSE,
reconnection, edit-lease loss and reload. `codex-install-cancel-browser.cjs`
checks immediate dismissal, remount, original operation count and real-failure
presentation using a synthetic host. These checks do not establish live model
or account acceptance. Bot skill guidance is unchanged: no Bot tool, permission,
execution or recovery workflow changes.
