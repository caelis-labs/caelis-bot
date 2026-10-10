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
`ilink_user_id` are accepted. The bearer token is in macOS Keychain under a
root-specific account. `weixin.json` is a private atomic state file containing
the account IDs, cursor, queued private text, context tokens, ingress receipt
states and outbound intent ledger. The UI does not receive tokens or message
contents from this state. The client sends the upstream start/stop presence
notifications when its connection starts or stops. Pausing cancels polling and
sending while retaining pairing. Removing deletes local state and the local
token; the protocol does not provide a verified server-side revocation operation.
The upstream `-14` stale-session response enters a durable one-hour cooldown,
then resumes polling without discarding the pairing or replaying uncertain sends.

`getupdates` is the only input transport. Text from the confirmed owner in a
direct chat enters `backend.SubmitRemote` with the original stable request ID.
The inbox and cursor are saved together before dispatch. A dispatching request
that loses its result is not sent again automatically. Completed assistant text
is sent with `sendmessage` and a context token; chunks are at most 1,200 UTF-8
bytes and prefer whitespace or sentence boundaries. An outbound intent is saved
before HTTP dispatch. A timeout or failed HTTP response is marked uncertain,
and that chunk and later chunks are not replayed. A successful HTTP JSON
response with absent `ret` follows the published client's success handling;
it does not prove the phone displayed it. The adapter obtains a typing ticket
from `getconfig` and renews `sendtyping` only while the paired user's main turn
is active, cancelling it at turn end. Text still sends only as final messages;
the upstream text builder uses `message_state=FINISH`, and the public protocol
does not document safe in-place edits. Approvals, groups, attachments, edits,
webhook delivery,
proactive window guarantees and Windows credentials are outside this POC.

Tencent's protocol document describes its current client and expressly does
not claim to be the full server contract. The MIT source license does not
itself grant a supported independent client integration or backend service
terms. Upstream service changes can break this adapter; recheck each protocol
version before wider release.

## Verification boundary

Local tests cover the published request/response shapes, lossless uint64 IDs,
owner and direct-chat filtering, atomic cursor/inbox persistence, and unknown
submission/send no-replay behavior. A separate unauthenticated request to the
official `get_bot_qrcode?bot_type=3` endpoint verifies only handshake
reachability and response shape. It does not verify account pairing, receiving
a real Weixin message, or the phone seeing a reply. Those require the user's
manual scan and a Dev Bot running with its isolated data directory.
