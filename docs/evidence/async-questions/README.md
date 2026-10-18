# Async question lifecycle and native UI acceptance

2026-10-18. Base: `bb00cd465a8a756eb5b4274b6bcb6a7a5abbaf42` (PR #193).
The lifecycle decisions are recorded [here](../../design/async-question-lifecycle.md).

## Diagnosis

- Completed native async question items outlive native turns. The former seven-day
  reference check ran only while connected and did not reconcile cached IM cards.
  Answered, expired or recycled references could therefore still appear pending
  in the chat history, especially after reload or when older history was loaded.
- A Worker question and the resident's paraphrased async question had separate
  identities. Answering one could leave the other pending. `present` now exposes
  the original question with one state and original answer binding. Old unbound
  duplicates are not inferred answered from natural-language similarity.
- The previous UI duplicated the long question in history and a collapsible tray.
  At narrow sizes its bottom option was clipped beside the composer.

## Screenshots

All images are actual native WKWebView renders of the production History
component, with disposable synthetic data. The before image uses the unchanged
base renderer in a separate archive checkout; only the preview harness and data
were added. No private screenshot or real conversation was committed.

The narrow native window is 340 × 560 points of content (680 × 1120 pixels at
2×, plus the native title bar). This matches the supplied narrow screenshot's
340 × 592 point window including its title bar. Wide content is 760 × 672.

| Before | After, narrow |
| --- | --- |
| ![Base renderer](before.png) | ![Direct question](after-narrow.png) |

[Dark narrow](after-narrow-dark.png) · [Wide](after-wide.png) ·
[Answered](answered.png) · [Dismissed](dismissed.png)

The new question body is 193 points high at the narrow size. All three options
and the always-visible custom input are fully visible. Its bottom is y=383;
the composer starts at y=490. No accordion, pending-question tray, internal scroll
area or question overlay remains. Avatars stay beside their messages. The separate
floating desktop-pet window was not exercised by this fixture; the question now
uses normal History flow instead of adding an overlay to its placement geometry.

## Verification

- `make check`: contract generation, public-tree checks, 231 Node tests, bilingual
  catalogs, TypeScript/Vite and the complete tagged Go suite.
- `make smoke`: pinned Codex 0.153.4 stdio initialization and three asset checks.
  It creates no conversation and sends no model request.
- `make build`: ad-hoc signed macOS Dev bundle, including pinned Desktop World.
- Go race coverage: textchannel, app, backend, Codex, Bot and Telegram. One initial
  concurrent run hit an existing recovered-owner fixture's local socket startup
  failure; the Codex race suite passed when rerun alone.
- Real durable state chains: original option mapping despite compact labels,
  immutable summaries, private sibling questions, stale chat/readback, dismissal
  and answer races, unknown dispatch, deadline/offline/restart, recycled numbers,
  schema supersession, task stop/failure/later completion, save failure and no
  native permission decisions. A completed turn that asks a question stays valid
  even when it contains commentary; prose does not prove the task finished.
- Codex adapter fixture: closure retained across restart for the original Worker,
  no turn/start for idle work, at-most-once steer to the exact active turn, and no
  question-answer envelope or permission manufactured by closure.
- Native manual keyboard: input → Shift-Tab to last option → Left to second option
  → Enter produces answered; typing a free answer and Enter also succeeds.
  Explicit close produces dismissed. Both become compact history with no controls.
  [Native assertions](native-checks.json) additionally verify choice index, reopening,
  visible geometry and absence of internal scrolling.

Native launch command (synthetic profile, no credentials, models or channels):

```sh
BOT_PREVIEW_FIXTURE=question-lifecycle BOT_PREVIEW_WIDTH=340 BOT_PREVIEW_HEIGHT=560 \
  BOT_PREVIEW_CAPTURE="$PWD/.cache/question-evidence/after-narrow.png" \
  ./script/build_and_run.sh --chat-preview
```

The fixture uses a worktree-specific bundle ID. Cmd-T runs its layout/action
checks; Cmd-S saves the current native render and JSON evidence; Cmd-R resets
synthetic data. For automated captures set `BOT_PREVIEW_AUTO_CAPTURE=1`, and use
`BOT_PREVIEW_APPEARANCE=dark` for dark mode.

These are source, persisted-service, native-adapter fixture and native-renderer
results. They do not claim real-model consumption, real-channel delivery, a
production-app session, Windows GUI acceptance or release/distribution acceptance.
Bot skill references were updated for original-question presentation, concise
copy, explicit task replacement and no-authority closure semantics.
