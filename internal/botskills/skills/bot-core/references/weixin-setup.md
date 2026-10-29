# Set up and use Weixin

Use Settings → Messaging → Weixin. The user scans, confirms on the phone and then
confirms the masked account on the host. Keep verification codes in settings;
never request codes, tokens or pairing screenshots in chat.

Weixin provides one paired user's private text entry and delivery window for the
same Bot conversation. Groups, incoming attachments and quoted-message authority
are unsupported. Pause retains pairing; removal deletes local credentials, not
server authorization.

## Commands and delivery

Each distinct valid phone text, including control commands, refreshes the local
window. Desktop/Telegram inputs neither close nor renew it. New shared output can
use remaining allowance regardless of its initiating entry; private Worker
transcripts and old history are not mirrored.

- `/r` or `/refresh`: continue retained unattempted reply parts and eligible recent
  progress; no model request or work replay.
- `/status`, `/help`: read local state or commands.
- `/stop`: request native interruption.
- `/approve`, `/answer`: decide the original pending request; see
  [Interactions](interactions.md).

First meaningful output is prompt; later short output is combined. Approvals,
questions and useful results take priority over progress/files. Native options
remain intact; a Runtime Final does not prove all Worker work is finished.

New input drops queued ordinary progress and nonurgent files. Important current
results may continue; results held by a paused/exhausted window need `/r`. That
command can recover progress from at most 128 current shared IM items within
twelve hours and since channel observation began. It does not traverse older
history or replay accepted, rejected or unknown sends.

The ten-send estimate and safety expiry are local policy, not guaranteed server
quota or billing. Each attempted text/card/file part counts, including rejection
or uncertainty; upload/typing do not. Acceptance does not prove phone display.

## Long replies and files

When full text cannot fit, delivery uses labelled complete-block excerpts with a
route to the full desktop conversation. An excerpt is neither a summary nor proof
that all results arrived. `/r` continues retained original parts without replaying
the preview. Use the desktop for bodies beyond the delivery cache.

Files require existing Bot-owned Artifact handles, not paths in prose. If an
unattempted file was deferred, identify the needed file and expose that same owned
Artifact in a new reply. Inspect settings and original receipts for preparation,
upload and message outcomes. Unknown file/text delivery must not replay; use
[Recovery](recovery.md).
