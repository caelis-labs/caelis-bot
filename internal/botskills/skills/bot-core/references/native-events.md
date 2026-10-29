# Schedule native observations

Discover metadata with `bot_schedule` `request.type:"sources"`. Native sources
are `clock.minute`, `desktop.usage`, `desktop.appChanged`. Use [Events](events.md)
for tool/script collectors and shared inspection, cancellation and wake budgets.

The clock coalesces missed minutes. Desktop samples require known active desktop
state. Usage provides `activeSeconds`, `idleSeconds`, `application`; app changes
add `previousApplication`. There is no implicit `unlocked` field. Explicitly
collect lock-state data when the task requires it; preserve unknown values.

```json
{"request":{"type":"save","id":"work-break","label":"Work break","prompt":"Offer the brief break suggestion we agreed; stay silent if nothing useful needs reporting.","trigger":{"type":"event","sources":["desktop.usage"],"condition":"event.activeSeconds >= 7200 && event.idleSeconds < 120 && local.weekday <= 5","timeZone":"Asia/Shanghai","cooldownSeconds":14400,"expiresSeconds":900}}}
```

Conditions use bounded CEL over `event`, `now` and timezone-specific `local`.
Multiple sources share a cooldown and pending occurrence; each condition sees
only the arriving source's data. Test with `bot_schedule` `request.type:"test"`,
the proposed `trigger` and sample `event`, before saving it.

Use the requested condition; lock state is not a global wake prohibition. Desktop
input tools retain their permissions and lock checks. Sleep and app exit pause
execution. List/read original handles before changing them. A changed definition
requires removal and a new ID; retained IDs cannot be rebound. Do not recreate
uncertain work. Change the shared wake budget only at the user's request.
