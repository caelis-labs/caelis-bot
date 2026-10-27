# Remote machines

The existing Settings breadcrumb owns the title. One aligned toolbar shows the
secondary **SSH configuration** and a visible primary **+** Add machine button
with a tooltip and accessible label. Per-machine checks live in the row menu. A **Pairing requests** bell appears only for pending,
unexpired requests. No hero, repeated large heading, scattered footer actions,
account/server dashboard, sharing switch, verification code or helper execution. The SSH choice is network transport only;
see [SSH channels](ssh-connections.md).

Add is a short dialog: Address/SSH choice, then address or configured Host and a
optional Bot port inside Connection options. Address and SSH share the same
small dialog; confirmed pending delivery stays in that dialog. Empty inputs
have placeholders; “Default 9317” is not a submitted value. The target name is read
from the original peer receipt; an established machine can be renamed in its same
settings dialog. There is no one-field accordion or duplicate name label.

Sending returns after B stores the request, never waits for human approval. Only a
confirmed pending row says “Request delivered; approve it in the other Bot.” An
unknown result only offers checks of its original request; changing its reachable
address uses the existing pinned-peer read boundary. A host-proven undispatched
intent can correct its address and explicitly send the same original intent. A
possible write is never cleared or retried with another ID/proof/owner.

The list distinguishes loading, empty, read failure, pending, unknown, declined,
expired, revoked and native readiness. Failed refresh preserves cached rows and
marks missing freshness. A local one-shot deadline repaints expired readiness and
incoming requests; it performs no network request. Page-entry/manual checks are
bounded, max three concurrent. Pending authorization is never polled in background
or offered for delegation.

The pairing-request dialog shows the self-reported name, observed address and proof-bound
fingerprint, with Decline/Allow. Names and loopback addresses are not identity
proof. Only B's authenticated local user may decide; the reverse direction needs
its own authorization. Handled/expired requests leave the inbox. Source revocation
remains in a collapsed list, changes only its direction and does not stop work.

## Errors and recovery

Read-only `WorkerSharing` reports state; explicit `RefreshWorkerSharing` coalesces
an attempt to reopen the **same** failed registry or prepare a missing listener.
It never reopens a healthy server, changes identity, grants authority or creates a
native owner. App close fences retained handlers and late factories. Local chat,
Workers, health and updates do not wait on this remote feature.

Registry version/permission/unreadable failures are distinct from listener and
Worker-readiness failures. Fixed safe messages offer Retry/diagnostic export;
original native errors, paths, tokens and private JSON are not rendered. A known
sign-in/model/configuration request shows its precise reason and a direct
Connections & Models action on the machine that needs it. Unknown errors do not
invent a configuration change for the user.

The unreleased v1 experiment is not migrated. In the existing isolated Dev, an
empty disabled v1 registry prevented the v2 service from starting; it was **not**
a missing Runtime/login/model/effort. Its one-time cold cleanup belongs to that
Dev upgrade, outside product code. Nonempty, malformed or incompatible state
remains fail-closed, with its contents and original effects preserved.

## SSH connection

Select an existing local OpenSSH Host, or enter a host, SSH port and username.
Manual authentication accepts a transient password or a selected local key and,
when needed, a transient passphrase. The remote Bot service port is separate.
There is no SSH file-management UI. Existing private/system configuration files
are consumed internally for compatibility and are never deleted with a connection.

## Local deletion

Delete permanently removes the connection profile and pairing proof, and cleans
its matching legacy credentials. Old task IDs remain in history with their
original environment unavailable. No remote unpair/stop request is sent. Re-add
uses a new identity and does not inherit the deleted connection's tasks or grant.
The confirmation and result reflect this behavior. Pending cleanup checks the
original local receipt; a lost reply never starts another deletion. Expired
receipts require reviewing current state rather than claiming a historical result.

## Validation

Actual native AppKit/WKWebView screenshots cover light/dark at 1280×900,
900×680 and 620×720: empty/list/Add/SSH selection/configuration/preview/pending/
authentication/incoming requests. The settings transport is synthetic; these are
native rendering checks, not Runtime or account acceptance. The actual React
browser matrix also covers the row menus, scoped editor dispatch, original unknown
checks, read recovery, local freshness deadlines and deferred SSH callbacks.
Independent review checks alignment, hierarchy, spacing and the two themes.

Use existing cached Chrome/Playwright (no install):

```sh
WORKER_PEER_PLAYWRIGHT_MODULE=/path/to/existing/playwright/index.mjs node script/remote-machines-ui.mjs
BOT_REMOTE_NEXT_UI_MATRIX=1 BOT_SETTINGS_PREVIEW_ROOT=/private/tmp/owned-preview BOT_SETTINGS_PREVIEW_EVIDENCE=/private/tmp/owned-evidence bash script/desktop-settings-preview.sh
```

Actual A/B HTTP/React application and real installed Codex + isolated Mock MCP
journeys are separate acceptance commands documented in [worker-peer-e2e.md](worker-peer-e2e.md).
The bounded URL contract and its explicit first-version exclusions are unchanged.
Preview/tests do not restart user instances. A separately authorized Dev update
uses the validated candidate and preserves its existing data; the release App is
untouched. The development-default profile selector is documented in SSH channels.

![Native empty, synthetic data](images/remote-machines-empty-native.png)
![Native compact list, synthetic data](images/remote-machines-native.png)
![Native Add machine, synthetic data](images/remote-machine-add-native.png)
![Native pairing request, synthetic data](images/remote-machine-request-native.png)
![Native SSH configuration list, synthetic data](images/remote-machine-ssh-config-native.png)


Deletion acceptance: run `CAELIS_DELETE_UI=<empty control directory> go test
./internal/machines -run TestDeleteUIService -timeout 22m`, then use
`script/delete-machines-ui.mjs` with `WORKER_PEER_PLAYWRIGHT_MODULE` pointing to
an existing Playwright installation. The browser script uses the control file at
`.cache/delete-ui/ready.json`; write `.cache/delete-ui/stop` after inspection.
The Go service and peer handler use disposable roots; general settings metadata
is synthetic. Native SSH/Keychain acceptance remains separate and opt-in.
