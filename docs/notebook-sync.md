# Notebook backup and manual node switch

This opt-in path copies ordinary files from the active Bot's actual Notebook to
stopped SSH backup nodes. Each node retains its complete Codex/Caelis runtime,
configuration and authentication. The default APP + local Runtime needs no SSH,
broker, node daemon or background deployment. No automatic failover, session
migration or snapshot protocol is involved.

## Product entry points

1. Open **Settings → Runtime and models**. Add an existing SSH node, then detect
   or install its Runtime and complete its own sign-in using the existing setup.
   Batch settings can apply conversation/Worker preferences and supported Caelis
   configuration to selected nodes, with per-node results and original receipts.
2. Existing Worker selection remains **Worker node → connect → choose for work**.
   A Worker task uses the exact chosen node/backend. Installation or a successful
   settings write alone is not evidence that a task ran there.
3. Expand **Notebook backup**, select stopped SSH nodes and their Runtime, choose
   an interval (1–1440 minutes; default 5), enable periodic backup and save.
   Saving prepares the fixed private target APP profile using that node's own
   Runtime paths and execution preferences. The UI accepts no SSH commands,
   profile paths, credentials or native session IDs.
4. **Back up now** performs the same one-way ordinary-file copy. Each node shows
   its last confirmed successful backup. Failure retains the last success time.
5. **Switch to this node** confirms the action, checks target Runtime readiness,
   freezes old Bot admission, stops its actual owner, confirms the stop, verifies
   the standby, performs the final copy, reconfirms the old stop and starts a new
   native conversation. The new owner's identity and existing product proxy must
   connect before a successful switch is recorded.
6. APP restarts through its existing normal entry point to use the saved pairing.
   The active Bot automatically becomes the backup source and the previous source
   becomes a stopped backup with its original Runtime. No settings rewrite is
   needed after a confirmed move. Remote-to-remote moves use the same sequence.
7. To return, choose **This machine → Switch to this node**. The remote owner must
   confirm stopped, then the final ordinary-file copy returns to this exact APP
   profile. The previous local Notebook is retained in full under
   `Product/notebook-before-return-<original operation>/Notebook`. Old local native
   conversation/Memory bindings are retained separately as for other fresh starts.
   APP then restarts its normal in-process local Runtime using existing local
   preferences/authentication. The return remains pending until that Runtime
   actually reports ready; startup rechecks the original remote owner's stop.
   SSH loss blocks local startup, and does not claim a completed return.

Only the current APP profile is a local target; no arbitrary local path is
accepted. Its retained native stop receipt and unlocked profile owner lock are
required before a return. Use an isolated APP profile for acceptance so the
original user's Bot/Host and Notebook are untouched. Outgoing-only nodes lack
an ordinary direct file-transfer pairing and are explicitly unsupported here.
Remote TaskDock interactive terminals remain unsupported and report that fact;
they do not prevent ordinary task execution, backup or node switching.

## Production composition and ordinary owner

`app.New` calls `attachDefaultNotebookSync` after node management/roaming
assembly and before any personal preparation or start. The default controller
is dormant with no targets. Saved enabled settings call `AttachNotebookSync`
with concrete local or remote ownership hooks. A user settings change stops the
old timer before replacing its native controller; browsing settings does not
start synchronization. The timer belongs to APP's lifetime.

Private local settings are `nodeplane/notebook-settings.json`. The backup and
original switch intent are `nodeplane/notebook-sync.json`. These are native
records, not files transferred by rsync. An uncertain action keeps its original
switch ID and non-ready phase; no new stop or start is dispatched to retry it.

The native one-shot `caelis-node notebook-owner --directory <enrolled directory>
--node-id <exact enrolled ID>` command accepts a closed JSON request through the
existing sanitized SSH channel. It prepares, observes, stops or starts the fixed
`<enrolled directory>/notebook-bot` APP profile. Its actual Bot process is the
existing entry point:

```sh
caelis-node serve-bot --profile ABS --listen 127.0.0.1:0 \
  --auth-file TARGET_PRIVATE_PRODUCT_AUTH --owned-node-id EXACT_NODE_ID
```

