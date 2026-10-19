# Arrange native observations

Use `bot_schedule` with `request.type:"sources"` to discover host-provided metadata.
Use the same schedule tools for all user-authorized follow-through. Prefer [Event arrangements](events.md) for after/at/interval,
script/tool observations and task completion; use [Calendar reminders](reminders.md) for calendars.

Native sources are `clock.minute`, `desktop.usage` and `desktop.appChanged`.
The clock coalesces missed minutes while the app runs. Desktop activity samples require a known
active desktop; this defines the sample, not permission to wake you. Usage fields are
`activeSeconds`, `idleSeconds`, `application`; app changes add `previousApplication`.
These payloads do not include an implicit `unlocked` field. A task requiring lock-state data must
explicitly collect it and test its output, preserving unknown rather than assuming unlocked.

Save a native condition with `bot_schedule_update`:

```json
{"request":{"type":"save","id":"work-break","label":"Work break","prompt":"Offer the brief break suggestion we agreed; stay silent if nothing useful needs reporting.","trigger":{"type":"event","sources":["desktop.usage"],"condition":"event.activeSeconds >= 7200 && event.idleSeconds < 120 && local.weekday <= 5","timeZone":"Asia/Shanghai","cooldownSeconds":14400,"expiresSeconds":900}}}
```

Conditions use bounded pure CEL over `event`, `now` and timezone-specific `local` fields.
Multiple sources share one cooldown and pending occurrence; each condition sees only the arriving
source's data. Use `bot_schedule.test` with sample data to check a condition without publishing it.
Use only the user's requested condition; desktop locking never adds an outer dispatch prohibition.
Desktop-input tools still enforce their own permissions and lock boundaries. Sleep and app exit
pause execution; they are distinct from locking the desktop.

Preserve returned handles and original receipts. List/read before changing an arrangement; list
queries match the original native ID and label as well as internal handles. Remove its exact handle
when work ends. A changed definition needs a fresh ID and reviewed registration; removal does not
free an ID while its definition is retained. Never recreate unknown work or infer a missing phone
origin from the latest conversation.
Imported arrangements keep their original receipts and authorization. Native and collected events
share the wake budget, busy admission and cancellation rules. Configure the budget only when the
user explicitly requests it. Registration is not delivery; phone delivery requires a valid original
channel association and that channel's current delivery window.
