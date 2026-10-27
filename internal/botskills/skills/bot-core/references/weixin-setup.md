# Help the user connect Weixin

Weixin is a private text entry to the same Bot conversation. Guide pairing in
**Settings → Messaging → Weixin**. The user scans the code, confirms on the
phone, and confirms the masked account on the Mac. If Weixin asks for a phone
verification code, the user enters it in Settings. Do not request the code,
pairing screenshot, or token in chat.

The channel sends merged progress, native approval and question cards, and the
final reply for the current Weixin input. A new user message closes the older
reply interval; messages from the Mac or Telegram stop prior Weixin delivery.
Internal Worker notices do not change that user boundary. Older queued replies
never borrow the allowance of a new phone message. Reply files already exposed
as Bot artifacts can be sent as separate file messages. Do not turn a file path in
prose into an attachment or imply that an arbitrary local file was shared.
It does not mirror the Mac or Telegram conversation history. The phone may show
typing during a main Turn; typing does not renew its send allowance. HTTP
acceptance does not prove phone display, for text or files.

The user can send `/r` or `/refresh` to refresh the receive allowance without
starting another model request. This does not replay old queued progress.
`/status` reads current state, `/help` lists commands, and `/stop` requests the native interruption. `/approve` and
`/answer` address the original pending Runtime request. If text delivery is
deferred or rejected, retain the complete result in the shared conversation.
A long final reply is split at Markdown boundaries when the remaining allowance
can hold all parts; otherwise the phone receives a notice directing the user to
the complete desktop result or a request for specific content. Do not silently
replace a critical result with a head/tail excerpt. Ask what content the user
needs next rather than promising that `/r` will replay text or files. If a
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
