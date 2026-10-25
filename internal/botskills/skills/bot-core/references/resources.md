# Look after working resources

Manage resources used by both you and your Workers: workspaces, linked worktrees,
test profiles and data, build output, intermediate artifacts, caches and downloads.
Include preparation and cleanup in the work itself, rather than leaving the user
to recover disk space afterward.

## Prepare and reuse

Check the existing workspace and useful local resources first. Reuse compatible
caches, downloaded dependencies and tools where their access and isolation rules
allow. Avoid copying a full cache into every worktree or downloading the same
payload again. Check available space before a large build or download when space
may be tight. Keep caches in an appropriate reusable location, not the Notebook.

For code changes needing isolation, prepare a branch and linked worktree and pass
its absolute path as the new Worker's `workspace`. A path in the assignment alone
does not select that directory. Simple or read-only work can use an appropriate
existing checkout. Prefer clearly named directories near the repository or in the
system temporary directory, outside Notebook and app-data `Tasks`. If an existing
Worker uses `Tasks`, keep it and direct code commands to an external workspace.

Use the smallest test environment that answers the question. Reuse a suitable
isolated profile when possible; create a separate one when a test could damage
live data or conflict with another owner. Keeping originals safe is not a reason
to accumulate unused copies of the same environment.

## Finish and reclaim

You own cleanup of resources you create or ask Workers to create. Ask a Worker to
identify temporary paths it leaves behind when that matters to the assignment.
Check what each path belongs to, whether a process or task still uses it, and
whether it holds undelivered or unique work. Keep final artifacts in their intended
durable location, then remove temporary test data, intermediate output and unused
reproducible builds, including task output outside the workspace.

For a linked worktree, check its changes are merged, it is clean and no task or
process depends on it; then use ordinary `git worktree remove`. Apply the same
ownership and delivery checks to standalone checkouts. Preserve unrelated user
files and shared repositories; do not force removal or discard dirty work with
`git clean`. A hidden card, disconnect or interrupted turn does not prove a
workspace is unused. Inspect the original task if execution is uncertain.

Keep useful caches and downloads while reclaiming obsolete task-specific copies.
Routine task-owned cleanup is part of finishing authorized work; it does not need
a new question for every directory. If an unmerged PR, active dependency or
undelivered result requires retention, record the path, reason, cleanup owner and
next step in a light Notebook note. Arrange a later check when useful, and finish
cleanup after that dependency ends. A completion report does not finish this
responsibility. Store links and brief records in the Notebook, not bulky backups.
