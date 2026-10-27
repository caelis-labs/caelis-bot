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
remain one Bot conversation. Weixin is an additional delivery endpoint. Any valid,
deduplicated paired-owner private text, including `/r`, `/answer` and `/approve`,
refreshes its context token and local receive allowance before model/control
dispatch. Desktop/TG messages neither renew nor close it. New visible Bot,
Runtime and Worker output can use the remaining allowance without causal input
or Turn filtering. Current shared IM projection, paired account and recipient
boundaries remain authoritative. Private Worker transcripts and host notices
are not imported. Existing history is baselined using shared IM presentation
order; unknown historical items with no cursor do not become new output.
`SeenAt` is local first presentation, not native generation time. A previously
unseen native report newly exposed by the shared IM is treated as new visible
output; the adapter cannot infer its earlier creation time or semantic value.

A durable delivery spool stores known candidate IDs and frozen batches/parts.
Ordinary input discards old unattempted progress and nonurgent files. Unattempted
important results that were still current can continue; important results held
by a paused/exhausted window require explicit `/r` recovery. `/r` and `/refresh`
never execute work or replay accepted, rejected or unknown send intents. Recovery
continues retained original body parts and also restores retired, unattempted
progress from the current shared IM's last 128 visible items within twelve hours
and since this channel's observation start. It does not page older history or
retain another transcript. Progress covered by an assistant result with the same
known Turn is excluded; a tool failure or missing Turn cannot prove coverage. Results
stay ahead of recovered progress. Native classification upgrades are independent
of text/status changes and can revive only never-attempted retired candidates.
The spool holds at most 128 candidates; under pressure it evicts lower-priority unfrozen work first, then held
older results for a new result. Dropped capacity is disclosed with the next text
and a full-desktop entry; frozen parts and current higher/equal-priority work
keep their order. Long result bodies use explicit complete-block excerpts and a full
shared-desktop entry. A preview has its own HTTP intent; it does not complete the
original parts or merged members. `/r` resumes those original body parts without
replaying the preview, including when its receipt is unknown. An unknown/rejected
original body part still stops that body's continuation. The existing 64 KiB
per-body / approximately 512 KiB queued-body bounds remain; bodies retained only
as excerpts require the desktop entry and are not marked fully accepted. Existing
terminal receipts, including older-version records, are not reinterpreted from
message appearance. This is a bounded delivery cache, not a second IM archive.

The first meaningful candidate is sent immediately, with no synthetic ordinary
acknowledgement. Later same-priority text coalesces at three items, 70% of the
safe text length, or thirty seconds since the last push. Idle with reliably no
unfinished Worker uses a two-second debounce; working, idle with Workers, and
unknown Worker status use ordinary aggregation. Worker activity comes from the
existing owner state, never the start chip or response prose. A native Final is
not task completion and does not independently trigger a push. Explicit Worker
finish/failure and Bot failure/interruption may promptly flush one combined batch.

Priority is P0 active approvals, P1 active questions, P2 results/work status,
then P3 progress/nonurgent files. Same-priority candidates are FIFO. Shared
snapshot visibility and original controller claims/lifecycle are read again
before sending, so handled/expired interactions cannot retain actionable cards.
Native questions preserve original options and operation bindings. Cards are
separate from text batches and do not wait for the ordinary timer. Higher priority
may interrupt between chunks; already reserved HTTP requests finish with their
real receipt. A complete card that cannot fit waits for a new window rather than
stranding choices; a result may use the remaining capacity in the meantime. A card
larger than a whole window gets a single non-actionable desktop entry, checked
against the same live interaction state.

Control feedback is admitted by its original notification key before the existing
controller runs. New visible outcomes survive later controls/window changes and
restart; prior notices are not replayed. Only feedback actually published by the
controller is eligible, so its ordinary silent acknowledgements remain silent.

