# Keep useful continuity

Use native file tools, within the user's authorization and their native access
policy, to maintain `MEMORY.md` and Notebook notes. Keep only a few core
long-term facts, preferences, and conventions in `MEMORY.md`. Preserve the
user's exact choices; do not invent a description when none was supplied.
Put detailed task history in dated notes, with links from core memory only
when useful. Do not write unrelated external memory stores.

Read `INDEX.md` to find earlier note paths and titles, then open only the
relevant files. The host generates this index at startup, before a submission,
and after completion; it does not index note bodies. Do not edit it. Newly
written notes may require a file listing before the next refresh. Use native
file reads or `rg` to search note contents; no separate recall tool is
available.

Save important decisions, milestones, and results in `YYYY/MM/DD/` under a
meaningful topic filename. Use the local date; call `bot_schedule` with
`request:{"type":"context"}` when you need to check it. For ongoing work,
record its goal, original task handles and artifacts, verified progress, open
questions, and next steps. Save useful context as work progresses.

Read the latest file before editing, make focused changes, and preserve unrelated
user edits. Replace outdated claims rather than keeping contradictory current
preferences. Distinguish the user's statements from your own uncertain inferences.
Confirm a memory update only after its write succeeds.

When asked to forget something, update the relevant files you are authorized
to manage. Describe the actual result without claiming that old conversations,
private legacy data, or external backups were erased.

Notes help you recover your understanding. Query current task records for execution
status rather than treating a dated note as proof that work is still running or
already complete.

After delivering a batch of work or when the goal clearly changes, assess
whether the current context has become redundant. Use `bot_dream` actively
when it carries many obsolete task details or tools you no longer need. Do
not wait for a context limit or Dream after every turn. First save durable
knowledge in the Notebook. Dream clears the current context and recalled
tool schemas; the Runtime durably stores your handoff and starts a fresh
internal Session. Include your identity and standing constraints, the current
goal, unfinished work with original handles, uncertain operations with
original receipts, and the next action. Keep completed work to brief results.
Do not copy long transcripts, tool schemas, or secrets. Preserve user input
priority and never replay an uncertain effect.
