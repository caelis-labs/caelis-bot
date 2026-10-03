# Remote machines v1 acceptance

Date: 2026-10-03. Implementation and development-build evidence; external-terminal
lifecycle is verified, while terminal-window visual acceptance remains open.
No public release or signing/notarization acceptance is claimed. CI results belong
to the exact PR head and are separate from this local/native evidence.

The controller is the macOS Bot APP. Fedora runs its existing Codex 0.159.2 and
Caelis 0.66.0 with target-local accounts. Tests use disposable controller profiles
and private remote task workspaces. Global accounts, model defaults, Team settings
and user SSH configuration were not changed. Detached task owners remain available
for recovery. Credentials and native binding identities are omitted from this
record; raw local logs are not publication artifacts.

The first-version scope covers nodes newly connected with the current helper.
Compatibility with retained remote owners from earlier development versions is
explicitly excluded; reconnecting such an owner is not claimed to update it.

## Verified paths

| Path | Evidence and limits |
| --- | --- |
| Risk POC: full native runtime on Fedora | Both runtimes completed exact remote file readback. Two TUI observers per runtime disconnected while work continued. Codex retained one original turn. [Redacted POC](poc.json). |
| Feature service: actual SSH/native inference | `TestFedoraNativeWorkers` passed for Codex and Caelis. Separate machine profiles, node-local work model, target-owned workspace, exact 14-byte readback, two original TUI observers, controller reopen and unchanged native binding. Each original task then continued, interrupted and resumed to completion; the target still had one Worker. Removing the original owner is rejected. [Redacted lifecycle result](feature-e2e.json). |
| Native Bot → remote Worker → result | One explicit Fedora assignment through actual macOS Bot chat. One Worker completed, remote bytes and one-file inventory independently verified. Original task was subsequently pinned/locked to Dock without another Worker. [Redacted result](native-chat.json), [native chat](ui-native-result.png). |
| Daily-profile SSH fix and machine removal | The same Fedora profile failed with unquoted `UserKnownHostsFile` under macOS `Application Support`, then succeeded with the same ProxyJump and a quoted path. Native daily Bot reconnected that original profile and reached ready Caelis without changing resident runtime preferences, SSH config, account defaults or Team. Native isolated QA deleted a disposable offline profile, verified cancellation and persisted removal. Chinese/English modal and wide confirmation layouts were inspected. Task ownership removal guard still passes. |
| SSH authentication | Real OpenSSH against isolated loopback fixture: password, private key, encrypted-key passphrase, agent, configured-Host password, wrong-password and wrong-host-key rejection passed, using a known-hosts path containing spaces, quotes, a backslash and percent tokens. This is transport coverage, not another Fedora login. |
| Credential persistence | Native macOS Keychain canary save/update/read/delete passed; only a unique disposable item was created. Passwords remain absent from serialized machine readback/state. |
| Ownership and recovery contracts | Owning race tests passed for machines, remote owner and tasks. Replayed requests across a resident-runtime change retain their machine; missing original Codex endpoints fail closed; corrupt/null node model settings do not fall back silently. |
| Native settings | Final app built/launched with `script/build_and_run.sh`. Actual Fedora Codex and Caelis nodes are ready. Chinese/English wide and narrow settings were inspected; resizing preserves the same editor. Caelis is ready before entering Team configuration. Its actual advanced Team model dialog was inspected then cancelled. |
| Final model interaction | Bot, work and Team use a compact parameter picker and separate searchable model list. Native Bot model/Effort/Fast save and reopen passed. A newly connected Fedora node read Codex/Caelis configured defaults, saved node-local Effort/Fast, and reset to default. `TestFedoraModelSettings` passed with 8 Codex and 2 Caelis models. Fast is shown only when native capabilities expose it. |
| Final settings presentation | Seven settings categories were inspected in Chinese and English. AI models, Connections & accounts, and Remote machines now have separate first-level entries. The terminal preference is in General; optional Team opens separately from Connections & accounts; advanced and SSH disclosures do not expand together. Search-empty, SSH authentication form, offline, host-trust, Team draft and Escape/focus return states were checked. No account, permission or global Team reset was performed. |
| Chat submission handoff | Actual Codex chat in an isolated native Bot profile completed a text-only acceptance reply. The production native WebKit fixture passed delayed acceptance, rejection, unknown receipt and accepted/draft-read failure: one submission each, one user message, stable 69-pixel user bubble and zero empty-reply frames over 211–212 sampled frames per case. Accepted draft synchronization ran once; synchronization failure blocked duplicate sending. This failure coverage is synthetic, not another real model run. |
| Connection catalog overflow | Actual native Caelis account, API and external-Agent catalogs were inspected with long English descriptions inside the Chinese UI. Descriptions wrap inside their cards, with separate thin chevrons. English 860×640 and single-column 620×600 browser fixtures were inspected; these are responsive fixtures, not native minimum-size claims. |
| Production Ghostty observer | After the user requested a retry, the production observer completed two open, collapse/restore, close/reopen cycles with the same original task binding and no task mutation. The earlier timeout remains recorded. Terminal pixels could not be inspected because Computer Use denied access to Ghostty. |
| Constrained configuration states | The same components at 860×640 were inspected with visual fixtures: login guidance, missing runtime, password failure, private-key chooser, optional advanced failure and nested Team model dialog. Existing Fedora accounts were not logged out to synthesize these states. |
| Required checks | `make check`, `make smoke`, `make build`, owning race tests, SSH authentication and Keychain canary passed. Linux amd64/arm64 helpers cross-compiled. Only Fedora amd64 has live hardware evidence. |