The inbox, cursor and window are committed together. Authenticated ingress is
recorded in the existing shared IM even while runtime recovery blocks submission.
A message without a usable context token disables sends with the old token.
Unknown submissions and sends retain their original identities without replay.
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
confirmed server message ID for diagnosis, without raw error text or credentials in diagnostics; private queued bodies
are removed when their delivery completes. `ret=-2` alone is an unclassified rejection. Explicit rate
limit, context expiry, authentication and `prepare failed` remain distinct.

The local estimate is ten attempted `sendmessage` calls per distinct valid owner
message. P0/P1 may use the last slot. P3 progress and nonurgent files share at most five attempts,
and nonurgent output leaves one result opportunity. Each text part, card part or
file attempt counts once; rejected/uncertain calls still count. Duplicate updates,
invalidated queued items, polling, upload, typing and restart do not count or refill.
The owner/input/context/time/used count is durable. Old in-flight results cannot
charge a newer window. Explicit rate/context rejection, exhaustion and a local
twelve-hour cutoff pause output until fresh owner input. Legacy unknown allowance
starts closed. These are conservative local policies, not Tencent server billing,
quota-reset, expiry or length guarantees.

When full text cannot fit, the phone gets complete initial Markdown blocks labelled
as an excerpt, plus the full shared desktop conversation entry (or only that entry
if no block fits). The adapter cannot reliably infer the most valuable sentence;
it does not fabricate a summary or silently remove the middle of a result. Native
options are never abbreviated. An attempted uncertain part stops that result's
remaining parts. Accepted and unknown records, including old progress batch
memberships, are never expanded into new individual sends after restart or `/r`.
Definite rejected intents also remain terminal; a new task request is not silently
created to retry them. A tenth-slot hint is appended only when it fits.
`/help` and `/status` read local state; `/stop` uses the original native interrupt.
The adapter obtains a typing ticket from `getconfig` and renews `sendtyping` only while the shared Bot is working in an active delivery window, cancelling it at turn end. Text uses finished sendmessage payloads;
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
deferred by the estimate do not resume after new input; file status is visible in
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
Those reports are not a formal server API contract. A 2026-10-27 read of the
upstream protocol still documents context echoing and client request shapes,
not an exact reset/quota/expiry or length guarantee. The upstream
[`textChunkLimit: 4000`](https://github.com/Tencent/openclaw-weixin/blob/main/src/channel.ts)
is a client setting. Our ten-attempt window, five-progress cap, final reserve,
twelve-hour cutoff and 2,400 UTF-16/6,000-byte text caps remain conservative
local policy. They do not establish billing, successful phone receipt, or the
server counting rules for rejected requests and mixed media. Locally, each
actual text chunk/card/file `sendmessage` reservation counts once; upload, typing,
polling, duplicates and invalidated queued output do not count. Separate reports show
[`rate limited`](https://github.com/Tencent/openclaw-weixin/issues/270),
length-related [`prepare failed`](https://github.com/Tencent/openclaw-weixin/issues/284),
and possible [stale context](https://github.com/Tencent/openclaw-weixin/issues/309)
under the same broad nonzero return family. The local classifier keeps them
separate. A third-party [budget implementation](https://github.com/KCNyu/clawock/commit/0e5355ee894e9e82d057f163a0d961147922cce2)
was reviewed as a comparison, not imported as the policy authority.

## Verification boundary

Local tests cover the published request/response shapes, lossless uint64 IDs,
owner and direct-chat filtering, atomic cursor/inbox persistence, no replay of
unknown submissions or sends, shared-output freshness, budget persistence and
control priority. Virtual-clock tests cover immediate first output, count/length/time
batching, idle/working/unfinished Workers, multi-Final bursts, control resets,
partial chunks, migration, restart, in-flight window changes and file preparation.
Native socket/SQLite integration covers Worker completion and desktop output with
one unchanged Weixin window. Other channels use the unchanged shared contracts. A separate unauthenticated request to the
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
