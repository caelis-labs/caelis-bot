# Isolated native SSH Worker acceptance

`native_worker_fixture.py` is a test-only supervisor for an existing SSH guest.
It requires a newly allocated random private `/tmp/caelis-bot-issue47-worker-`
root. It starts only the supplied official Caelis binary, uses isolated HOME/XDG
paths, and configures a loopback synthetic provider through the public Control
API. Generated Host management credentials remain target-local. A fault proxy
closes downstream after genuine native committed responses; it does not invent
native operations, approvals, artifacts or receipts. The shared Host is never
stopped by client detach. Only the fixture supervisor's explicit termination
stops its own child.

The opt-in Go acceptance requires a private local JSON config with `target`,
`root`, `helper`, `store`, `workspace_root` and test-control `endpoint` fields.
`target` is an existing strict-SSH destination; `helper` is the cross-built
`cmd/caelis-worker-bootstrap`, and `endpoint` is the fixture's target-loopback
origin. None of those private descriptors enter model discovery or test output.
Run `GOWORK=off go test ./internal/nodes -run TestSSHNativeWorkerIntegration -v`
with `CAELIS_BOT_SSH_FIXTURE_CONFIG` pointing to that private file. Without it,
ordinary checks skip actual SSH. Fixture preparation, transfer and teardown are
explicit native operations; the test does not install runtimes, alter SSH trust,
start persistent services, use the user's Store or call an external provider.

Actual acceptance on 2026-09-30 used an already running Linux arm64 Rocky guest,
standard system OpenSSH with existing strict host-key trust, and the official
Caelis 0.65.0 binary verified against its published SHA-256. Public initialization
and receipt provenance hashes are in `native_worker_065_evidence.json`.
The source attestation and model provider are synthetic. This is actual SSH and
actual native Core evidence, not real Codex source or real model/login acceptance.

Verified: target-local independent scoped enrollment, Worker-only start, lost
prompt receipt recovery without redispatch, exact native cancellation observation,
detach leaving the Host and active task alive, and same-grant reconnect completion.
The adapter creates no resident Caelis main Session to obtain Worker rights.

Three separately skipped gates remain incomplete for the shared-native Worker protocol on that runtime:

- Shared Worker `PublishArtifact` failed in the canonical feed with `not_found`.
  Its scoped application-session/configuration/resource API paths also returned
  404. A separate fresh synthetic resource registration probe recorded its
  private durable intent before the native call; this was not a model artifact.
  No resource was fabricated from assistant prose or a file path.
- A synthetic `RunCommand` reached `waiting_approval`, then automatic review
  returned `guardian_unavailable` / `approval_unavailable`, and the action did
  not execute. Public manual controller configuration with the exact Session
  revision and epoch rejected the SDK Worker because it was not an external ACP
  controller. Native human approval and its lost reply are unverified.
- Native cancel returned `committed` and interrupted the exact Worker, but
  scoped `/application/operations/<cancel-id>` returned 404 after reply loss.
  The local journal remains `unknown`; interruption observation is not promoted
  to a receipt, and the adapter does not resend the cancellation.

The numeric revision in genuine Worker command/operation receipts was decoded
by an exact uint64 compatibility method in a separate wire source file. Existing
schema/generated files and Core were not changed; output remains a decimal string.
The temporary supervisor and Host were explicitly stopped and only the recorded
fixture root was removed. Real-model acceptance remains pending human guest
configuration. No HA, Host restart grant transfer or resident Session transfer is
claimed.


## Explicit bounded application Worker

A second, explicitly selected native assembly policy uses
`WorkerProtocolBoundedApplication` in both `caelis.WorkerOptions` and
`nodes.SSHConfig`. The absent value retains the existing shared-native policy;
changing an existing private credential's pinned protocol is rejected. Tasks and
pending native profile bytes remain pinned to their original protocol. This
selector is private native setup policy, never arbitrary model or renderer
routing authority, and there is no automatic protocol fallback.

Pinned official source `8430bff9c36187d4cfaa33576ff238cd36c94c06` (v0.65.0)
supports a genuine bounded ApplicationSession profile with native workspace
execution, creation-bound `workspace-write`/manual permissions, and the native
ReadResource/PublishArtifact bridge. Each such Session executes one actual task;
there is no resident main Session, Bot callback catalog, inherited Bot memory,
MCP or CWD instructions, or claim of ordinary shared/TUI Worker attachment.
An independently enrolled application records its trusted host attestation in a
native background grant, then dispatches `authorized_background`; it does not
invent a Caelis parent user prompt to authorize a Codex activation.

Run only `TestSSHBoundedApplicationWorkerIntegration` against a freshly prepared
fixture, since provider case plans belong to that isolated run. Actual strict
SSH verification took 4.49 seconds with no skipped bounded gates. It verified
canonical resource publication and bounded checksum/ownership download, exact
native manual approval, independently observed native completion/interruption,
lost prompt receipt recovery, and detach/reconnect without replacement grants.
Four native bounded bindings had workspace-write/manual profiles and no callback
or inherited resident tools; zero shared-native Worker grants were created.
Source/helper hashes and public initialization provenance are preserved in
`native_bounded_worker_065_evidence.json`.

Generic native cancel and approval resolution still have no read-only scoped
operation receipt in 0.65.0. Same-POST replay is not a safe indefinite lookup:
operation retention can expire and re-admit a mutation. The adapter keeps the
original exact action journal `unknown`, never resends it, and projects
`Task.Outcome=unknown` alongside independent `Task.Status=completed/interrupted`.
Tests verify that ending, approval absence, and reconnect never confirm or erase
the original lost receipt. This supported v1 uncertainty behavior passed; receipt
recovery itself remains unsupported. Native setup/APP assembly must still select
and present this policy explicitly before full APP acceptance is claimed.
Real provider login/model and Host restart/HA remain outside this synthetic gate.
