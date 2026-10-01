# Simple Notebook backup (Issue47, opt-in native assembly)

This path copies ordinary files in the actual `<APP profile>/Notebook` from one
active Bot to stopped backup profiles. Every node retains its complete existing
Codex/Caelis runtime and its own authentication. It installs no daemon, login,
execution agent, snapshot protocol, session transfer, or consensus mechanism.
The existing cold migration/roaming path is retained unchanged.

## Implemented entry points

`app.AttachNotebookSync(application, app.NotebookSyncOptions)` attaches a native
controller **before `Application.Start`**. `SourceNodeID`, `Profiles` (actual APP
profile directories), `Interval` (at least one minute), and concrete native owner
checks are required. Existing enrolled direct SSH destinations are read from the
APP's node management document; no new SSH connection or credential is configured.
Outgoing-only nodes without a direct file-transfer pairing are explicitly unsupported.
The APP starts/stops its timer. No synchronization runs merely by constructing
`Application.New` or browsing settings.

The existing Wails/backend service exposes:

- `NotebookSyncState()` → `{sourceNodeId, targets:[{nodeId,lastSuccess,lastAttempt,error,phase}]}`.
- `SyncNotebook(ctx, nodeID)` → ordinary one-way backup and the resulting status.
- `SwitchNotebookNode(ctx, nodeID)` → persist intent, stop source, confirm actual
  stop, verify stopped standby, perform final sync, reconfirm source stop, then
  invoke the supplied existing target fresh-session start action.

UI integration should call `NotebookSyncState`, resolve labels from the existing
node catalog, and display **last successful backup** plus the current error.
An empty `lastSuccess` means no confirmed successful backup. Failure keeps the
old successful timestamp. `phase` other than `ready` requires native recovery;
it is never an instruction to launch a second Bot. UI integration remains with
the settings worker; no same-page frontend edits are included here.

## Actual remaining production gap

**`AttachNotebookSync` is not called by default production composition.**
The next native integration stage must call it after `AttachNodeManagement` and
before any `PreparePersonal`/`Start`, supplying verified actual target APP profiles,
`Hooks.StandbyStopped` and `Hooks.StartFresh` from the existing target lifecycle
owner. For a remote active source it must also supply `SourceActive`, `StopSource`,
`SourceStopped` and, optionally, an exact completed handoff reader. The ordinary target startup to adapt is `cmd/caelis-node/run.go`'s
`serve-bot --profile ABS --listen 127.0.0.1:0 --auth-file PRIVATE`; it reuses that
target's already configured runtime and existing authentication. The prepared
profile must omit an old native conversation binding so it creates a fresh
session; copying those bindings is outside this sync path. Target owner process
start/stop and product-pairing observation still need concrete integration there.
The next source
must be reconfigured explicitly after a successful move; this version does not
reverse backup direction or automatically fail over.

The local-source path is concrete: if local source hooks are omitted,
`NotebookLocalSourceHooks` uses `Application.PrepareUpdate`,
`guardRuntimeChange`, `codex.Session.FenceOwnedForBootstrap` (or the owned Caelis
`FenceStop`), and `retireRoamingSource`. Shared/uncontrollable Hosts cannot provide
stop proof. Failed/unknown stop or start stays fenced and requires the existing
native ownership recovery; it is not retried with a new ID. Saved non-ready intent
blocks restarting this old APP when this assembly is restored.

`nodeagent.ManagedStartPort.StartManagedRoaming` and
`Application.PrepareRoamingBootstrap` depend on the existing snapshot/generation
workflow, so they are **not** claimed as a working fresh-session target adapter
for this simple directory path. No actual remote Bot start has been implemented
or verified by this change. The remaining production composition/target adapter
is one integration gap, not a completed remote switch. Target identity and normal
fresh-session initialization must be prepared there using the existing portable
identity mechanisms; rsync never copies `bot.json` or SQLite.

## File semantics

Both endpoints need existing `rsync`. Transfers use SSH with the existing
sanitized connection metadata and strict host trust. Ordinary files are staged
in a private disposable local directory for two transfers, including for two
remote nodes, without needing credentials on the source. Staging is only
ordinary temporary files and is removed afterward; it is not a versioned snapshot
system. A periodic copy is not a point-in-time transaction across concurrent edits.

Includes `MEMORY.md`, dated/user notes, and ordinary attachments. Excludes hidden
paths, `INDEX.md`, `HANDOFF.md`, SQLite/database sidecars, runtime/session/history
folders, common credential filenames, locks, sockets and temporary files.
Exclusion patterns are case insensitive. Links and special files are refused;
APP profile ancestors must already be canonical. Source/target directories must
already exist; this path does not initialize or adopt an arbitrary profile.

There is no `rsync --delete` and no deletion of user files. Replaced target content
is retained under `Notebook/.caelis-sync-conflicts/<attempt>/`, excluded from later
copies and normal Notebook indexing. Source deletions leave destination files in
place. Before activation, the final sync reports extra old destination files and
refuses to start, preventing forgotten notes from silently becoming active again.
Those files remain untouched for explicit review. No automatic merge or backup
retention cleanup is implemented.

`HANDOFF.md` is Dream output, read by `PrepareContext` and consumed once by digest
after accepted native input. Periodic backup never transfers it. The concrete
local stop adapter captures only the host-confirmed, current completed Dream
output (`Runtime.CompletedNotebookHandoff` / `Vault.CompletedHandoff`), freezes
and stops the source, then compares the still-current file before final transfer.
Unfinished, stale, edited, missing or consumed output is not transferred. A
standby's existing `HANDOFF.md` blocks final switching without deleting that file.
The new session continues to use the existing one-use context consumption.

## Validation scope

Passed on isolated temporary Notebook directories:

```sh
GOWORK=off GOPROXY=off GOCACHE=/tmp/caelis-notebook-go-cache \
  go test -p 1 ./internal/notebooksync ./internal/notebook
```

Real local rsync verifies notes/attachments, excludes, preserved replacements,
no deletion, final stale-note/handoff refusal, and transfer failures. Controller
fixtures verify stop → final sync → fresh-start ordering, retained success time,
and no re-dispatch after failed/unknown stop, final copy or start. Remote SSH
argument/relay behavior is fixture-only; no real remote transfer was run.
Changed Go files pass syntax parsing and `git diff --check`.

APP/backend/nodeagent/Bot package compilation and their lifecycle integration
have **not** been run. Full build, race, model, GUI, broad tests and real multi-node
acceptance remain for the coordinated heavy-check stage. No user Notebook,
credentials, Bot55827 or Host18649 were read/copied/changed by validation, and no
persistent service was deployed. This is an implementation-stage deliverable,
not end-to-end production acceptance.
