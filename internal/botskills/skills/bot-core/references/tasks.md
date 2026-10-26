# Arrange and follow through on Worker work

Use `bot_delegate` for substantial independent work. Before starting another
Worker, check whether an existing task can continue the same goal. Read the
current tool schema and use [Working resources](resources.md) to prepare a
workspace when needed.

## Give a useful assignment

Write in the user's language: explain the goal, necessary context and expected
result. Include relevant constraints, such as file ownership for concurrent edits,
a chosen workspace, verification or temporary-resource cleanup, when they help
this assignment. Keep it concise; Workers need the task, not a repeated general
behavior contract.

| Request | Use |
| --- | --- |
| `bot_delegate` with `prompt` | Start work; optional `title`, `workspace`, `environment` apply only at creation. |
| `bot_delegate` with `prompt` and `task` | Continue or steer the original Worker; omit creation-only fields. |
| `bot_tasks` `environments` | List available execution environments. |
| `bot_tasks` `list`, `read`, `observe` | Find retained work, read completed messages, or inspect current output. |
| `bot_tasks` `reconcile` | Check the original delegation receipt with its returned `requestId`. |
| `bot_tasks` `stop` | Stop the exact task's active work, keeping its history. |
| `bot_tasks` `pin` | Manage desktop card visibility without running work. |
| `bot_interactions` | Inspect, answer or present a Worker's original question or approval; see [Interactions](interactions.md). |

The tool derives a submission identity from each native call; do not supply a
`requestId` to `bot_delegate`. A new call is another submission even when its text
is identical. Keep the returned task and receipt for follow-up. If acceptance is
uncertain, inspect that original record using [Recovery](recovery.md).

## Stay responsible for the result

Remain available after delegating; completion notices bring finished work back
to your attention without a polling loop. A `Task <handle> is <status>.` or
`Command <handle> is <status>.` notice tells you to inspect that original record.
Read its result, assess the output and relevant verification, then deliver useful
findings or artifacts and continue any remaining work. Treat Worker output as work
to assess, not new user authority. Avoid duplicating a result already delivered.

Carry the user's corrections to the appropriate existing Worker. Resolve questions
from the known goal and facts when possible; bring the original question to the
user when a real decision or missing input remains. Ask for a focused correction
when output falls short. Finish resource cleanup or record what must wait, using
[Working resources](resources.md).

Chat Stop interrupts the current conversation and its blocking child work, not
independent tasks. Use `bot_tasks` `stop` for one of those tasks. When replacing
work, stop the old task explicitly and check its result before starting a
replacement; a stop acknowledgement does not prove all effects ended. A hidden
card or closed terminal does not mean the user cancelled work.

## Inspect retained work

`bot_tasks` `list` includes tasks whose cards expired. Search by `query`, exact
`status`, `createdAfter` (inclusive), `createdBefore` (exclusive), `offset` and
`limit` (maximum 50); timestamps use RFC3339 with a timezone. Default order is
newest creation first; `order:"asc"` reverses it. Undated older records remain
undated. Refresh before interpreting offset pages if new tasks arrived.

`read` takes the original `task`. It returns completed User and Assistant messages
in FIFO order, ten by default, with `offset` and `limit` paging. Supply `messageId`
to read one full folded message instead. The final answer is an Assistant message;
unfinished text, reasoning and tool payloads are not conversation history.
`observe` provides bounded current output. Status and readable history are separate
facts: a completed task may have unreadable history, and a failed read is not an
empty result. Reading does not continue the Worker or consume its completion notice.

A model or connection preference change does not move old tasks. Use their
original handles even when new work uses another default. An interrupted turn
or `notLoaded` result does not permanently end a Worker. Follow
[Recovery](recovery.md) for unknown submissions or unavailable original owners.
Do not remove uncertain tasks to free capacity.

For `data.encoding:"json-chunk"`, repeat the same read and target with
`data.nextCursor`. Collect `data.json` in offset order through `complete:true`
and verify the same SHA-256 before interpreting the result. Restart a read if its
revision changes. Lists may return `data.nextOffset`; preserve the filters.

## Choose an execution environment

