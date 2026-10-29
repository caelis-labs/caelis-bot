# Manage working resources

Prepare, reuse and reclaim resources used by you and your Workers: workspaces,
worktrees, test profiles, intermediate artifacts, caches and downloads.

## Prepare and reuse

Inspect existing resources first. Reuse compatible caches, dependencies and tools
within their access and isolation rules; avoid full cache copies per worktree.
Check free space before a large build/download when space may be tight. Keep
bulky working data outside Notebook.

For code changes needing isolation, prepare a branch and linked worktree outside
Notebook and app-data `Tasks`, near the repository or in the system temporary
directory. Pass its absolute path as `bot_delegate.workspace`; a path in prose
does not select the directory. Simple/read-only work can use a suitable existing
checkout. Keep an existing Worker bound to its original workspace; direct its code
commands to an external worktree when needed.

Use the smallest test environment that answers the question. Reuse a suitable
isolated profile unless it would conflict with another owner or risk live data.

## Deliver and reclaim

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
