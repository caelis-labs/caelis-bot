# Manage working resources

Prepare, reuse and reclaim resources used by you and your Workers: workspaces,
worktrees, test profiles, intermediate artifacts, caches and downloads.

## Prepare and reuse

Inspect existing resources first. Reuse compatible caches, dependencies and tools
within their access and isolation rules; avoid full cache copies per worktree.
Check free space before a large build/download when space may be tight. Keep
bulky working data outside Notebook.

Prefer the actual project workspace for nonparallel work. Only when that workspace
may be occupied or conflict with other work, plan and prepare a separate branch
and linked worktree outside Notebook and app-data `Tasks`, near the repository or
in the system temporary directory. Pass the prepared absolute path as
`bot_delegate.workspace`; the Worker receives the actual workspace, and a path
in prose does not select it. You own its later cleanup.

Use the smallest test environment that answers the question. Reuse a suitable
isolated profile unless it would conflict with another owner or risk live data.

## Deliver and reclaim

In a desktop reply, use a Markdown link with the file's actual absolute path
and a recognizable label, for example `[Light preview](</absolute/path/亮色 预览.png>)`.
Quote the destination with angle brackets when it has spaces. Link directly
to a readable local file in Notebook, a project or a local Worker workspace.
Supported images appear at that label and can be enlarged. Keep delivered
files at their original paths for as long as the user needs them. Use a full
HTTPS URL for a website and show what the reader can find there.

Own cleanup for resources you create or ask Workers to create. Identify each
path's owner, active tasks/processes and unique or undelivered content. Preserve
final artifacts in their intended durable location, then remove task-owned
intermediate output, test data and unused reproducible builds, including output
outside the workspace.

Before `git worktree remove`, verify the worktree is clean, its changes are merged
and no task/process depends on it. Protect unrelated files and shared checkouts;
do not force removal or discard dirty work with `git clean`. Card visibility or
a disconnect does not prove a resource unused. Preserve uncertain resources and
inspect their original task.

Keep useful shared caches/downloads; remove obsolete task-specific copies. Routine
cleanup within the authorized task needs no new question for every path. When a
PR, dependency or delivery requires retention, record path, reason, cleanup owner
and next step in Notebook. Arrange a later check when useful and reclaim after
the dependency ends. Keep lightweight links, not bulky backups, in notes.
