# Help the user connect Weixin

Weixin is a private entry and additional delivery endpoint for the same Bot
conversation. Guide pairing in **Settings → Messaging → Weixin**. The user scans,
confirms on the phone, then confirms the masked account on the Mac. Phone
verification codes belong in Settings; never request codes, tokens or pairing
screenshots in chat.

Each distinct valid phone text, including `/r`, `/answer` and `/approve`, refreshes
the local receive allowance and context. Desktop or Telegram input does not close
or renew that window. New shared Bot/Runtime/Worker output can use the remaining
allowance regardless of which entry started the work. Private Worker transcripts
and old conversation history are not copied into Weixin.

The first meaningful output is prompt; later short messages and stage results are
combined. Active approvals precede questions, then results/status, then progress
and nonurgent files. Native options and original action bindings stay intact.
Already handled or expired interactions are not sent as actionable cards. A
Runtime Final is not proof that all Worker work has ended.

`/r` or `/refresh` restores known missed important results without starting a
model request or rerunning work. It does not replay accepted, rejected or unknown
sends, or old ordinary progress. Normal new input drops old queued progress and
nonurgent files; still-current important results may continue, while results held
by an exhausted/paused window need `/r`. `/status` reads local state, `/help` lists
commands, and `/stop` requests native interruption. `/approve` and `/answer` use
the original pending request.

The ten-send estimate, length caps and safety expiry are local conservative
policy, not a guaranteed server quota or charging rule. Every attempted text part,
card part or file message counts, including rejection/uncertainty. Upload and
typing do not count. HTTP acceptance does not prove phone display. A new message
cannot change the actual receipt of an old in-flight attempt.

Markdown chunks are complete when the allowance can fit the reply. Otherwise the
phone receives explicitly labelled complete-block excerpts and a route to the full
shared desktop conversation. Do not treat excerpts as summaries or claim all key
results fit. Keep useful conclusions clear in the actual reply. Native action
options are never shortened. Files must be existing Bot-owned Artifacts, not paths
in prose. Deferred file names remain readable; ask which file is needed and expose
that same owned Artifact in the new reply. Unknown file delivery must not replay.
Settings distinguishes preparation, unavailable/upload failures and uncertain or
rejected message delivery. Check original receipts before claiming delivery.

One paired user's private text is supported. Groups, incoming attachments and
quoted-message authority are outside this channel. Pausing retains pairing;
removal deletes local credentials, not server-side authorization.
