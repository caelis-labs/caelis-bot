# Managed Linux Bot updates (protocol 1)

This is the first managed Linux implementation. The shared Web settings owner can
integrate `frontend/src/managed-update-contract.ts`; this change does not replace
chat, split-panel, cancellation, or existing macOS/Windows updater UI. No public
Linux Bot release is implied by the implementation or the QA artifacts.

## Deployment and ownership

One stable `caelisbot-launcher` owns one private, same-user installation root and
one external private Bot profile. It can run as a user process or as the main
process of a user-owned systemd unit. The launcher remains alive when its Bot child
changes. Do not run each version as a separate service or restart a shared Runtime
unit. An installation root, its profile and configuration must be absolute,
canonical, owned by the current UID and private. No elevation or directory
migration occurs from the browser.

The install root contains `installation.json`, `current.json`, `update.json`, a
private Unix control socket, staging/download directories and immutable
`versions/<signed-manifest-sha256>/`. `current.json` is an atomic, fsynced selector.
The profile contains all user state and is never backed up, replaced or rolled
back with code. A process-wide flock is inherited by the Bot (and marked
close-on-exec before spawning helpers). If the supervisor dies while its Bot is
alive, another launcher refuses ownership. After an explicit service stop or a
host reboot, restart the same launcher/configuration; never start a second
profile from that install root. Direct execution from a managed version directory without its launcher is rejected,
including attempts to use a second profile. A separate bundle copied elsewhere is
an independent unmanaged installation.

Root-owned/read-only/system-package installations expose check/status but cannot
install. Detected containers expose `mode:image` and require their image host;
`init` and `serve` refuse that deployment. Unrecognized container environments
must also be configured/operated as image-managed, not as an in-container managed
installation. This is not a generic package manager or a privileged service
installer. Initial migration of an existing installation is a separate operator
action; this work does not migrate or restart an existing experience service.

## Trust and package contract

Production checks only the fixed `caelis-labs/caelis-bot` GitHub release feed.
Compatible assets must include `linux-<arch>-update.json`, its `.sig`, and the exact
archive named in the manifest. Missing Linux metadata/assets produces
`no_compatible_update`, not success or a guessed download link. HTTP/network
failures and invalid metadata remain failures. No valid production key is yet
pinned in `internal/managedupdate/release-key.txt`: publication requires a reviewed
persistent Ed25519 public identity and independent key custody first. If signed
Linux metadata appears before that prerequisite, checks fail closed with
`release_trust_unconfigured`.

The signature covers the exact manifest bytes, binding product/flavor/platform,
stable channel, version, full source SHA, build kind, launcher protocol, compatible
state schema, compressed size/SHA-256, expanded budget and every member's
path/size/mode/SHA-256. Only the Bot executable, bundled Caelis executable, Runtime
pin/license, code/character/third-party licenses and README are allowed. Both executables must be ELF64
for the target architecture. The embedded Runtime pin must match its actual
binary. Tar links, unexpected members, traversal, duplicate members and expansion
beyond the manifest are rejected before the old Bot closes. Limits are 512 MiB
compressed, 1 GiB expanded and 64 members; actual space must also retain at least
256 MiB or 10% of the next allocation, whichever is greater, plus 128 free inodes.

The stable launcher is distributed alongside the initial archive and is not
self-updated by protocol 1. Release builds require a clean tree, exact version tag
and matching package version. `script/build-managed-headless.sh` builds the Bot,
launcher and unsigned manifest, never signs or publishes. It reuses the existing
pinned Runtime cache and checks its checksum and ELF architecture. Signing is an
explicit separate invocation of `script/linux-update-manifest.mjs sign`; a
production signature must match the embedded public key. Linux runtime identity
must additionally be read back on a real machine before release.

QA builds require `BOT_UPDATE_KIND=qa`, a `-qa.N` version, the `managed_update_qa`
build tag and an explicit private `--qa-trust` file with an ephemeral public key
and a numeric loopback HTTP origin. Production builds reject that configuration.
No QA key is a default, a browser option, or an environment-variable trust
bypass. QA versions never qualify as stable offers. Clearly label all QA archives
and evidence as synthetic, not published upgrades.

