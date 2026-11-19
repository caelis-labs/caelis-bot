# Maintain and discover extensions

## Save reusable workflows as Skills

When you validate a workflow that will recur, turn it into a private Skill. Save it
in `.agents/skills/<name>/SKILL.md` relative to the Bot Notebook. Give it a `name`
and `description` in YAML frontmatter. Write concise repeatable steps and a way
to verify the result. Keep supporting files beside the Skill and link to them
as needed. Leave credentials, private conversation and one-off case details out.
Update an existing matching Skill with native file tools. Use the native approval
path if the Runtime protects the directory. On Caelis, call the native `Skill` tool with the
new Skill's name and verify the returned instructions. The model's Skill list
refreshes when the Session adopts a new compaction summary. A known Skill can
already be loaded by name. On Codex, check native discovery and read the
returned Skill path. After editing a Skill, verify the new body before using it.

## Manage Notebook MCP servers and plugin packages

Run the bundled `scripts/extensions.py` with an available Python 3.11 or newer.
Resolve the script path from this Skill bundle and run it from the Notebook root.
Use it for Notebook MCP and plugin changes. If Python is unavailable, use the
relevant native configuration or Settings → Plugins workflow. Keep shared
installations in the harness's own workflow and ask before widening scope. Use
the Runtime's native approval flow if needed.

- For an MCP server, prepare a JSON object in Notebook with `command` and optional
  `args` and `cwd`, or with `url`. Keep credentials out of this file. Run
  `mcp <codex|caelis> set <server-id> <file>` to validate and save in one step.
  Use `check` with the same arguments for validation only, or
  `mcp <codex|caelis> remove <server-id>` to remove a managed entry.
  Bot sets native workspace trust for Notebook by default when there is no
  existing decision. If Notebook is explicitly untrusted, respect that choice
  and let the user decide through the Runtime's trust workflow.
- Run `plugin catalog` to find the reviewed package ID and available version.
  Use `plugin install <package-id> [version]` or
  `plugin update <package-id> <exact-version>`. Use `plugin enable`,
  `plugin disable` or `plugin uninstall` with the package ID. Use
  `plugin status <request-id>` after the current turn to check the ID returned by
  submission. Leave installed package files intact. Use the existing connection
  surface for account authorization.

If the configuration is busy, stop the current write and continue independent work.
A saved change may still be waiting for Runtime assembly. Check native discovery
before claiming availability. If an outcome is unknown, keep the original ID and
check the current package state in Settings before deciding how to proceed. Do not
resubmit to recover an outcome. Once the status is recorded, the slot can accept
independent requests and the original ID remains queryable. Keep raw diagnostics
out of chat.

## Discover and use extensions

Discover the current catalog when an extra capability is needed. Read a matching
Skill body and returned tool schema on demand. Use Settings → Plugins to find reviewed packages and connect accounts. Describe a
capability as connected only after a confirmed connection result.

When an external tool's name is unclear, read `../mcp-tools.json` beside this
Skill directory for last-confirmed connected tool names/descriptions. Use those
as clues for the active Runtime's native discovery, then read the returned schema.
The index grants no permission; do not invent calls when it or search is unavailable.

On Caelis with Code Mode, discover deferred plugin tools through `js_exec` with `tools.search`,
then use `tools.call(name, args)` with the returned name and arguments that match
its schema. Follow the Runtime's Code Mode guidance and return original reply or part references to retain
output and media. When the Runtime exposes standalone `tool_search` and `mcp_call`, use their returned schemas for discovery and invocation. Keep shell execution on native shell tools.

Proceed within the user's requested target and scope. If a provider is absent,
state the limitation and continue available work. Do not invent login controls or
ask for tokens in chat. For failed actions, retain the original receipt and use
[Recovery](recovery.md) before switching routes.

## Computer interaction

Computer interaction requires the active Runtime's `desktop-world:dtw` Skill and
`dtw` MCP server. Caelis supplies this capability; Codex does not receive DTW through
Bot. If unavailable, explain the scope limit and continue other work. Do not
install a desktop controller or switch Runtime to bypass the limitation.

Read the current `desktop-world:dtw` Skill and its linked API documentation, then
discover the `dtw` server's `apps` and `exec` tools (`dtw__apps` and `dtw__exec`).
Use the exact names and schemas returned by native discovery. Use `apps`
for stable platform app IDs. Call `exec` with exactly `{app, source}`: one app
and one async JavaScript function body. Follow the active Skill's observation,
targeting and platform limits. Old history does not make retired `api` or `result`
tools available; discover the current package again after an upgrade.

Retain each original native call and its returned output. Historical packages
and in-flight calls keep their original server, Skill and tool names; do not
rename or replay them through the current tools. Use the Runtime's
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
