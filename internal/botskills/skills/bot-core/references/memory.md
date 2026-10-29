# Maintain Bot memory

Use `MEMORY.md` and Markdown notes relative to the Bot Notebook root.
`Notebook/MEMORY.md` names that core file from the Bot data directory; when your
working directory is the Notebook root, use `MEMORY.md`, not `Notebook/MEMORY.md`.
Maintain these files with native file tools as part of the user's ongoing work. Runtime
Memory, such as `~/.codex/memories`, is separate: its instructions and update rules
do not define Bot memory maintenance. Do not copy between the two automatically.

## Keep the core current

Keep your identity and personality, stable user preferences, important agreements
and links to continuing commitments in `MEMORY.md`. Preserve exact scope, limits
and revocations of relevant authorization; a saved agreement is context, not a
new grant or a substitute for native approval. Distinguish explicit choices from
inference. Keep private project habits here or in notes, not in shared Skills.

Review the core after meaningful decisions or corrections. Replace obsolete facts,
merge duplicates and move detailed history into linked notes. Keep enough detail
to preserve identity and important constraints without growing a transcript or
reducing memory to vague labels. Do not fill a quota or discard useful facts just
to make the file shorter.

At the first input of a new Bot session, the app supplies readable `MEMORY.md` as
saved context. It does not reload the file on every turn or inject note bodies or
`INDEX.md`. Read the latest file when updating it or checking a change; do not
assume a missing or unreadable file was supplied. Preserve unrelated edits and
confirm a memory update only after the write succeeds.

## Find and save continuing work

Use generated `INDEX.md` to find dated note paths and titles, starting with recent
work when reviewing commitments. Open relevant notes to find unfinished items;
the index does not contain their status or full text. Follow links to older open
work and use file reads or `rg` for content search. List files for notes not yet
indexed. Do not edit the generated index.

Save decisions, milestones and useful results in `YYYY/MM/DD/<topic>.md` using the
local date; `bot_schedule` `context` supplies fresh time when needed. For open work,
retain its goal, latest constraints, original task handles, artifacts, verified
progress, unanswered questions, authorization limits and next action. Record a
cancellation or changed decision where the old plan would otherwise mislead you.
Check current task, interaction and arrangement records before acting on a note.

Link to durable artifacts rather than copying builds or checkouts into Notebook.
For retained temporary resources, record path, reason, cleanup owner and next step;
follow [Resources](resources.md) when their dependency ends. When attention must
return later, use [Events](events.md) or [Reminders](reminders.md); a note alone
cannot wake you.

When asked to forget, update the authorized Bot memory files and their relevant
links. Do not restore deleted facts from history or claim to erase old Runtime
conversations, Runtime Memory or external backups.

## Prepare a useful Dream handoff

After delivering a batch of work or a clear goal change, assess whether obsolete
task detail or unused tools make the context cumbersome. Keep a useful context;
do not Dream after every reply or wait for it to become unusable.

Prepare in steps that can resume after an interruption:

1. Read current `MEMORY.md`. Use recent `INDEX.md` entries and known open handles
   to find work needing attention; follow relevant older links. Check original
   task and interaction records. Do not scan all history or wait for every Worker
   to finish. Keep review progress and detailed results in a dated note.
2. Update core memory and relevant notes. Apply new user corrections, decisions
   and revocations before continuing the handoff. Confirm the core is readable;
   resolve a failed read before calling Dream.
3. Supply a complete `handoff` of at most 16 KiB with identity and standing
   constraints, current goals, unfinished original task handles, pending decisions,
   authorization limits and revocations, uncertain operations and original
   receipts, retained resources and next actions. Keep completed results brief;
   omit transcripts, tool schemas and secrets.

Finish necessary file writes and delivery before the call. Dream ends the owning
turn; do not place other effects alongside it or depend on another step in that
turn. Preserve original work and receipts if handoff fails, and follow
[Recovery](recovery.md). A handoff does not answer pending questions, expand
permission or authorize replay of uncertain effects.

After renewal, use the supplied core and handoff to resume. Read pending reports
and their original records as they arrive; a handoff summary is not proof that a
Worker has finished or a user has answered. If renewal is deferred by a correction
or changed memory, continue from the latest instruction and files before preparing
another handoff.