Example build (from an isolated checkout; existing dependencies/caches may be shared):

```sh
BOT_UPDATE_KIND=qa BOT_UPDATE_VERSION=0.17.1-qa.1 \
  BOT_UPDATE_ARCH=arm64 bash script/build-managed-headless.sh
```

Initialize once with explicit local, verified archive/manifest/signature paths:

```sh
caelisbot-launcher init --root /absolute/private/install \
  --data-dir /absolute/private/profile --listen 127.0.0.1:9317 \
  --archive /verified/archive.tar.gz --manifest /verified/manifest.json \
  --signature /verified/manifest.json.sig
caelisbot-launcher serve --root /absolute/private/install
```

Use `--require-pairing` and exact `--origin` for an upstream behind a proxy, as for
the existing headless server. QA commands additionally require `--qa-trust` in
both invocations. Do not paste private signing keys into these commands.

## Web integration contract

All routes remain behind existing cookie, Host, Origin and CSRF checks. Use the
existing transport authentication headers, but no composer ownership is needed.

| Route | Result |
| --- | --- |
| `GET /api/v1/updates/status` | Shared typed status; poll about once a second during work. |
| `POST /api/v1/updates/command` | `id` plus `action` (`check`, `download`, `install`, `cancel`, `automatic`). |
| `GET /api/v1/updates/receipts/{id}` | Original managed command receipt after timeout/reconnect. |

`check` and `automatic` take no target. `download`/`install` use server-issued
`offerId`; install additionally uses `when:now` or `when:idle`. Both wait for safe
idle; `now` does not authorize killing active or unknown work. Cancellation needs
the exact `transactionId` and current `revision` and is rejected once committed.
Browser commands never accept a URL, path, shell string, executable or trust key.

Persist the original command ID and body before sending. Retrying identical
bytes returns the same receipt; changing the body under that ID conflicts.
Different tabs installing the same offer join the same transaction. An accepted
receipt proves durable acceptance, not installation. On an ambiguous HTTP result,
read the original receipt and status; do not create a replacement operation. The
bounded journal retains up to 4,096 command identities and fails closed when
full, rather than dropping deduplication evidence. External checks are read-only
and return `state:read_only`, with no durable installation receipt.

UI states are `idle`, `checking`, `no_compatible_update`, `available`,
`downloading`, `verifying`, `ready`, `waiting_idle`, `restarting`, `installed`,
`rolling_back`, `rolled_back`, `failed`, `unknown` (external checks can also return
`check_failed`). `receivedBytes/totalBytes` are actual transfer counts; a percentage
only describes download. Display verification/restart as stages, not fake progress.
`testArtifact` must visibly mark QA. Keep the user's original product/session
navigation; don't expose internal boot/transaction IDs as the main UI.

After disconnect, reconnect with bounded backoff and read both the original
receipt and status. Only show installed after `state:installed`, the expected
version/source/kind and a new `bootId` are read back. `rolled_back` must show the
old running identity and the failed transaction. A timeout, dead PID, 100% bytes,
selected directory, HTTP response or accepted receipt is not a health result.

## Safety and recovery

Checks/downloads are automatic by default (at startup and at most daily); no
restart is scheduled until a user confirms or arranges an install. Closing a tab
does not cancel accepted work. Download interruptions can restart the same
immutable offer; Range resume is deliberately absent. Staging verifies complete
bytes before readiness. Retention preserves current, one previous rollback
version and one candidate. It removes only installer-owned scratch/download
material and authenticated obsolete versions, never unrecognized files or
product state.

The final handoff excludes Web mutations, checks pending/unknown Web operations,
pauses resident/task admission and uses existing backend restart preparation.
It waits for running/sending/unknown work, unresolved receipts, approvals,
questions and Runtime setup/install/switching. Failed preflight leaves normal
work and decision admission open. No work is replayed, no approval is answered,
and no Session is replaced to facilitate an update. Core Close retains the
existing update detach behavior; this is not a new task/runtime owner.

