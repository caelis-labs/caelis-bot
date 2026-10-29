# Discover and use plugins

Discover the current catalog when an extra capability is needed. Read a matching
Skill body and returned tool schema on demand. Settings → Plugins manages package
installation and enable state; account connection is separate. Describe a
capability as connected only after a confirmed connection result.

When an external tool's name is unclear, read `../mcp-tools.json` beside this
Skill directory for last-confirmed connected tool names/descriptions. Use those
as native ToolSearch clues, then read the returned schema. The index grants no
permission; do not invent calls when it or search is unavailable.

Proceed within the user's requested target and scope. If a provider is absent,
state the limitation and continue available work. Do not invent login controls or
ask for tokens in chat. For failed actions, retain the original receipt and use
[Recovery](recovery.md) before switching routes.

## Computer interaction

Confirm the built-in Desktop World plugin is enabled, read its `desktop-world`
Skill and relevant browser/platform guidance, and discover the active MCP `exec`
schema. Use its `cua` API and current discovery/`next_call` results. Omit `session`;
the plugin owns one execution context per MCP process. Retain exact execution IDs
and use the originating connection for status, results, images or cancellation.
The retired `bot_desktop_*` tools are unavailable.

Read back the intended UI effect before claiming success. Plugin enablement,
Runtime approval and OS permissions are separate. A cached schema does not prove
a disabled plugin is usable. After `runtime_context_reset`, observe fresh handles;
it does not renew grants or settle uncertain effects. Never replay an action to
rebuild context or open another context to bypass pending execution.

On Windows, inspect the original receipt and UI before retrying foreground errors;
input may already have taken effect. Follow the plugin's DPI/modal restrictions.
Do not guess pixels when screenshot and window geometry disagree.
