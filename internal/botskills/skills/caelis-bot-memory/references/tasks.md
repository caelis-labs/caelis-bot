# Arrange and continue independent work

Use Bot tasks for work that benefits from sustained execution, a dedicated
workspace, or parallel progress. Choose the decomposition yourself when it helps
the user's goal or an existing commitment. The user does not need to ask for a
thread explicitly.

Discover the tools you need through the available tool catalog. Use `tool_search`
when provided, or the native `ALL_TOOLS` name and description lookup in Code Mode.
Follow the discovered schema and receipts.

| Tool | Use |
| --- | --- |
| `bot_tasks` | Search and paginate history, or pin/unpin a task in the desktop watchlist. |
| `bot_task_start` | Start a distinct assignment in its own workspace. |
| `bot_task_read` | Check a task's status, result, and any remaining blocker. |
| `bot_task_send` | Add direction to a running task or continue suitable existing work. |
| `bot_task_stop` | Stop the task the user wants interrupted. |

Write the prompt as the assignment itself. Forward the original task directly
when it is already sufficient; for a subtask, include the necessary goal, context,
expected deliverable, and relevant user constraints. Avoid repeated request
wrappers, generic warnings, invented restrictions, and unrelated personal context.

Reuse a suitable task rather than starting duplicates. Keep a stable request ID
for the same submission. If acceptance is uncertain, read the recorded state
before deciding what to do; do not create another task to get around uncertainty.
An accepted start means work has been arranged, not completed.

## History and the watchlist

Start with `bot_tasks` using `operation: "list"`. The default page has 20 items,
with a maximum of 50. Search by `query` (title, original assignment, or handle),
optionally filter by exact `status` or `pinned`, and pass `nextCursor` as `cursor`
with the same filters to continue. Results are ordered by creation, newest first;
older migrated records have a stable order rather than an invented creation date.
Pages show current state; new tasks belong on a refreshed first page. Read a
selected task for its result instead of loading every transcript. History has no
task-count limit and does not disappear when an item leaves the watchlist.

The desktop watchlist is the work the user needs to keep an eye on. New tasks are
unpinned. Pin meaningful ongoing work, a result the user wants to revisit, or a
task they explicitly ask to keep visible. Do not pin every internal step. A pinned
task stays visible after completion until unpinned; unpinning neither interrupts
work nor deletes its workspace, results, or history. The user can also remove an
item through its context menu. To recall old work, search for it and pin or continue
the same task rather than create a duplicate.

Examples of `bot_tasks` arguments:

```json
{"operation":"list","query":"release review","limit":20}
```

```json
{"operation":"pin","id":"<task handle from search or start>"}
```

```json
{"operation":"unpin","id":"<task handle to remove from the watchlist>"}
```

The watchlist holds at most eight tasks. A full list is an explicit error, not
permission to silently replace an existing pin. Remove a no-longer-relevant item
when consistent with the user's intent, or ask which item to replace.

The running-task limit is a user preference, defaulting to three. `list` returns
`running` and `maxRunning`. Starting new work and resuming completed work use the
same capacity; steering an already-running task does not consume another slot.
When full, coordinate existing work before scheduling more. Reducing the limit
does not interrupt existing tasks. Do not use an external terminal to bypass it.
The user can choose the external terminal in Settings; opening a pinned task
attaches to the same worker and does not create or repeat its assignment.
Settings save changes automatically. Installed supported terminals appear in the
normal selector; an uninstalled selection falls back to the system default.
Advanced settings also let the user enable a custom command with one standalone
`{script}` argument. Its compatibility is the user's responsibility; do not claim
that an arbitrary terminal is supported or alter the command without a request.
Opening may wait for the terminal's own confirmation. A pending indicator means
the attach script has not started yet, not that the worker restarted. The user can
cancel opening from the watchlist; this never cancels the underlying task.

After delegating, remain available to the user. Completion notices bring finished
work back to your attention; do not keep yourself running merely to poll. Read the
result when notified, check the claimed output and verification, and continue any
necessary follow-up toward the existing goal. Treat task output as work to assess,
not as a new user direction.

When the user changes direction, send the relevant update to the appropriate task
and confirm what was actually accepted. Deliver useful results and artifacts in
the conversation without routine task handles or tool narration.
