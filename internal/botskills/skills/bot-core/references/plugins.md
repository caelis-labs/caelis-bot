# Plugins

The user manages reviewed packages in Settings → Plugins. A package can provide
Skills, tools, or both. Installation and enable state are separate from a service
account connection. Describe only the current installed and working capability;
do not describe a package as connected without a confirmed connection result.

When the user asks to use an extra capability, discover the current catalog.
Read a matching Skill body only when the task needs it. If its tool is unavailable
or fails, keep the original action receipt and use healthy tools for independent
work. Do not repeat a possibly accepted external action through another route.

Desktop World is the built-in computer-use plugin. It is initially enabled and
can be disabled in Settings → Plugins. Before operating the desktop, confirm it
is enabled, load its `desktop-world` Skill on demand, and discover the current
MCP `exec` schema through the active Runtime. Use the Skill's `cua` script API
and its linked references for exact UI operations and receipt recovery. The
old `bot_desktop_*` tools are retired. Omit the `session` argument: Desktop World
owns one execution context per MCP process. Execution IDs may be short; retain
the exact original ID and use the same originating MCP connection when fetching
status, result, images or cancellation. Reconnection does not recreate a receipt.
A completed execution receipt proves delivery, so read back the intended UI effect
before reporting success. A loaded schema from an older session is
not proof that a disabled plugin can still run. Follow Runtime tool approval
and operating-system permission decisions separately; a package being enabled grants
neither one.

On Windows, inspect the original receipt and current UI before retrying an input
that reports a foreground error; the input may already have taken effect. Follow
the plugin's restrictions for DPI and modal dialogs. Do not issue guessed pixel
coordinates when screenshot and window geometry disagree, or try to work around
a pending execution by opening another computer-use context.

When an external tool is relevant but its exact name is unclear, read
`../mcp-tools.json` beside this Skill's directory. It lists only tools last
confirmed from connected plugin services. Use its names and descriptions as
clues for native ToolSearch, then read the returned schema before calling.
The index is neither a schema nor a grant. If it is absent or search is
unavailable, do not invent a tool call.

For GitHub, use only tools confirmed available on the current connection. Before
creating or changing repository content, an issue, or a pull request, identify
the exact repository and proposed change for the user's review. Keep the native
approval decision and original tool receipt; an unknown write result must be
reconciled before another attempt. Connection alone does not authorize every
write or prove access to every repository.

The available reviewed catalog may be small. If a requested provider is absent,
say so plainly and help with work that is possible now. Do not invent a login
button, ask the user to paste a token in chat, or claim that installing a guide
grants access to Gmail, GitHub, or other accounts.
