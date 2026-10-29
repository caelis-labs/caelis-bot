# Schedule calendar reminders

Use this page for calendar times and recurring reminders. For delayed work,
conditional checks or task completion, read [Events](events.md).

Read fresh local time and timezone with `bot_schedule`
`request:{"type":"context"}`; list saved reminders with
`request:{"type":"list","kind":"calendar"}`. Save with `bot_schedule_update`:

```json
{"request":{"type":"save","id":"work-break","label":"Work break","prompt":"Offer the break reminder we agreed.","trigger":{"type":"calendar","timeZone":"Asia/Shanghai","schedule":{"type":"interval","everyMinutes":60}}}}
```

Choose one calendar schedule: `at` with RFC3339 timestamp, `interval` with
`everyMinutes`, `daily` with HH:MM, or `times` with HH:MM entries. Weekdays and
allowed windows belong to the trigger. Use the fresh named IANA zone unless the
user specified another. If `timeZoneStatus` is unavailable, obtain the intended
zone; do not infer it from an offset or use `Local`.

Resolve timing/purpose ambiguity when it would change the action. Verify enabled
state and the returned next time. Remove the exact handle with
`request:{"type":"remove","automation":"calendar:work-break"}`. Lists support
label/ID search, default 20 and max 50 entries; follow `nextCursor` with the same
filters. Read the exact handle for its full saved prompt.

Keep standing arrangements within their purpose and cadence. Do not replace a
Bot reminder with shell sleep or an OS timer. Sleep-missed calendar reminders
coalesce; other limits are in [Events](events.md#availability-and-delivery).

On wakeup, check current time and relevant context, then perform the next useful
action. Delegate substantial work through [Tasks](tasks.md). Avoid duplicating
ongoing work or an already delivered reminder. Lead with the useful message and
next action; it may become a notification excerpt. Omit secrets and diagnostics.
Do not create a second notification to compensate for quiet system delivery.
