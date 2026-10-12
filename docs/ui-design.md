# Interface design

The editable web reference lives in `design/prototype/`. It guides visual hierarchy; native contracts and the existing product remain authoritative for capabilities, requests, permissions and receipts.

## Direction

- Visual thesis: warm neutral reading surfaces, precise alignment, a restrained sage accent and the existing character identity.
- Content plan: conversation and composer first; attachments and decisions in context; purpose-based settings; four existing onboarding phases.
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
| Onboarding | Quiet four-step progress and consistent footer | Existing introduction, extras, permissions, connection order and skip semantics |

The prototype's simulated catalog, account states and controls do not establish functionality. Magpie informs hierarchy and alignment; its session/navigation and model capabilities are not Bot features.

## Native presentation

AppKit continues to own window controls, sheets, focus, geometry and lifecycle. macOS 26 uses the existing public `NSGlassEffectView` runtime path, older systems use `NSVisualEffectView`; accessibility contrast/transparency settings select solid surfaces. The renderer owns only temporary presentation. Closing the contextual panel does not quit, cancel work or decide a pending request.

No Bot Skill update is required: this renovation changes presentation and window arrangement, plus model route descriptions, without adding agent capabilities, tools or recovery workflows. Functional defects discovered during acceptance belong in separate Issues.

## Review and acceptance

Run `bash script/run-ui-review.sh` for the isolated Dev profile, or add `--verify`
to rebuild it. The native launch always goes through `build_and_run.sh`.

- Verify every settings destination and its existing controls; no prototype-only capability is implemented.
- Check draft and attachment continuity, chat reading position, delayed native sheets, menu placement, Escape/Tab/arrow navigation and return focus. Opening a selected image reads its validated original for fit-to-window or actual-size viewing; chips keep the small thumbnail.
- Check real replies and questions while switching views; keep approval fixture coverage distinct from live approval acceptance.
- Walk the four existing onboarding phases and distinguish installed CLI and running service versions.
- Check normal, zoomed and full-screen windows; reduced motion, contrast and transparency remain system-owned.

Native attachment sheets retain their original pending call for the user's whole
selection/cancellation interaction; other RPCs keep bounded deadlines. A committed
selection notifies the current composer to read its native state even if the original
view was closed. This resolves [Issue #159](https://github.com/caelis-labs/caelis-bot/issues/159)
without reopening a chooser or automatically submitting an attachment.