## Visual evidence

| State | Screenshots |
| --- | --- |
| Saved offline machine removal | [Chinese delete confirmation](ui-delete-confirm-zh.png), [English compact confirmation](ui-delete-confirm-en.png) |
| Actual Chinese settings | [Wide inspector](ui-zh-wide.png), [narrow modal](ui-zh-narrow.png) |
| Actual English settings | [Wide inspector](ui-en-wide.png), [narrow modal](ui-en-narrow.png) |
| Actual target setup | [Fingerprint confirmation](ui-trust-en.png), [Caelis ready before Team](ui-caelis-ready-en.png), [optional Team model dialog](ui-caelis-team-dialog-en.png) |
| Login guidance fixture | [Wide](ui-fixture-login-en.png), [minimum](ui-fixture-login-min-en.png) |
| Minimum-window fixtures | [Missing runtime](ui-fixture-missing-min-zh.png), [password error](ui-fixture-password-error-min-en.png), [private key](ui-fixture-private-key-min-en.png) |
| Optional Team fixtures | [Advanced read failure remains ready](ui-fixture-advanced-error-min-zh.png), [Team table](ui-fixture-team-min-en.png), [nested model dialog](ui-fixture-team-dialog-min-en.png) |

Visual checks found and corrected a stretched radio input hiding model names,
the wrapped private-key chooser button, and connection actions/errors displaced
below the form. Primary actions now remain in a fixed footer, errors appear above
the connection fields, and background navigation is guarded while editing.

The final interaction pass produced 45 fresh native screenshots in private local
evidence. The following representative screenshots cover the final compact model
and optional Team design; the earlier screenshots above retain their original
live/fixture provenance rather than being presented as new captures.

| Final state | Screenshots |
| --- | --- |
| Saved model parameters | [Chinese compact picker](ui-final-model-zh.png), [English model list](ui-final-model-list-en.png) |
| Machine inspector and optional Team | [Wide machine inspector](ui-final-machine-wide-zh.png), [English Team dialog](ui-final-team-en.png) |
| Separated settings and wrapped catalogs | [Native AI models page](ui-separated-models-zh.png), [native external-Agent catalog](ui-connection-catalog-wrap-zh.png) |

The later chat/settings pass uses the seven-category navigation shown above.
Its [native send fixture summary](chat-handoff.json) contains only synthetic counts;
actual Codex inference and daily Caelis catalog inspection are separate native checks.

The final pass qualified the normal native window and wide inspector. The exact
860×640 states above remain fixture evidence, not a new native minimum-size claim.
Global Team saves, OAuth reauthentication and every optional ACP/provider editor
were not repeated during this final presentation pass.

## Resident reconnect and bounded history

An existing heavy Caelis conversation reproduced a reconnect response-header wait
of 40.526 seconds, beyond the Bot's former shared 20-second header deadline. The
ordinary initialize/state/callback endpoints remained reachable. The fixed stream
now has a separate cancellable header budget, and unchanged offline failures do
not repeatedly publish the same product-state transition.

Read-only measurements on the same existing history, with the server already warm:

| Recovery window | Bytes through sync | Time through sync |
| --- | ---: | ---: |
| 64 complete Turns | 18,648,487 | 5.246 s |
| 8 complete Turns | 2,439,857 | 0.715 s |
| Exact saved-cursor resume | 3,183 | 0.038 s |

The native adapter now requests eight complete recent Turns and uses the existing
chat “load earlier” interaction for finite older pages. Read-only native acceptance
with isolated projection writes recovered 150 display items and prepended another
72, preserving the original live state, cursor and newer items. Fixture coverage
includes both canonical replacement and exact-source pages, overlap, truncation,
invalid page boundaries, replacement during a read, and original command evidence
outside the display window. Existing saved projections remain intact on exact
resume. The window bounds display recovery; it does not truncate model context or
canonical history, and one unusually large Turn can still contain substantial data.

Completed tool receipts also occupied roughly 16 MB per list response. The Bot now
reads that full snapshot once on connection recovery to reconcile claimed calls,
then uses the runtime's pending-call wait instead of repeatedly polling the full
history. Idle read cancellation is not a connection failure. Fixtures retain the
original claimed receipt and process a subsequent pending call through the native
claim/result path.

These warm measurements do not establish a cold-start timing bound for Core's
reconnect preparation. No shared runtime restart, account reset or history deletion
was used to manufacture a faster result. The top-level change-connection button
was inspected in [Chinese](ui-switch-top-zh.png) and [English](ui-switch-top-en.png),
including opening and cancelling the existing selection dialog.