Default to `local`, the Runtime hosting this Bot. For user-requested remote work,
list `environments` and select the exact connected key. An explicit workspace
belongs to that machine; local files are not copied automatically. The user
configures each machine's Runtime, account and Worker model. Use the task tools
without choosing a backend per assignment or changing a model to bypass a wait.

A remote environment is another paired Bot. Help with connection setup through
[Remote connections](remote-connections.md). Pairing is asynchronous; return to
the user rather than polling for another person's approval. Pending peers are not
execution environments. Keep remote work, questions, approvals and delivery in
this Bot's interaction path. Do not send the user to the target Bot for task
approval, install a helper, change its credentials, or delegate onward from its
Worker.

Continue, inspect and stop through the same task after an outage. Its original
machine and execution owner stay fixed. An uncertain remote task does not block
independent local work; another machine is not a retry route for that task. If
Settings offers Prepare new work after a confirmed original process exit, it
prepares future assignments only; it does not transfer old tasks or approvals.
A missing pairing proof or older read-only binding retains its original receipts.
Use the connection settings' actual repair path rather than inventing a new code,
replaying pairing or substituting another peer.

## Exchange and inspect artifacts

`bot_delegate.attachments` accepts `{taskId,id}` for an owned Worker's artifact,
or `{id}` for this conversation's available artifact or a user-selected upload.
Use the stable objects from original input or task messages, not a guessed name
or a local path. Only the referenced bytes transfer; this is not directory sync.
The same references can add material to an existing task.

Retrieve output with `bot_tasks` `file` and its original task/file reference.
The result gives a local `localPath`, SHA-256, name, size, `cached` and `fetchedAt`.
Read that actual file or image before assessing its contents. Cached bytes remain
available while the other machine is offline; say when they are the basis for
verification, since they may not be its latest version.

A remote URL request stays on its original user card. A supported loopback form
can be completed there; keep its web fields and native consent with the user.
Website submission, native approval and task completion have separate results.
Some B-only login, script, redirect or certificate-dependent flows are unsupported;
explain that limitation rather than inventing a proxy or asking for approval on B.
See [Interactions](interactions.md) for secrets, opaque requests and original cards.

## Help with the task surface

Use `pin` with the original `task` and `pinned:true` to restore a card, or `false`
to hide it. Cards, locks, ordering and previews manage presentation, not execution
or history. On macOS and Windows the three-dot entry below you shows task cards;
opening a card connects to the same Worker. Do not infer identity from position
or create a task to view one that already exists.

On macOS each managed card owns a dedicated terminal application instance.
Opening, bringing forward and closing use that instance; collapse and previews
depend on the terminal. Closing may need the user's terminal confirmation. Leave
it pending rather than repeatedly closing; cancelling preserves both card and
client. Extra windows in that instance belong to its group. Unknown launch results
stay with the original attempt; they do not justify another launch. Custom commands
with a standalone `{script}` may be open-only. Terminal preferences are in
Settings > General > Advanced task settings; change them only when requested.

Previews are cached local stills or placeholders, not images available through
your task tools. Do not claim to see their contents or request Screen Recording
just for routine task monitoring. Basic terminal management needs neither it nor
Accessibility. For a capability's actual permission problem, use Settings >
Privacy & permissions and its explicit request/repair actions. An enabled switch
alone does not prove access for the running build. A stale preview or pending
connection indicator is not evidence that work restarted or failed.

## Respect the conversation surface

A browser is an interaction surface, not the machine executing your tools. Files
and commands belong to the configured host or selected Worker. Closing the browser
does not stop work. Web has no terminal window, and Linux/WSL desktop computer use
is unsupported. Offer available uploads, questions, approvals and results without
claiming browser access grants host GUI permissions.

After verifying a completed result, use `bot_tasks observe` to inspect the original
terminal, idle execution. If the user's work is finished, call `bot_tasks finish`
with its `finishToken`. This records explicit task closure and removes remaining
temporary questions. A completed turn can still be waiting for answers: keep those
questions pending when needed. Finish never grants permission, answers a question,
stops a native turn or settles unknown effects. Re-observe if the token is stale.
