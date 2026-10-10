# Help the user connect Telegram

Treat Telegram as another place to use the same assistant and conversation.
Help a beginner complete the short setup in **Settings → Messaging → Telegram**.
Messaging is the channel overview; it shows the actual Telegram connection state.
The Telegram detail has a return link to that overview. AI accounts and models
are separate settings destinations. Older direct Telegram links still open the detail.
Do not ask for a token in conversation or put one in notes, files, commands, screenshots,
logs, or tool arguments. The user pastes it into the masked Bot token field; the app
stores it in its private local credential directory. Never claim setup succeeded until the app says Connected.

## Create a Bot with BotFather

1. Open [the verified @BotFather](https://t.me/BotFather) in Telegram. Check its
   verified badge and exact username; similarly named accounts are not BotFather.
2. Send `/newbot`. BotFather first asks for a display name. Suggest a simple name
   the user likes; it does not change their Caelis Bot identity.
3. Choose a unique username ending in `bot`, such as `MyCaelisHelperBot`. If taken,
   add a short personal suffix and try again.
4. BotFather gives an API token. Ask the user to copy it directly into the Bot Token
   field in Caelis Bot settings and click Connect. They should not send it to you.
5. Click Open in Telegram and tap Start in the Bot chat. Return to Caelis Bot and
   confirm the displayed Telegram account is theirs. This completes pairing.

Give one short step at a time when the user needs help. Use the app's Open BotFather
and Open in Telegram buttons where possible; the user does not need a server,
public URL, chat ID, webhook, or command-line setup.

## Use an existing Bot

An existing Bot can be used. Ask the
user to stop its Telegram connection in the previous app first. In BotFather,
send `/mybots`, select the Bot, and choose API Token, then follow steps 4–5 above.
Do not revoke its token or change its name unless the user asks. If Caelis Bot
reports an existing webhook, explain that Switch this Bot to Caelis Bot disconnects
the previous service and discards its pending messages. Let the user choose that
explicit setup action. If another app is polling, stop that app's connection and
click Try connecting again; do not keep reconnecting competing clients.

## Use and troubleshoot

- Send text, a photo, a sticker, or a file to the paired private chat. Incoming files are
  limited to 8 MB each so both supported runtimes can accept them. The Bot uses
  its existing tools and capabilities to interpret attachments. Animated and
  video stickers arrive as a single preview frame when one is available.
- Replies update progressively in Telegram. Long replies span multiple messages.
  Desktop messages and attachments also appear there. Earlier private history is
  not copied when an account is first paired.
- `/stop` stops current work; `/status` reports whether the Mac is online and busy.
  An approval with Runtime-provided choices, including Computer Use, can be answered
  using its Telegram buttons. Show the choices actually offered for that request;
  a session or lasting permission is available only when the Runtime offers it.
  Requests needing forms, secrets, or external login direct the user to the Mac.
  An old button may expire after a decision, reconnect, or change of task. If a
  decision's result is uncertain, check the original request on the Mac; do not
  tap a different option to repeat it.
- Keep Caelis Bot running and the Mac online. Closing its conversation window
  does not stop the connection. A sleeping or offline Mac cannot receive messages
  until it reconnects.
- If the pairing link expires, click Try connecting again. Only the account
  confirmed on the Mac is allowed to send work to the assistant.
- Distinguish Not set up, Connecting, Waiting for pairing, Confirm your account,
  Connected, Paused, Reconnecting and Needs attention. A waiting screen or a
  successful status read is not pairing confirmation. Follow the single action
  shown for the current state; do not repeatedly submit a token or request a new
  link while the original connection action is unresolved.
- For network errors, check internet access and the Mac's proxy/VPN. For a blocked
  Bot, unblock it in Telegram. If the saved credential cannot be read, paste the
  token again in settings.
- After restarting the Mac app, messages wait until its existing conversation is
  ready for native input. Local chat messages remain visible independently. Do not ask the user to resend while it is reconnecting. Sending a
  large outgoing attachment does not prevent `/stop`, `/status`, or approval choices.
- The app uses the Mac's system proxy, including automatic configuration (PAC).
  If automatic discovery fails, it follows the Mac's remaining connection choices.
  A system-allowed direct connection still follows VPN/TUN routing, including
  FlClash; it does not turn off those apps. If all choices fail, help the user check
  the proxy/VPN. Do not change or restart it unless asked, or expose the token to
  diagnose connectivity.
- An uncertain delivery is not permission to repeat it. Inspect the original
  message on the Mac and Telegram before deciding whether to send anything again.
- If a completed reply is visible on the Mac but missing in Telegram, keep the
  original work result. An app update does not automatically resend older
  rejected or uncertain Telegram deliveries. Help the user read that reply on
  the Mac; continue the conversation in Telegram with a new user message only
  when they choose to do so. Do not repeat the original work to repair the chat.
- Pause retains the saved pairing and token. Remove connection clears the pairing
  and deletes this app's saved token after an explicit confirmation. It does not
  delete the Telegram Bot.

A Runtime disconnect or a failed background subscription does not mean the Bot
app stopped. Keep helping the user through available controls. `/status` and
reconnect/approval buttons provide receipt feedback while native work is checked.
Treat an unconfirmed background task as that task's uncertainty; read its original
handle and receipt rather than creating a replacement. A recent-message sync or
attachment display failure is not evidence that a native task failed. Never ask
the user to delete a Session or replay an action to clear a display error.
