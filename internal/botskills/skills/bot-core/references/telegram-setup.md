# Set up and use Telegram

Use Settings → Messaging → Telegram for the same Bot conversation. Keep tokens
and complete pairing links out of chat, notes, commands, tool arguments and logs.
The user pastes the token into the masked settings field. Confirm success only
when the app reports Connected.

## Pair

1. Open the verified [@BotFather](https://t.me/BotFather); check the exact username
   and verified badge. Send `/newbot`, choose a display name and a unique username
   ending in `bot`. These names do not change Caelis Bot's identity.
2. Copy the issued token directly to settings and select Connect.
3. Use Open in Telegram and Start through the complete short-lived pairing link.
   If needed, use Copy complete pairing link and open it privately in a browser.
   A plain Bot URL or username search cannot identify the pairing attempt.
4. Confirm the displayed account in Caelis Bot settings. Receiving Start alone
   does not authorize it.

Offer one short step at a time when needed. No server, webhook or command-line
setup is required. For an existing Bot, stop its previous app's Telegram connection
and retrieve its token through BotFather `/mybots`. Do not revoke or rename unless
asked. Switch this Bot to Caelis Bot replaces an existing webhook and discards its
pending messages; leave that explicit choice to the user. Stop a competing poller
before Try connecting again.

## Use

Text, images, stickers and files go through the paired private chat; incoming files
are limited to 8 MB. Animated/video stickers supply a preview frame when available;
see [Received media](received-media.md). Replies stream and may span messages.
Desktop messages/attachments also appear; pairing does not copy old private history.

`/status` checks availability; `/stop` interrupts current work. Native decisions
use the exact offered card/buttons or commands in [Interactions](interactions.md).
Keep marked secrets in native fields. Some logins or OS prompts need the desktop.
Keep Bot running and the host online; closing chat does not stop it, but sleep
or disconnection delays reception.

## Diagnose setup or delivery

Follow the current state's action: Connecting, Waiting for pairing, Confirm your
account, Connected, Paused, Reconnecting or Needs attention. A waiting screen is
not proof of expired pairing or failed polling. Preserve unresolved attempts;
refresh an expired link through Try connecting again, not repeated token submission.

For network problems, inspect host internet/proxy/VPN availability. The app uses
system proxy choices, including PAC; system direct routing does not disable a VPN.
Do not change/restart network software unless authorized. Unblock a blocked Bot
in Telegram; re-enter an unreadable saved token only in settings.

During reconnection, keep original messages queued rather than asking for resends.
If a completed reply is missing on Telegram, retain the result and inspect the
original delivery. Updates do not backfill rejected/unknown sends. Help read the
reply on the desktop; do not repeat the work to repair delivery. Use
[Recovery](recovery.md) for uncertain tasks or actions.

Pause retains pairing and token. Remove connection deletes this app's pairing and
saved token after confirmation; it does not delete the Telegram Bot.
