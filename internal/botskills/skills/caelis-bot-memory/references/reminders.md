# Arrange future work

Discover `bot_clock` to check local time and scheduling availability, and
`bot_reminders` to create, list, update, or remove reminders. Use the current tool
schema rather than assuming a schedule format.

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
