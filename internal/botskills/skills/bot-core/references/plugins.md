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

Read the current `desktop-world` Skill, then discover its `apps`, `api`, `exec`
and `result` tools; clients may prefix their names. Use `apps` for stable platform
app IDs and `api` for exact method schemas. Call `exec` with exactly `{app, source}`:
one app and one async JavaScript function body. Preserve returned refs as opaque
values and follow the active Skill's lifetime and platform limits.

Retain each original execution receipt and read it through `result` on the same
originating MCP connection. Cancellation or an unavailable/unknown result is not
proof that input did not happen. Inspect the record's error and effect ledger
even when status is `completed`. Never repeat `exec` to recover a receipt or
bypass a pending decision. Verify the intended app effect before claiming success.
Runtime app approval and OS permission are separate; do not infer either from a
cached schema or generated prose.

For delegated work, rely on that Worker's native discovery and approval flow.
Keep its original task and receipts; do not copy credentials or inject a second
plugin configuration into its prompt. The retired `bot_desktop_*` tools are
unavailable.