Required checks, owning race tests and real native Host/Guardian integration
passed after these changes, including the 90-second review timeout that never
dispatches an effect. The existing daily Bot profile resumed with unchanged
runtime settings and no new connection-error records over more than two minutes.
Bot-facing skill guidance needs no update: paging is display-only, and callback
transport keeps the existing claim, receipt and uncertain-effect workflow.

## Existing SSH configuration

The connection editor offers existing SSH config or a new connection before any
manual fields. Native Host discovery reads user/system config and bounded Include
files without rewriting them. Selecting an alias delegates effective user, port,
identities and the supported jump route to OpenSSH. The new connection form starts
with authentication, then address/user/port and conditional credential fields.

A fresh Fedora controller passed configured-Host discovery, native fingerprint
confirmation, authentication through the existing jump host, both runtime detection
and a ready Codex inspection after reopening; no model call or remote account/model
change was made. The native UI also completed search, selection, fingerprint and
Codex readiness. Native password, key, encrypted-key, agent, optional configured-Host
password and incorrect-password rejection passed against the isolated SSH fixture.
Inventory regressions cover Includes/cycles, quoting, duplicates/patterns, refresh,
missing files and read bounds; real OpenSSH projection preserves identity/jump settings
and ignores stale manual user/port/key values in the existing-config route.

Chinese/English native dialogs and wide inspectors, manual authentication fields,
selection and empty-search states were inspected. Wide inspector actions now stay at
the bottom with a scrollable body. Missing/unreadable config states were inspected
with browser fixtures and keep refresh/new-connection available. Representative
native views: [existing config](ui-ssh-config-zh.png),
[wide inspector](ui-ssh-config-wide-zh.png). Required checks, smoke and affected race
tests passed. Bot skill guidance is unchanged because this is user-facing connection
setup and introduces no Bot tool or task-routing semantics.

## External-terminal lifecycle and remaining visual gate

The first real Ghostty developer smoke reached its 90-second opening deadline
without a confirmed usable observer window. After the user explicitly requested
another attempt, the production `--remote-terminal-smoke` passed at 13:42 Asia/Shanghai:
two cycles of open, collapse/restore and close/reopen retained the original native
binding, with no task mutation. This is fresh production WindowManager evidence,
in addition to the native SSH PTY observers and lifecycle fixtures.

Computer Use still rejected access to Ghostty and macOS Terminal for safety
reasons. No alternate automation bypassed that denial. Terminal contents and a
fresh Dock screenshot therefore remain unqualified, despite the passed production
observer lifecycle. Full external-terminal visual E2E and release readiness must
not be claimed. See [reproduction and isolation requirements](../../development.md#remote-machine-acceptance).

## Worker defaults and retained task backends

The local and per-machine Codex/Caelis choice now selects only the default for
new tasks. The task ledger and remote routes freeze each task's actual backend
before dispatch. Old reads, continuations, stops, completion reports and terminal
targets use that binding across subsequent selection changes. Local inactive
adapters restore owned Workers without resuming a second resident Bot or rebinding
its tools. The Bot task-tool schemas and explicit-machine routing remain unchanged.
Tool consolidation remains a separate TODO in the implementation plan.

`TestFedoraDefaultSwitch` passed against one fresh configured Fedora SSH profile
with its existing ProxyJump. Two native tasks completed with exact file readback;
controller reopen, Codex to Caelis to Codex, old Codex continuation/interrupt and
old Caelis continuation retained both original terminal bindings. Local
`TestLocalNativeDefaultSwitch` also passed with explicit Codex `gpt-6-luna` and the
configured Caelis default, simultaneous owned tasks, exact bytes and old Caelis
continuation under the Codex default. This local proof uses an explicit available
model: the initial implicit Codex-default attempts reached native `failed` without
a result and are not counted as passed inference acceptance. User account and
global model settings were not changed.

Fixtures cover working, completed, approval-waiting, interrupted and unknown
tasks; durable migration, idempotent replay, both backend caches, original-owner
unavailability, persistence failure, current-resident admission and exactly-once
completion report delivery. Adapter wire fixtures confirm no inactive resident
resume/rebind. Reopen and unavailable-owner recovery are covered. The Fedora
acceptance also injected a failed native SSH read through a disposable unreachable
endpoint and separate control socket, then recovered through the unchanged
original profile. The Fedora network and user SSH config were not modified.

Native Chinese and English settings were inspected with independent Bot/Worker
choices, including the machine dialog's fixed actions and optional advanced
section. A 620 by 760 browser fixture verified the local choice wraps cleanly,
and its shared model drawer, Effort keyboard adjustment and save work. Example
views: [local Chinese](ui-worker-default-local-zh.png),
[remote English](ui-worker-default-remote-en.png),
[compact English fixture](ui-worker-default-compact-en.png).

Full `make check`, `make smoke`, affected ownership/adapter race tests and the
native Dev build passed for this slice. These are local development checks,
separate from exact-head CI and signed release acceptance.
