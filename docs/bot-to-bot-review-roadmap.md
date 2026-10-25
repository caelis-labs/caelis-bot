# Bot-to-Bot completion gates

The first version now uses [asynchronous target approval](remote-machines-ui.md).
The code exchange, SSH/helper environment, installation/configuration editor and
experimental compatibility/recovery graph are removed. Historical checkpoints are
in Git history; they are not current product instructions or migration promises.

## Authority and product scope

- A's tasks.Manager owns product tasks/UI, output/history/files, questions, native
  approvals and completion reporting. B owns isolated source-bound native execution.
- Local and remote share WorkRuntime and adapters. There is no second task ledger,
  scheduler, recursive delegation, account/configuration authority or generic proxy.
- A submits a connection request and returns. B's local user approves only the
  machine connection. A lazily/manual checks the original request; pending peers
  never appear in delegation or participate in background profile-health polling.
- Each direction has its own proof/approval. Revocation is not Stop or task approval.
- Existing new-protocol tasks retain original IDs/owners; network loss never becomes
  local fallback, unknown never becomes success, and old native requests never go
  to a replacement owner. Local chat/Worker/health remain independent of peer faults.

## Finding/regression map

| Boundary | Current requirement |
| --- | --- |
| Initial Web request and restart | Durable original profile/proof/intent before delivery; identity pin before POST; proven no dispatch differs from lost response. |
| Original target | Query original address/intent/peer; verified address repair only; different B or later intent cannot confirm old uncertainty. |
| Pending lifecycle | No native work/grant before local B decision; no continuous A wait/poll; expiry/decline do not authorize. |
| Admission | Unapproved peers absent from TaskMachines and rejected at bind, not merely hidden by UI. |
| Native ownership and capacity | Bound reads ignore current resident/default; unknown activity retains capacity; confirmed exit permits only new-work recovery. |
| Stop and interactions | Original op/expected execution generation checked atomically; all original question/approval IDs and choices retained. |
| Observation | 16 SSE budget with bounded full-observation compensation; terminal question history does not monopolize subscriptions. |
| Output/history/files | Latest text and received output survive history failure; safe cache paths retain verified original download names and offline copies. |
| Local health | Slow/disconnected cleanup, malformed peer state, unknown remote receipts and source revocation do not block local operations/update health. |
| Authorization safety | Same-ID content cannot change, terminal request IDs cannot revive, partial/5xx responses remain unknown, pre-auth body reads are bounded. |

The previous code-specific R2-1/R2-3 forms/fixtures retire with that mechanism;
the shared R2-2/R2-4/R2-5 and native F1–F8 regressions remain through the new flow.

## Validation and delivery

Required: independent review closure, exact-head make check/smoke/build, affected
race suites, `make e2e-worker-peer`, `make e2e-worker-peer-real-url` and Product CI.
Keep Draft while any required gate is open. Never merge or release automatically.
Use a distinct candidate output root; do not replace the user's running Dev/data.

Real Codex/Mock MCP/local-model evidence is distinct from controlled native protocol,
physical-machine/account evidence and native screenshot presentation. No Q22 account
input is available; no account use is inferred. Those real-account/GUI business
journeys are not current merge blockers. Deferred B-only scripted URL classes remain
exactly [the approved scope](worker-peer-url-contract.md), not claimed implemented.
