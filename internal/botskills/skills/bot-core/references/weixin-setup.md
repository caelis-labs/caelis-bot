# Help the user connect Weixin

Treat Weixin as another entry to the same assistant and conversation. The current
channel accepts private text from one paired account and sends final text replies.
Use **Settings → Messaging → Weixin** to guide setup. The user scans the pairing
code, which refreshes automatically while the settings page is open, and confirms
the account on their phone. Never operate Weixin for them,
request a screenshot of the code, or ask for a bot token in conversation.

After the phone confirms, the Mac asks the user to confirm the masked account.
Only then is the connection saved. Keep the Mac and Caelis Bot running. If a
phone verification code is requested, the user enters it in the Weixin settings
field; do not ask them to send it in chat.

The channel may show a typing indicator while a paired user's main turn is
active, but it sends only final text replies and does not edit streamed text.
The Weixin channel does not currently accept groups, attachments, commands, or
remote approval actions. Handle ordinary text requests and Worker coordination
through your existing tools. If a request needs an approval, guide the user to
Caelis Bot on the Mac. Do not promise that a sent reply was displayed on the
phone merely because the server accepted the send operation.

If a reply send is uncertain, the channel retries that same reply at most twice
more. Duplicate replies can appear. After the three-attempt limit, ask the user
to check Weixin and the Mac. An uncertain incoming request or Worker action is
not submitted again; use its original receipt. Pausing stops polling and
keeps local pairing. Removing the connection deletes local credentials; it does
not revoke authorization inside Weixin. The user handles any phone-side removal.
If settings reports a temporary session cooldown, explain that polling will
resume automatically and the user need not pair again yet.
