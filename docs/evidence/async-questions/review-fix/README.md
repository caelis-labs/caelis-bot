# PR195 independent-review corrections

The three independent counterexamples were reproduced, unchanged apart from
formatting, on `5e0d4ec1f1d08d0c1453dc636c36d0495a9c96ec` before implementation.
[Before: all three failed](reproduction-before.txt).
[After: original counterexamples and additional chains pass](regression-after.txt).
Tests are retained in `internal/textchannel/pr195_review_test.go` and
`internal/backend/codex/question_turn_lifecycle_test.go`.

## Semantics

- F1: there is no semantic display alias. Displayed options must equal the native
  answers exactly and in order. Optional title/context only partition the complete
  original question text; changed/omitted conditions are rejected. Existing saved
  aliases are ignored by projection and removed on open. Long original options
  remain visible. Tests cover reversal, omitted conditions, migration and restart.
- F2: ordinary completed turns, commentary, and even `final_answer` are not task
  termination. The typed `TaskEnded` fact is separate from `Task.Result` prose.
  Codex currently only asserts it for explicit retirement; stop, failure and the
  unchanged 24-hour deadline also close unanswered questions. Without an explicit
  task-end contract, a successful ordinary turn keeps unresolved fields bounded
  rather than guessing the user's task ended. The native adapter test answers Q1,
  observes subsequent commentary/final-answer turns and reopens both stores: Q2
  remains pending until explicit retirement.
- F3: durable `asyncRetired` fingerprints fence display generations independently
  of answer states. Old unknown/dispatching receipts keep their original request
  identity; replay and late answer callbacks project the current generation.
  Restart and the actual in-flight dispatch/replacement race are covered.

Native permissions retain their separate approval path. Closing/expiry creates no
answer or authorization. Bot/Worker English guidance was updated accordingly.

## Visual evidence

The before images were supplied by the independent reviewer. After images are
native WKWebView captures of the production History renderer through
`script/build_and_run.sh --chat-preview`, with synthetic data, not browser mocks.
After captures contain the 340x528 content area of a 340x560 whole native window;
the title bar was separately observed in native screenshots. The reviewer's
long title and all three original long option strings are reproduced verbatim.
No alias, semantic truncation, accordion, internal scrollbar or hidden free input.

| Case | Before | After |
| --- | --- | --- |
| Original long question/options | [Reviewer failure](before-legacy-long.png) | [Full original text](after-legacy-long.png) |
| Two questions | [First close clipped](before-multiple.png) | [Both closes and decisions visible](after-multiple.png) |
| Original long question + second question | — | [Light](after-combined.png), [dark](after-combined-dark.png) |
| Keyboard answer of long option | — | [Q1 answered; Q2 remains pending](after-keyboard-answer.png) |

Question rows use the available width, left-aligned long options and word-aware
line breaks; normal-weight context is separate from the question. Opening chat
preserves the first pending question's reading position. Explicit navigation to
new messages and submitting a new chat message still go to the latest content.
The combined scene fits both sources, deadlines, closes, all choices and inputs
above the composer. [15 native layout/action checks](native-checks.json) include
original long text, multiple questions and their combination. Manual Tab/Left/
Enter selected the second original long option; closing Q2 removed its controls.

The complete ad-hoc Dev App was also built and launched through the same entry
point with a new isolated profile and `cliPath=/usr/bin/false`. The actual pet
opened `wails://localhost/?surface=context`. Real native Dismiss persisted Q1;
Q2 stayed pending across restart with unchanged deadline and empty reply IDs.
These full-App screenshots were inspected through native computer use; the
committed images above are the reproducible renderer-preview captures. This is
not live model consumption, real-channel, Windows GUI, or release acceptance.

## Verification

- `make check`: 233 Node tests, generated contracts, i18n, TypeScript/Vite and the
  full tagged Go suite passed. Focused scroll tests also preserve explicit latest
  navigation when pending questions exist.
- `go test -race ./internal/textchannel ./internal/backend/codex -count=1`: passed.
- `make smoke`: [actual initialization evidence](smoke.txt) identifies Codex
  **0.162.1** separately from schema baseline **0.153.4**. No conversation or model
  request. All three asset checks passed. The earlier pinned-version claim has
  been corrected rather than retroactively claiming that runtime was tested.
- `make build`: ad-hoc macOS Dev bundle and pinned Desktop World verification passed.
- Final exact source SHA and terminal CI status are recorded in the PR handoff.

No main checkout, production profile or real channel was changed. No merge or release.
