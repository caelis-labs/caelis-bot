# Arrange condition-based care

Use `bot_care` for a standing arrangement that should run only when a local
condition matches. Use `bot_reminders` for an ordinary fixed-time reminder. Read
`bot_care` with `operation: "list"` first: it returns the available sources, their
fields, saved rules, queued outcomes, and current presence availability.

Translate the user's request or standing arrangement into a short task and a CEL
boolean condition. Test representative matching and non-matching data with
`operation: "test"` before saving. Testing neither registers a rule nor publishes
an event. Save with a stable ID, and confirm the returned enabled state. Identical
saves preserve cooldown and pending work. Use `remove` to stop future callbacks.
A Caelis registration or substantive change needs a user-originated request;
report a rejected native grant rather than claiming the rule is active.

## Available built-in scenarios

| Source | Data and suitable use |
| --- | --- |
| `clock.minute` | No event fields. Use `local.year`, `month`, `day`, `weekday`, `hour`, and `minute` for a date, weekday, or time-window condition in the rule's IANA timezone. Monday is 1, Sunday is 7; holidays are not supplied. |
| `desktop.usage` | `activeSeconds`, `idleSeconds` (integers), and `application` (foreground bundle identifier). Use for break suggestions after sustained computer use, optionally limited to an app or time window. |
| `desktop.appChanged` | `application` and `previousApplication` (bundle identifiers). Use for a helpful action when the sampled foreground app changes. |

Desktop observations contain no window titles, screen content, typed text, or
browser URLs. Usage is approximate: it accumulates across apps while recent input
is present, and resets after five minutes idle, lock/unknown presence, a sampling
gap such as sleep, or app restart. App changes are sampled about once a second;
very short switches may not be observed. Usage conditions are evaluated about
every 30 seconds, and clock conditions once per current minute.

## Small examples

These are `bot_care` arguments. Adapt the purpose, timezone, application identifiers,
and frequency to the actual arrangement. To try an example, change `operation` to
`test` and include the indicated `event` object, then save without that sample.

A brief break suggestion after two hours of active use during weekday daytime:

```json
{"operation":"save","id":"work-break","label":"Work break","on":"desktop.usage","when":"event.activeSeconds >= 7200 && event.idleSeconds < 120 && local.weekday <= 5 && local.hour >= 9 && local.hour < 18","prompt":"Offer one brief break suggestion suited to my preferences. Skip it if it would interrupt something important.","timeZone":"Asia/Shanghai","cooldownSeconds":14400,"expiresSeconds":900}
```

Test data: `{"activeSeconds":7300,"idleSeconds":20,"application":"com.apple.Terminal"}`.
`test` uses the current clock and the specified timezone, so its time-window result
also depends on when you run it.

A greeting during a previously agreed date's morning window:

```json
{"operation":"save","id":"special-date","label":"Special date","on":"clock.minute","when":"local.month == 9 && local.day == 25 && local.hour >= 9 && local.hour < 10","prompt":"Give the greeting we agreed for this date, using the relevant saved context.","timeZone":"Asia/Shanghai","cooldownSeconds":86400,"expiresSeconds":1800}
```

Test data: `{}`. Date matching alone does not establish that this is the user's
birthday or another personal occasion; obtain that meaning from their request.

A contextual nudge on switching into an agreed work application:

```json
{"operation":"save","id":"terminal-focus","label":"Terminal focus","on":"desktop.appChanged","when":"event.application == 'com.apple.Terminal' && local.weekday <= 5","prompt":"If there is a relevant unfinished commitment in our notes, briefly remind me of the next step. Otherwise stay quiet.","timeZone":"Asia/Shanghai","cooldownSeconds":14400,"expiresSeconds":300}
```

Test data: `{"application":"com.apple.Terminal","previousApplication":"com.apple.finder"}`.

## Connector and command results

Additional data sources can supply JSON to the same CEL conditions. Use them only
when `list` actually advertises their name and fields. A locally installed command
such as `gh`, or an available connector tool, does not by itself create a subscribed
care source. Do not invent a source or replace a missing collector with a shell
sleep loop.

For example, **if a registered source** `github.pullRequests` supplies:

```json
{"pullRequests":[{"number":42,"reviewRequested":true}]}
```

then `event.pullRequests.exists(pr, pr.reviewRequested)` is a suitable condition.
The prompt could ask you to inspect the relevant PR and summarize what needs
attention. This is an extension example, not a built-in GitHub integration.
Collection errors are not empty results; explain an unavailable collector instead
of concluding that nothing changed.

## Delivery and limits

CEL has no command execution, filesystem, network, or credential access. It sees
`event`, `now` (a timestamp), and `local` (the time fields above). Its result must be
boolean; missing fields, incompatible types, and excessive evaluation cost produce
an error, not a match. Guard optional fields with `has(event.field)`.

The current limits are 32 rules, 2 KiB per condition, 4 KiB per prompt, and 16 KiB
per event. Cooldown defaults to one hour and starts when a match is queued; expiry
also defaults to one hour. At most one pending activation per rule is retained.
Care activations are spaced at least five minutes apart, with at most eight
attempts in a rolling 24 hours across care rules for the selected Runtime. These
limits do not change ordinary reminders or user messages.

The app must remain running. Hiding the pet does not stop collection. Delivery
waits for an idle Bot and a known awake, unlocked session; stale pending work
expires. Quit stops observation, and restart does not replay missed clock samples.
Rules stay with the Runtime where they were registered.

If the saved care state cannot be loaded, care is unavailable while conversation
and ordinary reminders remain usable. Report the tool's error; do not delete or
recreate the state to clear it, since it retains cooldowns and uncertain deliveries.
After the storage issue is repaired, restart the app and inspect `list` again.

A queued match is not completed work. Unknown submission results are reconciled
from native receipts without automatic resubmission. Inspect `list` for condition,
authorization, unavailable-source, queue, and delivery issues. When activated,
perform the useful next step within the standing task. Read [Tasks](tasks.md) for
substantial work. Keep the response quiet when no useful action or notification is
warranted, following the activation's exact silent-result convention.