This uses the complete ordinary application services. Codex uses the target's
App Server/authentication; Caelis uses its existing designated, marked owned Host
and watchdog. Shared/uncontrollable Caelis Hosts cannot supply stop proof.
`NewOwnedResident` does not require or create a roaming generation, broker or
snapshot. The existing product proxy credential format is generated on target,
kept private there and never returned/copied to APP. Runtime login credentials
are never transferred.

`Product/owner.json` publishes only the loopback endpoint and product identity.
Stop uses the existing product command, freezes admission and checks active,
pending and uncertain work before the concrete runtime fence. Releasing the
actual `.product-owner.lock` confirms owner exit; SSH failure proves nothing.
Start records its original operation before spawning a detached `serve-bot`;
a failed/unknown start is not replayed. Successful starts are observed through
the actual product identity, then through the existing SSH product proxy.

A previously activated, confirmed stopped target can start a new conversation.
Its old native bindings, personal Memory evidence and product task/admission
records are retained locally under `Product/retired-<original start ID>/` before
fresh initialization. No old binding or receipt is resumed; none of these native
files are transferred to another node. Notebook and old user artifacts remain
in place. This is preservation of the target's original files, not a migration
or restore mechanism. Current target execution preferences are read again at
startup; no source machine's CLI path/model configuration is substituted.

## File semantics and handoff

Both endpoints need existing `rsync`. Transfers reuse strict host trust and the
sanitized enrolled SSH metadata. Remote-to-remote transfers stage only ordinary
files in a disposable private local directory, without needing source-to-target
credentials. Staging is removed after the attempt. Periodic copies are not a
point-in-time transaction across concurrent edits.

Includes `MEMORY.md`, dated/user notes and ordinary attachments. Excludes hidden
paths, generated `INDEX.md`, periodic `HANDOFF.md`, SQLite/database sidecars,
runtime/session/history folders, common credential filenames, locks, sockets
and temporary files. Exclusion patterns are case insensitive. Links and special
files are refused; Notebook directories and their ancestors must be canonical.

No `rsync --delete`, file deletion, automatic merge or retention cleanup occurs.
Replaced target content is retained under
`Notebook/.caelis-sync-conflicts/<attempt>/`. Source deletions leave destination
files intact. Final switching refuses extra old destination files, preventing
forgotten notes from becoming active again; they remain available for explicit
review. An unresolved target `HANDOFF.md` also blocks switching without deletion.

Only a host-confirmed completed Dream handoff whose current file still matches
is transferred after stopping. Remote owners preserve this proof privately and
recheck it at final transfer. Unfinished, stale, edited, missing or consumed output
is not transferred. The new conversation uses the existing one-use digest-based
Notebook context consumption. Fresh local Memory evidence is built normally;
SQLite, credentials, session bindings and runtime history are never synchronized.

## Verification and live acceptance

Local fixture checks exercise real temporary files/rsync, conflict preservation,
excludes, stale-note refusal, controller stop ordering/uncertainty, default dormant
composition, startup fencing, target preferences, profile owner locks and fresh
session preservation. Frontend checks exercise the actual settings components.
Compilation/fixture coverage is distinct from real deployment and model/GUI
acceptance. No existing user Notebook or real node is modified by these checks.

The independent real-node acceptance should use a fresh isolated APP profile,
current packaged helpers and normal new sessions. Verify SSH enrollment and
Runtime setup, per-node batch results, an actual selected Worker task, timed and
manual file backup, preserved overwritten files, and stop-first switch/reconnect.
Verify local → remote → local with automatic backup source/target reversal,
local old-Notebook retention, and a fresh local session without old binding.
Also verify busy/unknown refusal and a lost SSH response without a second owner.
Record each actual runtime owner/Worker result independently. The old QA thread's
active-writer conflict is not evidence that its original session resumed.

## Existing product RPC control entry

The complete ordinary `serve-bot` owner, including `--owned-node-id`, composes
these same Node/Worker/Notebook controllers. Its inspected identity advertises
`capabilities.nodeManagement`. This is native user settings access over the
existing authenticated loopback product listener or `proxy-product` SSH stdio;
it is not a Bot tool, a new credential scheme, or a generic Service dispatcher.
The GUI's switch confirmation remains unchanged. An authenticated native caller
must explicitly request the switch action; native idle/stopped checks still run.

Read via `POST /v1/management/nodes` with the currently inspected `botId` and
`generation`. The closed `action` values are:

