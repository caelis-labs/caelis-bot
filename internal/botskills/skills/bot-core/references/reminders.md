# Arrange future work

Use `bot_schedule` with `request:{"type":"context"}` for fresh local time and
timezone; use `request:{"type":"list","kind":"calendar"}` for saved reminders.
Use `bot_schedule_update` to save or remove an arrangement. Follow the actual schema:

```json
{"request":{"type":"save","id":"work-break","label":"Work break","prompt":"Offer the break reminder we agreed.","trigger":{"type":"calendar","timeZone":"Asia/Shanghai","schedule":{"type":"interval","everyMinutes":60}}}}
```

Calendar schedules choose exactly one type: `at` with an RFC3339 timestamp,
`interval` with `everyMinutes`, `daily` with HH:MM, or `times` with a list of HH:MM.
Use the named IANA timezone from fresh context unless the user specified another.
If timeZoneStatus is unavailable, obtain the intended timezone; never use Local
or infer a named zone from an offset. Weekdays
and allowed time windows belong to the calendar trigger. Confirm the actual
next time and enabled state. To remove, use the exact returned automation handle:
`request:{"type":"remove","automation":"calendar:work-break"}`.
List supports label/ID search, 20 entries by default, 50 maximum and `nextCursor`;
keep filters unchanged when continuing.

Turn the requested timing and purpose into a clear reminder. Resolve ambiguity
when it would change when or why the reminder runs. For work arising from a standing
arrangement, preserve that arrangement's intended scope and cadence.

Confirm the saved schedule from the tool result. Do not substitute a shell sleep
loop or an operating-system timer for a Bot reminder. The application must remain
running: hiding the pet does not pause reminders, missed reminders during sleep
coalesce, and explicitly quitting pauses future execution.

When activated, check the current time and relevant context, then perform the
useful next action. If the reminder calls for substantial work, read
[Tasks](tasks.md) and arrange it. Avoid duplicate delivery or restarting work that
is already underway. Keep routine progress quiet unless there is a result,
meaningful change, blocker, or decision the user should know about.

For care that depends on computer use, foreground app changes, or another
registered data source, read [Proactive care](proactive-care.md). Use its local
conditions to avoid waking a model just to check whether anything is relevant.
