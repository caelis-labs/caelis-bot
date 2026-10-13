# Help the user connect Weixin

Weixin is a private text entry to the same Bot conversation. Guide pairing in
**Settings → Messaging → Weixin**. The user scans the code, confirms on the
phone, and confirms the masked account on the Mac. If Weixin asks for a phone
verification code, the user enters it in Settings. Do not request the code,
pairing screenshot, or token in chat.

The channel sends merged progress, native approval and question cards, and the
final reply for work started from Weixin. Reply files already exposed as Bot
artifacts can be sent as separate file messages. Do not turn a file path in
prose into an attachment or imply that an arbitrary local file was shared.
It does not relay work started from the Mac or Telegram. The phone may show
typing during a main Turn; typing does not renew its send allowance. HTTP
acceptance does not prove phone display, for text or files.

The user can send `/r` or `/refresh` to resume delivery of unsent progress
without starting another model request. `/status` reads current state, `/help`
lists commands, and `/stop` requests the native interruption. `/approve` and
`/answer` address the original pending Runtime request. If text delivery is
deferred or rejected, ask the user to send `/r` and check the original result.
`/r` resumes text progress, not a backlog of files. If a
file was deferred for budget, keep its readable name in the final reply and
invite the user to name the specific file they want. Expose that same owned
artifact in the new reply only for the requested file. A file whose send
result is unknown must not be replayed automatically. The Weixin connection
status on the Mac distinguishes an unavailable file, upload failure, deferred
file and uncertain or rejected file message. Ask the user to check the original
artifact and the phone before claiming receipt.

Weixin currently accepts one paired user's private text. Incoming attachments,
group messages and quoted-message authority are outside this channel. Pausing
keeps pairing; removing the connection deletes local credentials but does not
revoke authorization inside Weixin.
