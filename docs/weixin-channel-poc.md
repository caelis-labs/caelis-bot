# Weixin Remote Channel POC

This local adapter uses the HTTP JSON protocol described by
[Tencent/openclaw-weixin](https://github.com/Tencent/openclaw-weixin/blob/main/docs/protocol.md),
at the pinned published plugin version `@tencent-weixin/openclaw-weixin@2.4.9`.
The preceding `@tencent-weixin/openclaw-weixin-cli@2.1.4` is an OpenClaw
installer, not a runtime dependency. The app contains a Go transport and a pure
Go QR encoder; no OpenClaw or Node process runs for this channel. Node 24 is
used by the existing frontend build only. The upstream plugin and QR encoder
MIT notices are bundled from `licenses/`.

## Scope and state

Opening Settings → Messaging → Weixin starts a five-minute QR session and
refreshes it after expiry while the page is open. The user scans
with their own phone, enters a phone verification code if asked, then confirms
the masked account on the Mac. One `ilink_bot_id` and the scanning
`ilink_user_id` are accepted. The bearer token is a 0600 file under
`<Bot data>/Credentials/weixin/`; Telegram tokens and plugin API keys/OAuth
grants use separate namespaces in the same private credential root. Existing
Keychain items migrate on first use and the old item is deleted. `weixin.json` is a private atomic state file containing
the account IDs, cursor, queued private text and progress, context tokens, ingress receipt
states and outbound intent ledger. The UI does not receive tokens or message
contents from this state. The client sends the upstream start/stop presence
notifications when its connection starts or stops. Pausing cancels polling and
sending while retaining pairing. Removing deletes local state and the local
token; the protocol does not provide a verified server-side revocation operation.
The upstream `-14` stale-session response enters a durable one-hour cooldown,
then resumes polling without discarding the pairing. An outbound HTTP result
that cannot be classified is retained as uncertain and is not resent.

`getupdates` is the only input transport. Text from the confirmed owner in a
direct chat enters `backend.SubmitRemote` with the original stable request ID.
Text `/approve` and `/answer` commands use the shared native decision controller,
including while the Bot is waiting on the original request. Each card lists
numbered native options and a bold `提示：发送 /approve A8 + 选项标号` or
`提示：发送 /answer Q8 + 选项标号` instruction. Open questions use
`+ 完整回答`; each question title and hint is bold while options keep their
original order and labels. The local `A1`–`A999` and `Q1`–`Q999` references
cycle after settlement or seven days without an answer. Expiry affects the
local reference only; it does not approve or answer the Runtime request.
Free text is accepted only for a question that
supports it; a custom opinion on a fixed approval never grants permission.
Multi-field requests use one `Q` number per field, allow edits before the
required answers are complete, and submit once to the original native target.
Multiple selections use comma-separated indexes. URL requests display the
original link and native accept/decline/cancel choices; opening a link is not a
successful authorization receipt. A Runtime-marked secret answer is not stored
in the durable inbox or ordinary control receipts. It is held in memory until
the original native decision; a restart asks the owner to re-enter it.
Weixin treats the user's newly typed text as the entire input. It ignores
`ref_msg` for both ordinary model context and control decisions; approvals and
questions require the full `/approve` or `/answer` command. A real 2026-10-11
text quote arrived as a `type=0`, `msg_id="0"` placeholder without quoted text,
`title` or `svr_id`. Earlier attempts sent an empty reference block and then an
unavailable-content marker to Codex; both misled the Bot. Neither is sent now.
The quoted source is not inferred from the timestamp or a nearby chat message.
Tencent's OpenClaw plugin can use inline quoted text or an ID-only `svr_id`
backed by its own cache, but this observed payload supplies neither. See its
[quote cache design](https://github.com/Tencent/openclaw-weixin/blob/main/docs/quote-cache_zh_CN.md)
and [inbound resolver](https://github.com/Tencent/openclaw-weixin/blob/main/src/messaging/inbound.ts).

Telegram buttons call that same controller. Desktop and both companion channels
remain one Bot conversation. Weixin sends only output traced to an accepted
Weixin input and its Turn; mixed or unbound Turns and inputs from other entries
are not pushed to Weixin. Telegram keeps its own mirroring behavior. Pairing and
legacy records baseline existing history.
The inbox and cursor are saved together before dispatch. A dispatching request
that loses its result is not sent again automatically. A completed assistant
item can still be commentary inside a working Turn. The first completed item
is sent as progress; later items merge in order with a visible divider after
three items or when the combined text approaches 70% of the safe chunk limit.
No fixed timer drives sends. A terminal Turn identifies its last ordinary
assistant item as the reply; unsent progress joins it when one message fits.
Codex clears `CurrentTurn` on completion; the adapter binds that terminal
snapshot to the exact latest accepted owner ingress receipt and its user-item
TurnKey, or to a previously observed active Turn. An unrelated old receipt or
unscoped item cannot turn historical commentary into a new reply.
Native approval and async question cards, control receipts and Worker-start
notices remain deliverable while the Turn is running. Questions wait rather than
spend the last estimated slot reserved for a final reply. Direct Worker
approval cards use the original task ID and its saved Bot tool source Turn to
enforce the same Weixin-only routing rule. Later Worker reports use the native
report receipt ID to resolve that task before routing the Bot's result Turn.
Disposable typing may express
progress, but does not refresh a context token or budget. Final text uses
`sendmessage` and the latest owner ingress context token. A reply within 2,400 UTF-16
units and 6,000 UTF-8 bytes stays in one message. Longer replies split at
Markdown blocks and natural boundaries; a complete fenced code block stays
together when it fits, while an oversized fence is closed and reopened across
messages. These are conservative client caps, not a published iLink server
limit. An outbound intent is saved before HTTP dispatch, including its exact
text digest, context token, client ID, text length and estimated budget use.
A timeout or failed HTTP response is marked uncertain; neither the same client
ID nor a new one proves deduplication, so the adapter does not automatically
resend it. Later chunks remain unsent after an uncertain or rejected part. A successful HTTP JSON
response with absent `ret` follows the published client's success handling;
it does not prove the phone displayed it. The private outbound ledger stores
`ret`, `errcode`, a redacted `errmsg` class, a bounded result category and any
confirmed server message ID for diagnosis, without reply text, raw error text
or credentials. `ret=-2` alone is an unclassified rejection. Explicit rate
limit, context expiry, authentication and `prepare failed` remain distinct.

The current phone-window estimate is ten attempted `sendmessage` calls after a
distinct paired-owner input. Ordinary progress uses at most five attempts;
other nonterminal output leaves one slot for a final reply. An actionable
approval can use the last slot when it blocks the work, after which the user's
decision supplies a fresh ingress. Each Markdown chunk and each card counts
as a send; rejected and uncertain calls conservatively consume a slot. The
window is saved with the owner, original input ID, context token and input
time; duplicate updates, typing, restart and an older send result never refill
it. A local twelve-hour age cutoff is conservative. Neither it nor the
ten-send observation is a Tencent service contract. An upgraded legacy record
with unknown remaining allowance waits for a fresh user input. When a full
final reply needs more estimated slots than remain, Weixin sends one
deterministic head/tail excerpt with an omission count and invitation to
request the rest; the shared desktop record retains the complete text.
Pending progress remains in the private Weixin delivery state across restart.
`/refresh` and `/r` renew the local estimate from their original owner ingress
and merge unsent progress without submitting a model request. `/help` and
`/status` are local reads; `/stop` submits the native interrupt, not a prompt.
Unknown sends are not replayed after a later ingress. A predicted tenth send
adds a short `/r` hint when it fits; it is not a separate message. Approval
options and commands are never abbreviated; a multi-part card waits for enough
estimated space for every part. No estimate guarantees service acceptance after
the server has exhausted its actual window.
The adapter obtains a typing ticket
from `getconfig` and renews `sendtyping` only while the paired user's main turn
is active, cancelling it at turn end. Text uses finished sendmessage payloads;
the upstream text builder uses `message_state=FINISH`, and the public protocol
does not document safe in-place edits. Text cards and terminal status therefore
arrive as separate messages. Outgoing reply Artifacts use the Host-owned resolver
already used by Telegram, and each file is a separate budgeted send. The
adapter accepts regular files up to 20 MiB with valid base filenames, reads a
stable snapshot, and uses Tencent's media_type=3 upload and type=4 file item.
The upload is AES-128-ECB with PKCS#7 padding and a fresh 16-byte key; its
MD5 is a protocol checksum. CDN upload requires HTTPS on the pinned Weixin CDN
host, rejects redirects and foreign hosts, and never forwards the bot token.
Upload retry may repeat encrypted bytes before any file message exists. The
original Artifact ID and a persisted send intent fence the final send: an
unknown result is not retried even after a new owner input. Unattempted files
deferred by the estimate may resume after new input; file status is visible in
Settings. HTTP acceptance alone does not prove phone receipt.
Native approval buttons, groups, incoming attachments, edits,
webhook delivery,
proactive window guarantees and Windows credentials are outside this POC.
An HTTP success cannot prove phone display, especially for a proactive send
without a recent context token.

Tencent's protocol document describes its current client and expressly does
not claim to be the full server contract. The MIT source license does not
itself grant a supported independent client integration or backend service
terms. Upstream service changes can break this adapter; recheck each protocol
version before wider release.

The send-window estimate is supported by independent observations in
[Tencent issues #81](https://github.com/Tencent/openclaw-weixin/issues/81)
and [#202](https://github.com/Tencent/openclaw-weixin/issues/202), and by
[pushplus's ClawBot guide](https://www.pushplus.plus/doc/channel/clawbot.html).
Those reports are not a formal server API contract. Separate reports show
[`rate limited`](https://github.com/Tencent/openclaw-weixin/issues/270),
length-related [`prepare failed`](https://github.com/Tencent/openclaw-weixin/issues/284),
and possible [stale context](https://github.com/Tencent/openclaw-weixin/issues/309)
under the same broad nonzero return family. The local classifier keeps them
separate. A third-party [budget implementation](https://github.com/KCNyu/clawock/commit/0e5355ee894e9e82d057f163a0d961147922cce2)
was reviewed as a comparison, not imported as the policy authority.

## Verification boundary

Local tests cover the published request/response shapes, lossless uint64 IDs,
owner and direct-chat filtering, atomic cursor/inbox persistence, no replay of
unknown submissions or sends, terminal-turn selection, budget persistence and
control priority. A separate unauthenticated request to the
official `get_bot_qrcode?bot_type=3` endpoint verifies only handshake
reachability and response shape. It does not verify account pairing, receiving
a real Weixin message, or the phone seeing a reply. Those require the user's
manual scan and a Dev Bot running with its isolated data directory.

The user manually paired the isolated macOS Dev Bot and confirmed that a
private Weixin text message reached the resident Bot and its final reply
arrived on the phone. After the Markdown-aware chunking change, the user
reported one longer reply delivered in three readable parts. Saved copies of
those parts measured 5,781, 5,868 and 3,483 UTF-8 bytes, respectively; the
three fenced code blocks in the middle part remained complete. This is observed
account/device acceptance, not a published service length guarantee. The
phone's earlier “cannot connect to OpenClaw” banner has not been confirmed
resolved. In a later production observation, eleven requested replies yielded
only the first ten on the phone; sending another user message restored delivery.
The old ledger omitted the rejected response's `ret`, `errcode` and `errmsg`,
so this supports a per-ingress budget without proving a formal server rule.
The updated delivery policy has fixture coverage only until the user checks a
real phone. Typing visibility, cooldown recovery, removal, uncertain-send
deduplication and other accounts have not received live acceptance.
