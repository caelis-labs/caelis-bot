# Approval and reconnect acceptance (macOS development)

This PR starts from main after the Desktop World rc.3 merge. Tests used synthetic
requests, a private local Runtime fixture, and an isolated native WebKit preview.
No personal Telegram chat or running Bot instance was used.

| Path | Evidence | Limit |
| --- | --- | --- |
| Caelis Runtime permission head | `TestComputerUseApprovalPreservesEveryNativeOptionAndSingleResolution` projects four exact native IDs/kinds, rejects an invented option, resolves the original target once, and refuses a competing option. Existing live approval and pagination tests remain in `internal/backend/caelis`. | Fixture options demonstrate transport support; a particular Runtime offers only the choices in its current head. |
| Codex MCP elicitation | `TestComputerUseElicitationUsesOriginalNativeRequestAndOfferedChoices` drives the App Server request through the Bot session, rejects unoffered session authority, and checks the response against the original native request ID. | The tested empty-form Computer Use request offers one-call accept, decline and cancel; it does not advertise a persistent choice. |
| Telegram approval | `TestApprovalKeyboardAppearsOnExistingMessageAndOnlyOriginalButtonCanDecide`, `TestUnknownApprovalDecisionIsNeverRepeatedAfterBridgeRestart`, and SDK contract tests cover text-to-keyboard and markup-only edits, message ID binding, competing callbacks, durable unknown outcome and native choice preservation. | No personal or live Telegram account was contacted; real Bot API delivery is untested. |
| Reconnect and history | `TestTerminalOutgoingDoesNotMoveToTailAfterHistoryReplacement`, `TestOrdinaryImagePresentationFollowsReceiptAndRestoresByRequest`, `TestRecoveryBaselinesHistoricalTerminalStatusAndKeepsCurrentReceipt`, `TestRejectedAndUnknownLocalBubblesDoNotMirrorAsDeliveredInput`, plus existing native history paging and Telegram ingress dedupe tests. | Terminal local displays retire when their original anchor leaves the bounded history; uncertain input retains its original request ID and is never submitted again. |
| Desktop presentation | Isolated native WebKit `--chat-preview` screenshots were inspected in English and Chinese with a four-choice synthetic approval. The single-call option is the primary button; native labels are shown unchanged. | This is a UI fixture, not a live OS or Telegram permission grant. |
| Native Codex Runtime to Telegram callback | Opt-in `TestNativeCodexApprovalThroughTelegramCallback` launched a new owned Codex 0.160.0 App Server with the existing login, a private Bot thread and a disposable MCP tool. The actual native one-call elicitation appeared in the Bot snapshot before tool execution; a local Telegram bridge callback resolved that exact request once. The tool-owned marker was created once, and a second callback did not submit another decision. | The bridge used a local Telegram transport fixture. No Bot API request or human button click occurred. No persistent permission was offered or granted. |
| Build | `make check`, `make smoke`, `make build`, and targeted Go race tests. The Dev bundle and nested components were verified with the locally selected Apple Development identity. | A development signature is not a notarized public release. |

The previous Electron instant-menu binding candidate count was not preserved by
the Desktop World test. This PR does not change that provider behavior or claim
the menu binding issue is fixed.

The public Desktop World rc.3 assets were downloaded again for release readiness.
All five `SHA256SUMS` entries passed, the Darwin archive matched the pinned
`9bf246b352c37f4e7f719fc222ab528221abc8e0fcba83ab61b03ba715b9fbd9`,
and the source tar matched
`359fa2215cbead13cdc516551a4e56488d3e2847f3fcbf097cd829b8724b593d`.
The locally packaged, Apple Development signed helper passed manifest and
signature verification against revision
`43172bb1f3dd26b90a88c346bc9978cbabab5785`.

No user-owned, paired Telegram test chat was available for this acceptance.
The user accepted the native Runtime, Bot protocol and UI evidence for the
macOS release without a live Telegram delivery or human callback. Those paths
remain untested and are not claimed as real-chat acceptance.
