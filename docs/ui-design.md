# Interface design

The editable web reference lives in `design/prototype/`. It guides visual hierarchy; native contracts and the existing product remain authoritative for capabilities, requests, permissions and receipts.

## Direction

- Visual thesis: warm neutral reading surfaces, precise alignment, a restrained sage accent and the existing character identity.
- Content plan: conversation and composer first; attachments and decisions in context; purpose-based settings; connection, extras and introduction during onboarding.
- Interaction thesis: one contextual chat/settings window with retained drafts and reading position, anchored menus, and short consistent transitions that respect reduced motion.

## Surface map

| Surface | Presentation | Preserved behavior |
| --- | --- | --- |
| Conversation | Compact message flow, quiet window controls, in-flow decision tray | Transcript paging, streaming, stop/steer, exact native request and receipt bindings |
| General | Preferences, background work, shortcuts, storage | Existing saving, validation and cleanup confirmation |
| Appearance | Character preview and aligned controls | Finished assets, selection, scale preview/commit, import provenance |
| Connections & models | Connection, conversation, background work and execution permission groups on one page | Native model selectors, approval modes and Team Profile IDs; duplicate-name route details from PR #158 |
| Channels / remote / plugins | Clear list/detail hierarchy | Existing authentication, trust, routing and capability availability |
| Extras / privacy / updates | Aligned rows, explicit current state | Feature choices independent of OS grants, approval policy, update and diagnostic receipts |
| Onboarding | Connection → extras → introduction, with consistent actions | Confirmed model unlocks editing; connected runtime gates the first message; OS permissions are requested at use |

The prototype's simulated catalog, account states and controls do not establish functionality. Magpie informs hierarchy and alignment; its session/navigation and model capabilities are not Bot features.

## First-run choices and progress

- After preparing Caelis, route by authenticated, available models. Existing users
  see only a model selector and **Next**; preserve a valid saved choice and keep
  selector changes local until confirmation. Do not load provider catalogs or
  require another authentication on this path. Additional connections belong in Settings.
- Users without an available authenticated model enter the service picker directly.
  Selecting a service opens its required authentication and one model selection;
  **Connect and continue** advances after authentication and model confirmation,
  without a redundant confirmation screen. Reused credentials have no
  editable empty key field. Supported reasoning effort stays visible directly
  below the model selector; selecting it must not require opening advanced options.
  Use compact, equal-width segments with a small consistent inset around the
  selected background.
- Codex follows installation (when missing), authentication (when needed), then
  the same model confirmation row. Authentication alone does not finish setup;
  there is no empty ready/continue screen. Use the runtime's actual model catalog.
- Shared selectors left-align their value and menu labels. When a control has spare
  width, center the same content column in both trigger and menu, keeping all labels on the same
  leading edge; badges and checks occupy consistent trailing columns. Do not
  center each label independently. Placeholders appear only in the trigger, never as disabled
  menu rows; meaningful empty choices such as Default remain selectable.
- Use one shared circular progress indicator: centered in the content area while
  discovering a page, inside the initiating button while applying a choice. Keep
  the button size, label for assistive technology, form geometry and entered
  non-secret values stable. Disable duplicate submission until the effect settles.
- Do not combine independent readiness and operation messages, clear loaded pages
  for background reads, or insert standalone processing text below the footer.
  Progress ends on authoritative completion, never on an animation timer. Respect
  reduced motion; announce progress with status/busy semantics, not alerts.
- Keep these flows and layouts shared between macOS and Windows. Check both
  authenticated and fresh-user paths, slow reads/actions, return navigation,
  expired credentials and narrow/zoomed windows separately from native acceptance.
- After confirming a model, prepare the resident connection while the user chooses
  extras and edits their introduction. **Start chatting** waits for real activation
  and a fresh ready snapshot before journaling the first message. Retain entered
  text on failure, offer return to connection setup, and never replay uncertain
  authentication, configuration or first-message effects. Returning to connection
  selection invalidates the old completion's UI authority.
- Do not include a system-permission page. Extras saves preferences only and goes
  directly to introduction. Request OS consent in the capability that needs it;
  preserve Settings > Privacy & permissions for status and explicit recovery.
- The introduction repeats the route screen's avatar/title heading. Name and
  optional description share an open form column with consistent spacing and no
  enclosing card border or inset. Align labels/fields to the page's leading edge
  and **Start chatting** to the fields' trailing edge. Keep borders on editable
  controls only. Omit redundant subtitles and internal submission explanations.

## Sending feedback

On Send, move the message into chat and clear its text, attachments, references
and quote from the editor in the same visible frame. Start the normal Bot thinking
indicator immediately, including while the original submission is awaiting a
Runtime receipt. Do not add a separate Sending caption to the user bubble.
The first streaming reply replaces the waiting indicator; approvals and stop
retain their native priority. Never invent execution or stop capabilities.

This immediate handoff is presentation only. Keep the durable draft and original
request until acceptance, merge the native echo by request ID and consume the
draft once. Refusal restores the original editable content; uncertainty retains
the original request and blocks another send until reconciled. Delayed reads must
not restore consumed attachments or overwrite newer input. Native draft-write
barriers and IME behavior remain unchanged on both platforms.

## Notification attention

Chat replies and task completion already have conversation/pet presentation; do
not duplicate them as OS toasts. New meaningful reminders use their actual concise
message, not a generic completion label. Keep actionable approvals and update-ready
notices, suppress notifications during focused conversation, and remove resolved
approval notices. Do not send technical errors, quiet checks or restored history
to the notification center. Notification clicks open the app without granting
permission or replaying work.

## Native presentation

Windows task previews follow the macOS dock's three-dot entry and bounded card
stack. Anchor to the pet's feet, use a horizontal fan when space permits and a
side stack near screen edges. Keep the surrounding window transparent; show a
terminal still or icon with a concise caption, and reveal compact lock/close
controls on hover or keyboard focus. Do not replace this with a scrolling task
dashboard. Geometry uses the current monitor's work area and display scale.

AppKit continues to own window controls, sheets, focus, geometry and lifecycle. macOS 26 uses the existing public `NSGlassEffectView` runtime path, older systems use `NSVisualEffectView`; accessibility contrast/transparency settings select solid surfaces. The renderer owns only temporary presentation. Closing the contextual panel does not quit, cancel work or decide a pending request.

Bot guidance in `bot-core/references/screen-input.md` and `tasks.md` describes
permission requests at use; presentation-only changes do not add agent tools.

## Review and acceptance

Run `bash script/run-ui-review.sh` for the isolated Dev profile, or add `--verify`
to rebuild it. The native launch always goes through `build_and_run.sh`.

- Verify every settings destination and its existing controls; no prototype-only capability is implemented.
- Check draft and attachment continuity, chat reading position, delayed native sheets, menu placement, Escape/Tab/arrow navigation and return focus. Opening a selected image reads its validated original for fit-to-window or actual-size viewing; chips keep the small thumbnail.
- Check real replies and questions while switching views; keep approval fixture coverage distinct from live approval acceptance.
- Walk connection, extras and introduction with no permission grants; distinguish installed CLI and running service versions.
- Check normal, zoomed and full-screen windows; reduced motion, contrast and transparency remain system-owned.

Native attachment sheets retain their original pending call for the user's whole
selection/cancellation interaction; other RPCs keep bounded deadlines. A committed
selection notifies the current composer to read its native state even if the original
view was closed. This resolves [Issue #159](https://github.com/caelis-labs/caelis-bot/issues/159)
without reopening a chooser or automatically submitting an attachment.
