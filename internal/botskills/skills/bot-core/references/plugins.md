# Maintain and discover extensions

## Save reusable workflows as Skills

When you validate a workflow that will recur, turn it into a private Skill. Save it
in `.agents/skills/<name>/SKILL.md` relative to the Bot Notebook. Give it a `name`
and `description` in YAML frontmatter. Write concise repeatable steps and a way
to verify the result. Keep supporting files beside the Skill and link to them
as needed. Leave credentials, private conversation and one-off case details out.
Update an existing matching Skill with native file tools and verify its current
body before using it. On Codex, use native discovery and read the returned path.
Catalog metadata can lag a file edit. If the listing is stale, check again in a
later turn. On Caelis, load a known name directly with the native `Skill` tool.
The model's Skill list refreshes when the Session adopts a new compaction
summary. Remove only a private Skill you own or the user has named for removal,
then confirm its absence through native discovery.

## Manage Notebook MCP servers and plugin packages

Check `python3 --version`. Run the installed `scripts/extensions.py` from the
Bot Notebook root with Python 3.11 or newer. If `python3` is older, use the full
path to a compatible interpreter. Resolve the script path from this Skill bundle.
Use the Runtime's native approval flow for protected writes. If no compatible
interpreter is available, use native Notebook configuration for MCP or Settings
→ Plugins for reviewed packages. Ask before widening the scope to a shared
installation.

- Save the MCP candidate as a JSON object inside Notebook. Use `command` with
  optional `args` and `cwd`, or use `url`. Leave credentials out. For example,
  pass `mcp codex check probe notes/probe.json` after the script path. Use a
  Notebook-relative candidate path. Absolute paths are rejected.
  Replace `codex` with `caelis` for the Caelis Runtime.
- Use `mcp <runtime> set <server-id> <file>` to validate and save. `check` validates
  the declaration without starting or contacting the server. `saved` confirms
  the requested declaration is on disk. Keep it when the effective configuration
  is already correct. Confirm a new tool or schema through current native
  discovery and a safe call when the result needs verification. If discovery
  still shows the old catalog, check it again in a later turn. Codex applies
  project changes through native MCP reload. Caelis discovers them from the
  trusted Notebook project.
- Use `mcp <runtime> remove <server-id>` for a Notebook entry you own or the user
  has named for removal, then confirm the tool disappears from native discovery.
  Codex protects entries outside the script-managed section. Caelis `set` and
  `remove` address entries by ID in `.mcp.json`. Confirm the ID before changing
  an existing entry. Respect an explicit untrusted decision for Notebook and
  let the user decide through the Runtime's trust workflow.
- Run `plugin catalog` for the reviewed package ID and available version. Use
  `plugin install <package-id> [version]` or `plugin update <package-id> <exact-version>`.
  Use `plugin enable`, `plugin disable` or `plugin uninstall` with the package ID.
  Keep the returned request ID. `pending` means the request is queued. Use
  `plugin status <request-id>` after the current turn and confirm the contribution
  through native discovery. `saved` confirms the requested package state. An
  update to the current version can return `saved` with the same revision.
  Leave installed package files intact and use Settings → Plugins to connect accounts.

If the configuration is busy, stop the current write and continue independent
work. If a request status is `unknown` or `unavailable`, keep the original ID and
check the current package state in Settings before deciding how to proceed. Do not
resubmit to recover an outcome. Once the status is recorded, the slot can accept
independent requests and the original ID remains queryable. Keep raw diagnostics
out of chat.

## Discover and use extensions

Use the active Runtime's catalog. Read a matching Skill body or the returned tool
schema on demand. Describe a capability as connected only after a confirmed
connection result. If `../mcp-tools.json` exists beside this Skill directory, use
its last-confirmed entries as clues and verify them through native discovery.

On Codex, use the native tools listed for the current turn. In Code Mode, check
`ALL_TOOLS` and call the tool under its current name. On Caelis with Code Mode,
use `js_exec` with `tools.search` and call the returned name and schema with
`tools.call(name, args)`. Return original reply or part references when needed to
preserve output and media. Where the Runtime exposes standalone `tool_search`
and `mcp_call`, use their returned schemas. Keep shell work on native shell tools.
If a Caelis command yields a Task handle, use the native `Task` tool with
`action: "wait"` and the original handle to collect its result.

Proceed within the user's requested scope. If a provider is unavailable, state
the limitation and continue available work. For failed actions, retain the
original receipt and use [Recovery](recovery.md) before switching routes.

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
