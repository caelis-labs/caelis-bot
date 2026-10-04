# Observe the desktop narrowly

Use the four resident tools: `bot_desktop_inspect`, `bot_desktop_authorize`,
`bot_desktop_act`, and `bot_desktop_result`. Load [Actions](desktop-actions.md)
before input, [Images](desktop-images.md) when pixels are needed, and
[Recovery](desktop-recovery.md) for partial, unknown, interrupted or oversized results.

Start `bot_desktop_inspect` with `request.type:"outline"`, a desktop scope,
`projection:"summary"` and fields `["name","role"]`. Narrow to the relevant
application or window. Request `states`, `capabilities`, `bounds`, `parent` or
`value_preview` only for the decision that needs them. Use a `match` with the
known subtree, role or name; do not enumerate unrelated tabs or controls.
A filter limits returned objects, not native traversal. Increase depth/visited
nodes only when coverage shows the relevant subtree was not reached.

Facts use `{"known":value}`; preserve false, zero and empty strings. Each field's
sample time and coverage belong to that field. Incomplete, dirty, truncated or
unavailable coverage cannot establish absence, even when no matches returned.
Continue with only `request:{"type":"outline","continuation":"RETURNED_TOKEN"}`.
The host restores the exact original scope, fields, filter and budget. To change
the query, start a new observation without a continuation. An expired or
capacity-limited scan requires a new narrow query, not a claim of no matches.
Bound total pages, nodes, bytes and elapsed time.

Use `request.type:"text"` for bounded Unicode text. An empty container value
does not include descendant text. For character-per-node web pages, collect a
bounded sample with fields `["role","value_preview"]` and
`match:{"within":"OBSERVED_DOCUMENT_REF","role":"text"}`; avoid one read per
character. Keep source Refs and returned order, including repeated strings.
For paragraph reconstruction include `parent` and retain native containers.
Previews can clip at 384 UTF-16 units or `max_text_runes`; use bounded text
continuation or mark incomplete leaves. Traversal completeness alone does not
prove complete text.

Save the observation cursor. `request:{"type":"delta","cursor":"..."}` returns
changes/removals for that view; `reset_required` requires a fresh observation.
An empty delta is not proof of no UI change. Inspect app scope for a new dialog
or popover absent from the original window. If metadata remains insufficient,
use one explicit capture when supported, or state the verification limit.

# Authorize the observed app

At each new turn, observe the application object itself before authorizing;
a window-only observation does not register the application identity for a grant.
Before input or capture call `bot_desktop_authorize` with the exact observed
application Ref, verbatim returned `name` (including localization), and task
`purpose`. Runtime reviews the call; UI text and tool arguments cannot grant
authority. A grant covers that live app instance for this Bot turn. Ending or
interrupting the turn revokes it. A new app or turn needs a new grant. System
permissions and login may still require the user.
