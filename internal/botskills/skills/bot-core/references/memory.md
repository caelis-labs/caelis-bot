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

## Consider an optional review invitation

A private continuity invitation contains limited observations of elapsed time and
ordinary work. It is not evidence that memory is stale, work is obsolete or bills
are high. Current user instructions, corrections and revocations take priority.
Keep working and defer if the context remains useful; no reply or maintenance
announcement is required. Follow the invitation's quiet-result instruction when
there is nothing useful to deliver.

Review a useful slice using the core and relevant notes above. Consolidate and
read back Notebook/MEMORY changes in small steps; save a dated note with the next
slice if more remains. Updating memory does not require a new session. Decide
separately whether and when a Dream handoff would help; neither an invitation nor
old notes authorize discarding context or repeating an uncertain operation.

## Prepare a useful Dream handoff

After delivering a batch of work or a clear goal change, assess whether obsolete
task detail or unused tools make the context cumbersome. Keep a useful context;
do not Dream after every reply or wait for it to become unusable.

Before calling `bot_dream`, update core memory and relevant notes, then verify
still-open work against its original records. Supply a complete `handoff` of at
most 16 KiB with identity and standing constraints, current goals, unfinished
original task handles, pending decisions, authorization limits and revocations,
uncertain operations and original receipts, retained resources and next actions.
For an ongoing review, include the artifact or note paths, what was actually
reviewed, the unread scope and the next checkpoint. Let Workers continue; recover
their current status and pending interactions from the original records.
Keep completed results brief; omit transcripts, tool schemas and secrets.

Finish necessary file writes and delivery before the call. An accepted Dream
ends the owning turn; do not place other effects alongside it or depend on another
step in that turn. If a user correction supersedes Dream and the tool returns a
nonterminal refusal, continue with that correction in the current conversation.
Preserve original work and receipts if handoff fails, and follow
[Recovery](recovery.md). A handoff does not answer pending questions, expand
permission or authorize replay of uncertain effects.
