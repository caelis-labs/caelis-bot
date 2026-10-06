# v0.9.1 availability contract

Bot control and local conversation remain available independently of Runtime
observation, Worker observation and optional local features. Runtime owns native
sessions and execution; an uncertain original action is reconciled by its original
receipt. Self-recovery must never replay that action or replace its owner.

| Fault | Local degradation and recovery | Evidence |
| --- | --- | --- |
| Large tool results or embedded attachment images | Stream and drain unused bytes; retain identities, approval choices and terminal facts; continue the next frame | 14 MiB transport and escaped-result fixtures |
| Oversized retained display data | Omit this projection, return the correlated read error, keep controls and subsequent frames alive; retry optional recent sync | Oversized prose fixtures with ID before/after payload |
| Invalid framing, partial frame, lost socket or event overflow | Replace the observer connection to the original owner; bounded fast retries followed by a quiet supervisor | Transport classification, reconnect and pending receipt fixtures |
| Recent-message sync failure | Ready connection remains usable; fetch only latest turn summary asynchronously | Thin recovery fixtures; no full-history fallback |
| Failed Worker subscription/read | Keep that task and receipt unknown; retry only its observation; resident reconnect does not await all Workers | Worker recovery and retired-subscription fixtures |
| Unknown main native send/approval | Keep native action fenced and controls available; reconcile without replay | Original-ID and unknown-outcome fixtures |
| Failed optional store/configuration | Preserve original bytes/identity, keep shell and controls alive; retry reopening repaired originals | Optional-store startup fixture and component retry paths |
| IM cache disk failure/corruption | Retain live messages; retry writes; preserve damaged display-only database and rebuild | Disk obstruction, corrupt SQLite, offline reopen and local pagination fixtures |
| Slow state write, diagnostics or operation admission | Deadline on receipt storage/admission; cached control snapshots; asynchronous bounded diagnostics | Lock admission and native cleanup fixtures |
| Telegram Runtime outage/download stall | Durable incoming queue with separate callback, command and ordinary-message lanes; immediate callback feedback | Durable-before-offset, ingress recovery and priority fixtures |
| Telegram approval/card update error | Preserve original native decision; retry idempotent edits on the original message; stale controls cannot grant authority | Native persist choices, card convergence and edit-recovery fixtures |
| Renderer/resource failure | Recover the affected surface/asset; local history and native work survive remount | Surface boundary, native window inspection, existing window lifecycle fixtures |
| Initial visibility/draft read or post-send draft sync failure | Retry only the read, cancel hidden-surface retries, retain accepted send receipt and restore the composer after successful observation | Read retry, cancellation and accepted-send synchronization fixtures |
| Caelis stream/callback/Worker failure | Retry the original cursor/receipt independently; only failed Runtime-owner facts change owner connectivity | Component watcher and native protocol fixtures |

Local IM storage contains only user/assistant display messages and attachment
metadata. The initial visible window is 200 messages; explicit earlier-message
loading reads this local database. Codex reconnect uses `excludeTurns: true`,
metadata-only `thread/read`, and optionally one latest `itemsView: summary` turn.
Tool results/images are never loaded as a recovery history. Projection limits
bound Bot allocations, not Runtime or Worker capability.

Verification must distinguish deterministic faults, real local UI, native Runtime
observation, Telegram transport fixtures and published artifact acceptance.
Closing a panel or recovering its renderer never quits Bot or cancels a Worker.
