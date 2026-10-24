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
  pairs. The relay also drops an original pair before B sees it (zero grants).
  A offers explicit fresh-code authorization, refuses reuse of the old code,
  permits correction of a known bad new code, then reconciles a real B grant
  whose response is lost. Refresh and rename keep the same connection/proof;
  the original unknown receipt and browser pending ID remain unchanged.
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

## Additional URL journey and remaining gates

`make e2e-worker-peer-real-url` now runs actual A/B/React with **real installed
Codex**, an isolated ordinary Mock MCP and controlled local Responses model.
It does not replace Codex with the native fixture. The clean `de4b791c` six-case
journey verifies B-loopback static form completion through A, original native
consent/continuation, at-most-once effects, decline, expiry, lost peer/provider
responses and verified native-owner exit. See [contract and first-release
scope](worker-peer-url-contract.md) and [repeatable entry](worker-peer-url-forms.md).
The old synthetic URL negative-control probe is historical; `--url-contract`
forwards to the real-Runtime entry.

The first release retains public reachable normal-browser HTTP(S), including
scripts/OAuth with reachable callbacks, plus the bounded static form portal.
B-only localhost multistep/GET/JS/redirect/different-form-action pages, public login
callbacks to B-loopback and B-only certificates/browser state are explicitly
deferred limitations. They are not repaired by safe refusal and do not justify
an arbitrary URL proxy, external-forward opener or B-side user approval.

Physical macOS headless A → Fedora headless B (real Codex/controlled model/MCP)
and a separate native macOS GUI → Core v0.73.2 controlled-model journey exist at
`8f203370`. They do not establish later-head, real-account, remote-Worker GUI or
full lifecycle/upgrade acceptance. Remaining interaction/loss/restart/revoke/
two-source/local-helper checks must retain their actual evidence layers.
Q22 isolated account/model input is still missing; dependent real-account steps
remain unperformed without another question and are nonblocking follow-up under
the latest review scope. Final-head checks, affected application
journeys and whole-PR independent audit are required; no merge or release is
authorized by a successful test command.
