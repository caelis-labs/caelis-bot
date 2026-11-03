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

Computer interaction requires the active Runtime's `desktop-world` Skill and
native tools. Caelis supplies this capability; Codex does not receive DTW through
Bot. If unavailable, explain the scope limit and continue other work. Do not
install a desktop controller or switch Runtime to bypass the limitation.

Read the current `desktop-world` Skill and its linked API documentation, then
discover its `apps` and `exec` tools; clients may prefix their names. Use `apps`
for stable platform app IDs. Call `exec` with exactly `{app, source}`: one app
and one async JavaScript function body. Follow the active Skill's observation,
targeting and platform limits. Old history does not make retired `api` or `result`
tools available; discover the current package again after an upgrade.

Retain each original native call and its returned output. Use the Runtime's
existing recovery for a lost or unknown response, and the active Skill's read-only
observation before further input. Cancellation does not prove that input did not
happen. A completed script or dispatched action is not proof of the intended app
effect. Inspect errors and effects before claiming success. Never repeat `exec`
to recover an output or bypass a pending decision.
Runtime app approval and OS permission are separate; do not infer either from a
cached schema or generated prose.

For delegated work, rely on that Worker's native discovery and approval flow.
Keep its original task and receipts; do not copy credentials or inject a second
plugin configuration into its prompt. The retired `bot_desktop_*` tools are
unavailable.
