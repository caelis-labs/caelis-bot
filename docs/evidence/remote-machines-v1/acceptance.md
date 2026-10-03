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
| SSH authentication | Real OpenSSH against isolated loopback fixture: password, private key, encrypted-key passphrase, agent and wrong-password rejection passed. This is transport coverage, not another Fedora login. |
| Credential persistence | Native macOS Keychain canary save/update/read/delete passed; only a unique disposable item was created. Passwords remain absent from serialized machine readback/state. |
| Ownership and recovery contracts | Owning race tests passed for machines, remote owner and tasks. Replayed requests across a resident-runtime change retain their machine; missing original Codex endpoints fail closed; corrupt/null node model settings do not fall back silently. |
| Native settings | Final app built/launched with `script/build_and_run.sh`. Actual Fedora Codex and Caelis nodes are ready. Chinese/English wide and narrow settings were inspected; resizing preserves the same editor. Caelis is ready before entering Team configuration. Its actual advanced Team model dialog was inspected then cancelled. |
| Final model interaction | Bot, work and Team use a compact parameter picker and separate searchable model list. Native Bot model/Effort/Fast save and reopen passed. A newly connected Fedora node read Codex/Caelis configured defaults, saved node-local Effort/Fast, and reset to default. `TestFedoraModelSettings` passed with 8 Codex and 2 Caelis models. Fast is shown only when native capabilities expose it. |
| Final settings presentation | Five settings categories were inspected in Chinese and English. The terminal preference is in General; optional Team opens separately; advanced and SSH disclosures do not expand together. Search-empty, SSH authentication form, offline, host-trust, Team draft and Escape/focus return states were checked. No account, permission or global Team reset was performed. |
| Production Ghostty observer | After the user requested a retry, the production observer completed two open, collapse/restore, close/reopen cycles with the same original task binding and no task mutation. The earlier timeout remains recorded. Terminal pixels could not be inspected because Computer Use denied access to Ghostty. |
| Constrained configuration states | The same components at 860×640 were inspected with visual fixtures: login guidance, missing runtime, password failure, private-key chooser, optional advanced failure and nested Team model dialog. Existing Fedora accounts were not logged out to synthesize these states. |
| Required checks | `make check`, `make smoke`, `make build`, owning race tests, SSH authentication and Keychain canary passed. Linux amd64/arm64 helpers cross-compiled. Only Fedora amd64 has live hardware evidence. |

## Visual evidence

| State | Screenshots |
| --- | --- |
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

The final pass qualified the normal native window and wide inspector. The exact
860×640 states above remain fixture evidence, not a new native minimum-size claim.
Global Team saves, OAuth reauthentication and every optional ACP/provider editor
were not repeated during this final presentation pass.

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
