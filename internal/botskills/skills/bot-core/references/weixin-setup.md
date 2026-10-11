# Help the user connect Weixin

Weixin is a private text entry to the same Bot conversation. Guide pairing in
**Settings → Messaging → Weixin**. The user scans the code, confirms on the
phone, and confirms the masked account on the Mac. If Weixin asks for a phone
verification code, the user enters it in Settings. Do not request the code,
pairing screenshot, or token in chat.

The channel sends merged progress, native approval and question cards, and the
final reply for work started from Weixin. It does not relay work started from
the Mac or Telegram. The phone may show typing during a main Turn; typing does
not renew its send allowance. HTTP acceptance does not prove phone display.

The user can send `/r` or `/refresh` to resume delivery of unsent progress
without starting another model request. `/status` reads current state, `/help`
lists commands, and `/stop` requests the native interruption. `/approve` and
`/answer` address the original pending Runtime request. If an estimate is
exhausted or the service rejects a send, ask the user to send `/r` and check
the original result; uncertain sends must not be replayed automatically.

Weixin currently accepts one paired user's private text. Group messages,
attachments, and quoted-message authority are outside this channel. Pausing
keeps pairing; removing the connection deletes local credentials but does not
revoke authorization inside Weixin.
