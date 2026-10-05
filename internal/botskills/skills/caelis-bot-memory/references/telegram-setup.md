# Help the user connect Telegram

Treat Telegram as another place to use the same assistant and conversation.
Help a beginner complete the short setup in **Settings → Connections → Telegram**.
Do not ask for a token in conversation or put one in notes, files, commands, screenshots,
logs, or tool arguments. The user pastes it into the masked Bot Token field; the app
stores it in the macOS Keychain. Never claim setup succeeded until the app says Connected.

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

- Send text, a photo, or a file to the paired private chat. Incoming files are
  limited to 8 MB each so both supported runtimes can accept them. The Bot uses
  its existing tools and capabilities to interpret attachments.
- Replies update progressively in Telegram. Long replies span multiple messages.
  Desktop messages and attachments also appear there. Earlier private history is
  not copied when an account is first paired.
- `/stop` stops current work; `/status` reports whether the Mac is online and busy.
  Simple approval choices can be answered using Telegram buttons. Requests needing
  forms, secrets, or external login direct the user to the Mac.
- Keep Caelis Bot running and the Mac online. Closing its conversation window
  does not stop the connection. A sleeping or offline Mac cannot receive messages
  until it reconnects.
- If the pairing link expires, click Try connecting again. Only the account
  confirmed on the Mac is allowed to send work to the assistant.
- For network errors, check internet access and the Mac's proxy/VPN. For a blocked
  Bot, unblock it in Telegram. For Keychain access errors, allow Caelis Bot access
  or paste the token again in settings.
- After restarting the Mac app, messages wait until its existing conversation is
  recovered. Do not ask the user to resend while it is reconnecting. Sending a
  large outgoing attachment does not prevent `/stop`, `/status`, or approval choices.
- The app uses the Mac's system proxy, including automatic configuration (PAC).
  If automatic discovery fails, it follows the Mac's remaining connection choices.
  A system-allowed direct connection still follows VPN/TUN routing, including
  FlClash; it does not turn off those apps. If all choices fail, help the user check
  the proxy/VPN. Do not change or restart it unless asked, or expose the token to
  diagnose connectivity.
- An uncertain delivery is not permission to repeat it. Inspect the original
  message on the Mac and Telegram before deciding whether to send anything again.
- Pause retains the saved pairing and token. Remove connection clears the pairing
  and deletes this app's saved token. It does not delete the Telegram Bot.
