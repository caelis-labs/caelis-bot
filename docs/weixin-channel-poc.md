# Weixin Remote Channel POC

This local POC uses the HTTP JSON protocol described by
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
the account IDs, cursor, queued private text, context tokens, ingress receipt
states and outbound intent ledger. The UI does not receive tokens or message
contents from this state. The client sends the upstream start/stop presence
notifications when its connection starts or stops. Pausing cancels polling and
sending while retaining pairing. Removing deletes local state and the local
token; the protocol does not provide a verified server-side revocation operation.
The upstream `-14` stale-session response enters a durable one-hour cooldown,
then resumes polling without discarding the pairing. Bounded outbound retry
follows the saved attempt counter after restart.

`getupdates` is the only input transport. Text from the confirmed owner in a
direct chat enters `backend.SubmitRemote` with the original stable request ID.
Text `/approve` and `/answer` commands use the shared native decision controller,
including while the Bot is waiting on the original request. Each native option
has a complete numbered command. Free text is accepted only for a question that
supports it; a custom opinion on a fixed approval never grants permission.
Telegram buttons call that same controller. Desktop and both companion channels
remain one Bot conversation. Visible user messages, final assistant messages,
text approval cards, terminal notices and control receipts are mirrored through
each transport's independent delivery ledger. An original Weixin input is not
echoed back to its own chat. Pairing and legacy records baseline existing history.
The inbox and cursor are saved together before dispatch. A dispatching request
that loses its result is not sent again automatically. Completed assistant text
is sent with `sendmessage` and a context token. A reply within 2,400 UTF-16
units and 6,000 UTF-8 bytes stays in one message. Longer replies split at
Markdown blocks and natural boundaries; a complete fenced code block stays
together when it fits, while an oversized fence is closed and reopened across
messages. These are conservative client caps, not a published iLink server
limit. An outbound intent is saved
before HTTP dispatch, including its exact text digest, context token, stable client ID, and
attempt count. A timeout or failed HTTP response is marked uncertain and
retried after 2 and 8 seconds, at most three total attempts. The same client ID
is used, but the upstream server does not document deduplication; duplicate
phone replies remain possible. Unsent later chunks wait for this result. A successful HTTP JSON
response with absent `ret` follows the published client's success handling;
it does not prove the phone displayed it. The private outbound ledger stores a
bounded result category for later diagnosis, without reply text or credentials.
The adapter obtains a typing ticket
from `getconfig` and renews `sendtyping` only while the paired user's main turn
is active, cancelling it at turn end. Text still sends only as final messages;
the upstream text builder uses `message_state=FINISH`, and the public protocol
does not document safe in-place edits. Text cards and terminal status therefore
arrive as separate messages. Native approval buttons, groups, attachments, edits,
webhook delivery,
proactive window guarantees and Windows credentials are outside this POC.
An HTTP success cannot prove phone display, especially for a proactive send
without a recent context token.

Tencent's protocol document describes its current client and expressly does
not claim to be the full server contract. The MIT source license does not
itself grant a supported independent client integration or backend service
terms. Upstream service changes can break this adapter; recheck each protocol
version before wider release.

## Verification boundary

Local tests cover the published request/response shapes, lossless uint64 IDs,
owner and direct-chat filtering, atomic cursor/inbox persistence, no replay of
unknown submissions, and bounded unknown outbound retries. A separate unauthenticated request to the
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
resolved, and typing visibility, cooldown recovery, removal, unknown-send
deduplication and other accounts have not received live acceptance.
