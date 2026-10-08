---
name: bot-dream
description: Perform a bounded memory review and write a short handoff only when the Bot host explicitly sends a system Dream request with an output path and handoff marker. Never invoke this skill autonomously during ordinary work or conversation.
---

# Prepare the next conversation

Use this skill only for an explicit host Dream request. The request supplies the
exact writable HANDOFF.md path and the marker to put on its first line. Do not
choose another path, start a new session, or initiate Dream yourself.

Review the current conversation and read only the existing memory or dated notes
needed to preserve useful continuity. Optionally update important decisions,
stable preferences, and verified progress using the usual memory workflow. Read
the latest files before editing and preserve user changes. Skip unnecessary
memory writes; do not reorganize the whole notebook or continue unfinished work.

Write a fresh, concise HANDOFF.md at the exact requested path. Include the given
marker, completed or cancelled work, genuinely unfinished or waiting items,
essential constraints and uncertainties, and links to useful notes or artifacts.
Carry forward still-relevant obligations from the previous handoff. Say when
nothing remains open. Completed work is background, not a new task to execute.
Do not duplicate the full MEMORY.md or restore information the user removed.
Aim for at most 400 words. Use the current user's language for the recap. Do not
send progress commentary during this maintenance turn; its final recap is the
only routine user-facing message.

The host displays a quiet napping status while this turn runs. Do not emit a
progress message, gesture, or `zzz` to create that status. User input takes
priority; if this maintenance turn is interrupted, do not retry or resume it
autonomously. A later host request will supply its own path and marker.

Confirm that the handoff write succeeded. Then end this turn with only one short,
user-facing recap sentence describing what was accomplished and what, if anything,
is still pending. Do not describe internal files, Dream, or session management.
If writing fails, report the failure accurately instead of claiming completion.