Channel handoff stops and joins the existing receive/delivery loops, including
file delivery, and confirms the final durable checkpoint. Enabled/paused settings,
queued inputs, original receipts and historical unknown deliveries are preserved.
Pairing and in-memory secret answers wait for completion; configured, paused or
offline channels alone do not block installation. Unknown operations are never
resent to make an update succeed.

The signed whole package may carry a new Caelis pin. On an already used, selected
Caelis connection, the candidate offers its bundled CLI through Core's existing
`service start` owner before health acknowledgement. Core decides idle replacement
or deferral; idle Sessions do not promise detached/background task survival. Bot
never kills or downgrades a shared Host. The actual authenticated Host version,
build and instance are read back separately from the bundled version and refreshed
while running. A deferred Host is shown as pending, not as the new version running.
A startup/readback failure prevents candidate health and retains its real failure
category. Fresh profiles and externally selected Codex do not start a dormant Core.

Health confirms the signed Bot identity, profile lock, listener, local state and
applicable Core handshake. It does not prove model authentication or task success.
Candidate failure may restore the previous Bot code, but never rolls back user
data or requests a Core downgrade. No general migration/reversal contract is added.
External Codex remains under its existing explicit installation flow.

Durable phases are waiting/requested → committed → switching → switched → healthy,
or rolling_back → rolled_back/unknown. Only confirmed old-owner Close can commit;
only observed child exit allows replacement. On restart, no commit means old
version; committed/switching resumes that same transaction; switched without a
persisted health acknowledgement rolls back. The previous version is restarted
once after a new child cannot start or reach health within 45 seconds. A rollback
failure stops with unknown; it never loops. Supervisor loss with an orphan child
retains the flock and blocks takeover. Power-loss tests simulate persisted phase
boundaries and abrupt process termination; they are not physical power-cut proof.

## Qualification and integration handoff

Run the managed Go race tests, `script/linux-update-manifest.test.mjs`, repository
checks/smoke, the two Linux architecture builds, and the isolated Linux QA runner.
Keep source/unit/fixture, actual Linux execution, cross-compilation and public
release evidence separate. No model or user account is needed for the basic
A/B/failed-candidate loop. Real connected-channel and Runtime-session qualification
must not be inferred from that loop.

No Bot-core skill update is needed: this slice only adds settings/update-owner
operations, with no new Bot tool, conversational update authority or recovery duty.
Web Settings now consumes the independent contract through the existing authenticated
transport and shared settings components. It retains the original command in tab
storage before dispatch, reads its receipt after ambiguous replies/reload, and
does not resubmit an unknown command. Installation success requires the original
transaction, expected version/kind, signed-owner source identity and a new boot.
Download percentages use actual bytes; unknown totals are indeterminate. Channel
handoff and Runtime-pin refusal remain explicit local reasons. External/image
installs show checking and their actual management boundary, without install
controls. No native updater UI or channel lifecycle is replaced.

`script/managed-update-client.test.mjs` covers command retention and confirmation.
`script/managed-update-browser.cjs` runs the shared production UI against explicitly
synthetic owner responses for narrow/wide layouts, lost replies, cancellation,
progress and rollback presentation. These fixtures do not qualify public releases,
real accounts, connected channels or a cross-pin transition. Initial migration,
production signing and publication remain separate operator decisions.

The Linux QA runner accepts separate `--a`, `--b`, `--bad`, `--public-key`,
`--root`, `--port` and `--feed-port` paths/options. It refuses an existing root and
port 19317. Run a second fresh root with `--pending-operation` to prove an
unresolved original Web operation prevents installation and survives cancellation.
A/B must be real QA Bot bundles; BAD is deliberately fault-injected QA content,
not a source-authentic release or evidence of a public Linux asset. The runner
records evidence in its private root and stops only its own created processes.
