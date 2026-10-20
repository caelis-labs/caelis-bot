# Shared Codex installation (App and Web)

Codex remains an explicitly installed external runtime. Opening setup, refreshing,
reconnecting or restarting Bot does not install it. The missing-program primary
action now uses the same installation service and shared UI on macOS and Linux
(amd64/arm64, including a Linux WSL host). Installation happens on the Bot server,
not the browser machine. Native Windows installation is outside this change.

## Source and authority

The original Web adaptation replaced installation with instructions in
`RuntimeSettings` and `CodexRuntimePanel`. `runtimeinstall.Codex` also rejected
all non-macOS hosts; only Caelis emitted measured download progress. These were
Bot limitations, not a limitation of the current official Codex installer.

The existing [official installer](https://learn.chatgpt.com/docs/codex/cli) is
still the only installation source: `https://chatgpt.com/codex/install.sh` with
HTTPS redirects restricted to that host and `releases.openai.com`. Its platform
selection, release SHA-256 verification, install lock and staged atomic package/link
publication remain authoritative. Bot does not modify or vendor third-party code.
The reviewed served script SHA-256 on 2026-10-20 was
`150e3cf675682efeaac115aa3747add3f27887896d04ce6d0b56478d8b428bf6`;
this is evidence, not a permanent script pin or a signature claim.

`CODEX_INSTALL_DIR` is fixed to the server user's `~/.local/bin`; caller-supplied
installer overrides are removed. The package home is the server's configured
`CODEX_HOME` (default `~/.codex`), following the official
[installer environment contract](https://learn.chatgpt.com/docs/config-file/environment-variables).
A redirected bin directory escaping the user's home is rejected. Script download
and temporary children are scoped to a disposable installer directory. Cancellation
kills only that installer's dedicated process group on macOS/Linux. Existing CLI
discovery still wins; an install after another client already installed Codex is a
readback, and an explicitly configured external program is never updated in place.
Account files, history and user configuration are not removed or logged.

## Lifecycle and presentation

- `ApplySetup(install/update)` and `SetupInstallProgress` remain the typed transport
  contract; no shell/RPC endpoint or additional Web allowlist capability is exposed.
  Existing Host/Origin/CSRF, local pairing boundary and operation receipts apply.
- `codexInstallation` holds one host-owned operation. Admission returns promptly;
  other tabs adopt its ID. Disconnecting a renderer does not cancel installation.
  Only an explicit cancel or application shutdown cancels the owned process.
- Optional progress fields (`runtime`, `id`, `action`, `state`, `error`) extend the
  existing DTO. Preparation/inspection can read `installing` without waiting for
  the install lock. Both App/Web use `CodexInstallation` for progress and retry.
- Only known official stdout stage markers are projected; raw stdout/stderr,
  paths and credentials never enter the UI. Script download totals are unavailable,
  so the bar is indeterminate, with no invented percentage. Actual stages are
  checking, downloading, installing and installed-program verification.
- Successful detection leads to the existing ChatGPT device-auth page. Installation
  does not log in, log out, start a model task or auto-select a model. Existing login
  may be reused. API Key remains a secondary option.
- Known failure/cancellation has a local retry. A durable `codex-installation.json`
  contains only operation metadata. After an unclean server interruption, its
  original ID is retained as `unknown`; reads do not replay it and a new install is
  blocked pending operator reconciliation. The UI offers re-detection, not a new
  write. Manual repair of an unconfirmed crashed installer remains a boundary.
- Normal first-run installation has no path/advanced dialog. Runtime management,
  an existing explicit CLI override or a real compatibility problem retains repair
  access. First-run route guards and all shared navigation/feedback rules remain.

No Bot skill update is required: this changes explicit user setup and host
installation lifecycle, not Bot-visible tools, scheduling, Worker ownership or
assistant workflows. The shared seams are setup DTO/generation, `app.setup` plus a
shutdown cancellation hook, and the shared runtime settings components; no #204
worker lifecycle or model/business logic was replaced.

## Acceptance and evidence boundaries

- Real Rocky Linux arm64 build with the repository-required Go toolchain in a new
  temporary directory; no global toolchain/network changes.
- A process-local unreachable HTTPS proxy produced a real download failure. Removing
  that test-only proxy and clicking Retry installed official **Codex 0.163.0** at
  `CODEX_HOME/packages/standalone/releases/0.163.0-aarch64-unknown-linux-musl/bin/codex`.
  Binary SHA-256: `8a96f66d059d653e68bfd5f3c79337e6f1d7d170d80bf705eb7d0249b34c0ece`.
- The real service reported checking → downloading → installing → verifying →
  complete, then setup `login`. An independent authenticated client submitted a
  duplicate while running and observed the same operation ID. The test profile had
  no `auth.json`; no account authorization or model task occurred.
- Go lifecycle/race tests cover concurrent requests, disconnected callers, explicit
  retry, cancellation, preserved unknown IDs/no replay, destination containment and
  existing external CLI preservation. Shared Web/App panel regression covers the
  installation button. Existing Web authentication/receipt tests remain in the gate.
- Real browser screenshots cover missing, failure and installed-login states;
  mounted shared-component fixtures additionally cover remount/recovery. Actual
  macOS WebKit windows render the same missing/progress/failure components at a
  comparable viewport. A separate real Wails Onboarding App with an empty profile
  was also inspected on its normal missing-Codex route; its install button was not
  clicked. Native fixture screenshots are not evidence of a real Mac install.
- `make check`, `make smoke`, isolated `make build`, affected Go race tests and real
  Linux tests passed. Sandboxed socket-test failures were rerun with local fixture
  socket permission. The repository has no `make commit-check` target.

Evidence is retained under `output/playwright/codex-install/` and copied to the
user's Notebook evidence directory at handoff. Docker sandbox compatibility and
native WSL hardware remain untested; this acceptance uses Rocky, not Docker.
The 19317 experience is updated with the new Bot code while Codex remains absent;
its existing login/history are retained so the user can trigger installation.
