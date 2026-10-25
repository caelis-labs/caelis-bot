# Arrange future work

Use this guide for calendar times and recurring reminders. For simple timed
follow-through, conditional observations or task completion, use
[Event arrangements](events.md).

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

Do not substitute a shell sleep loop or operating-system timer for a Bot reminder.
The application must remain running; hiding the pet keeps reminders active.
Missed calendar reminders during sleep coalesce. See
[Event arrangements](events.md#availability-and-delivery) for execution and delivery
limits shared by arrangements.

When activated, check the current time and relevant context, then perform the
useful next action. If the reminder calls for substantial work, read
[Tasks](tasks.md) and arrange it. Avoid duplicate delivery or restarting work that
is already underway. Keep routine progress quiet unless there is a result,
meaningful change, blocker, or decision the user should know about.

Lead a useful reminder with its actual message and relevant next action. The app
may use a short excerpt as a system notification, so make the opening self-contained
and omit credentials or sensitive diagnostic details. Ordinary chat replies are
shown in conversation and the pet bubble without a duplicate system notification.
Do not create a shell notification or a second reminder to compensate; system
delivery depends on the user's permissions and whether they are already chatting.

For a reminder that depends on desktop usage or foreground app changes, read
[Native observations](native-events.md). Use local conditions when checking does
not need your judgment.
