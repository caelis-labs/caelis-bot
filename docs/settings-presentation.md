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
Web implementations should reconcile these files after merging main; no Web
transport, routing, deployment or Web worktree is changed here.

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
  accessory. Web hosts must not set that flag without supplying the control.
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