- `catalog`: `catalog` contains the authoritative enrollment/catalog revision.
- `workers`: `workers` contains its own revision and exact enrolled Worker facts.
- `notebook`: `notebookSettings` and `notebookState` contain settings, last success,
  failure/phase and original switch operation.
- `configuration`: include `nodeId` and `backend` for target Runtime configuration,
  detection and reviewed installer versions.
- `enrollment`: include `nodeId` equal to the original enrollment operation ID.
  This is the authoritative native original receipt, including recovery of a
  verified existing SSH Node after bootstrap observation loss. Do not resubmit
  Add with a new ID; the outer command receipt remains a historical observation.
- `operation`: include the unchanged original `operation` NodeOperationRef.

Mutate through the existing `POST /v1/commands` journal, with one stable original
`id`, the inspected scope, `kind: "manage-nodes"` and one `nodeManagement` payload.
For example, after reading the current catalog revision:

```json
{
  "botId": "INSPECTED_BOT",
  "generation": "INSPECTED_GENERATION",
  "id": "enroll-fedora-original",
  "kind": "manage-nodes",
  "nodeManagement": {
    "action": "add-node",
    "add": {
      "operationId": "enroll-fedora-original",
      "label": "Fedora",
      "join": "ssh",
      "sshDestination": "EXISTING_SSH_ALIAS_ON_THIS_OWNER",
      "expectedRevision": "CATALOG_REVISION"
    }
  }
}
```

The remaining payloads map to the existing Service methods:

| `action` | Sole payload | Existing method |
| --- | --- | --- |
| `detect-node` | `nodeId` | `DetectNode` |
| `configure-node` | `configuration` (existing guard/ref and semantic change or installation) | `ChangeNodeConfiguration` |
| `save-worker-node` | `worker: {nodeId, backend, revision}` | `SaveWorkerNode` |
| `probe-worker-target` / `connect-worker-target` / `disconnect-worker-target` | same `worker` payload | Exact Worker target methods |
| `save-notebook-settings` | `notebookSettings: {enabled, intervalMinutes, targets: [{nodeId, backend}]}` | `SaveNotebookSyncSettings` |
| `sync-notebook` | `nodeId` | `SyncNotebook` |
| `switch-notebook-node` | `nodeId` | `SwitchNotebookNode` |

Worker labels and routes are resolved from this owner's enrolled catalog. No
wire input may supply a Worker transport/helper/socket/Store/workspace path;
worker observations omit these native fields. Notebook observations preserve
last-success and phase while categorizing private native errors. Native settings
revision checks, existing ownership and configuration guards are not relaxed.

The product command's `outcome` and optional `nodeManagement` view confirm only
that operation's existing result. An uncertain native switch remains `unknown`.
A lost response must use `/v1/receipt` with the original ID, then re-read the
relevant current state; replay never dispatches again. Stored receipts do not
cache current node/Worker/Notebook views. Node configuration uses the native
`nodeplane` semantic validator and length-prefixed digest. Copy the exact opaque
`guard.revision` into `change.expectedRevision`; Codex may return a 64-character
SHA256 revision. Both `conversation-model` and `worker-model` are supported. Native enrollment/configuration unknown
outcomes use the original `enrollment`/`operation` lookup, not another command ID.

After a confirmed headless source switch, official `stop-bot` closes that already
fenced owner normally and releases its profile lock. `serve-bot` still requires a
resident local profile: it cannot become a thin remote APP or bypass that existing
restriction. The ordinary desktop APP remains the original profile's controller
for remote source backup and return to this machine. A GUI automation blocker is
therefore separate from successful authenticated headless control; do not claim
full desktop round-trip acceptance using headless wire calls alone.

Packaged enrollment artifact preflight can run without starting a Bot:
`caelis-node inspect-node-artifacts`. It verifies both Linux architectures through
`DefaultNodeAgentArtifact` and, on macOS, both signed native helpers through the
existing checksum/Mach-O validators. It prints the verified build source, package
paths and artifact digests; it opens no profile, Runtime, credential or SSH channel.
APP executables resolve `Contents/Resources/NodeAgent`; standalone helpers resolve
only their adjacent manifest. Build revision injection and all manifest/source/
checksum/architecture checks apply to standalone host helpers as to APP.
