# Worker peer application E2E

`make e2e-worker-peer` runs two production `serve` application processes, their
real product HTTP endpoints, built React UI in a browser, actual stdio Bot MCP,
`tasks.Manager`, machine routing, Codex adapter and private native-owner lifecycle.
Only the native Codex wire peer is a deterministic test binary. No product API is
mocked, no account is read and no model is called. This is application acceptance
with a synthetic native Runtime, not live-account, native GUI or cross-machine
acceptance. macOS/Linux test entry points do not claim Windows desktop support.

## Run without downloads

Use existing Go/Node dependencies, Playwright and an installed Chrome/Chromium.
The command does not install packages, browsers, Core or release artifacts.

```sh
WORKER_PEER_PLAYWRIGHT_MODULE=/path/to/node_modules/playwright/index.mjs \
WORKER_PEER_CHROME=/path/to/installed/chrome \
make e2e-worker-peer
```

On macOS the default executable is the existing Google Chrome application. On
Linux set `WORKER_PEER_CHROME` explicitly. Dependencies must already be cached;
Go module network access is disabled. The fixture is built only with the opt-in
`workerpeere2e` tag and cannot run without an explicit isolated root. It is not
included in release builds. Existing configured synthetic Runtime/accepted
introduction documents are seeded only in newly created private A/B directories;
all mutations in the journey use real UI or the real native-to-Bot MCP bridge.
A and B use separate browser contexts, representing different machines rather
than sharing host cookies across two localhost ports. Ambient credential variables
are removed from App children; Codex home and the inactive Caelis profile are
isolated. The command does not alter any installed Bot or tunnel.

## Executable journey and assertions

- Enable sharing on B; Web A submits a wrong code, then corrects the same form and
  pairs. This exercises the real Web operation rejection/receipt path.
- Upload CSV/PDF through the actual browser file picker, select B through real
  `bot_tasks environments`, and create via `bot_delegate`. Both A/B native layers
  read the original bytes and verify names, lengths and SHA-256. Add a decodable
  PNG while continuing the same original task and B native thread.
- B emits an ordinary original request and an opaque native approval; A's React
  controls answer them. The native request IDs, offered choice and original turn
  remain authoritative. Native response and execution effect counts are one.
  Opaque offers stay outside the Agent's filtered approval authority.
- A's synthetic resident uses actual `bot_tasks observe/read/file`, then reads the
  checked local bytes with its native file reader. A's UI previews and downloads
  the output with its original name and bytes. B's product task list excludes the
  foreign task. Stop B's App and verify the retrieved copy still opens on A.
- A transparent test HTTP relay drops a create response only after B produced it.
  A queries the original operation and digest through `bot_tasks reconcile`; B's
  native original input count remains one. The relay never fabricates an API
  success/rejection and does not forward browser or peer credentials into evidence.
- Hold a real Worker upload after partial bytes reach B; click A's visible Stop.
  Native interrupt cancels its real MCP child, the product IPC/HTTP call cancels,
  the original task remains unknown, and B has no native input effect. Reconcile
  the same original operation; a missing native binding is unavailable evidence,
  not proof of rejection or permission to replay. No replacement ID/owner is used.

Output lives in ignored `output/playwright/worker-peer-*/`: source SHA/tree,
platform/build, steps, screenshots, sanitized original operation/effect counters
and cleanup result. Failure snapshots contain synthetic data only. Each run uses
new profiles and cleans its own App/browser processes and detached synthetic
native children after verifying executable/PID/cwd. `WORKER_PEER_KEEP_PROFILE=1`
is a debugging opt-in; remove those explicitly owned profiles after diagnosis.

## Defects exposed by the application journey

Remote approval content hashes cannot serve as numeric renderer revisions: their
values can decrease and exceed JavaScript's exact integer range. The machine
cache now maintains a sorted-view, monotonic approval projection revision, like
the existing local retained-approval view. This does not add approval authority.

An intentional Stop can recycle an App-owned native server for scoped tool
cleanup. It must not reconnect to the now-dead retained endpoint. Only after the
original terminal fact, owned cleanup, and OS process-birth proof of exit does it
reuse the existing owner archive/new-resident recovery. Old receipts/tasks stay
archived on their original owner; B's remote bindings are not migrated. Alive or
unknown process evidence cannot start a replacement. Real private-child tests
cover failure before this repair and recovery after it.

## Still-open gates

The native fixture itself independently verifies that a URL GET has no effect,
that original consent alone is insufficient, and that a native nonce POST plus
original consent is required. **The product B-loopback URL path is not implemented
or accepted by this checkpoint.** A task/original-request-scoped authenticated
channel, isolated browser content, exact native target validation, revocation and
native completion evidence still need a separate implementation/review. This is
not permission to add an arbitrary URL proxy, bypass origin checks, invent an
approval authority, disable the capability or redirect the user to B.

The full response-loss/restart/revocation/two-source matrix still has existing
contract/fixture coverage to combine with further application journeys. Native
form/marked-sensitive/URL application journeys and real Codex/Caelis accounts,
macOS→Linux, macOS desktop lifecycle, same-SHA candidate hardware acceptance and
final independent review remain separate gates. Passing this command alone does
not make the Draft ready to merge or release.
