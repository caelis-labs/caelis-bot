# Codex device login

App and Web share `settings/runtime/CodexLogin.tsx`. ChatGPT device login is the
primary action; API Key is one secondary form. Normal sign-in has no program
path, install/uninstall guide, advanced dialog or re-detection button. Missing
installation and incompatible-runtime repair remain separate preparation paths.
First-run navigation still requires both setup and the introduction to finish.

## Native source and support boundary

`internal/backend/codex/setup_device.go` uses the existing setup-only standard
App Server client, not another Runtime, CLI login subprocess or OAuth owner:

1. Read `account/read` with `refreshToken:false`. Existing authentication is reused;
   neither ChatGPT sign-in nor API Key silently replaces it or invokes logout.
2. On an explicit click, call `account/login/start` with
   `type:chatgptDeviceCode`. Accept only the expected response type, a native
   login ID, a bounded user-code string and the official HTTPS verification URL.
3. Display only the user code and verification URL. Codex polls the issuer, saves
   credentials and refreshes tokens. Bot never handles OAuth/refresh tokens or
   the native device-auth secret.
4. Match `account/login/completed` to the original native login ID. Observe account
   readiness and model choices; only the ordinary Next action activates the model.
5. Cancel through `account/login/cancel` with the original native login ID.

The [official App Server authentication protocol](https://learn.chatgpt.com/docs/app-server#3b-log-in-with-chatgpt-device-code-flow)
documents this flow. [Authentication guidance](https://learn.chatgpt.com/docs/auth#login-on-headless-devices)
requires device login to be enabled for the user's account/workspace; device
login is suitable for headless machines. No browser callback on the service
host is required. This integration intentionally does not copy auth caches or
implement externally managed token authentication.

Verified on 2026-10-20:

- Repository schema baseline: `0.153.4`, including `LoginAccountParams.json` and
  `LoginAccountResponse.json`. Schema/dependency pins were not changed.
- Installed macOS CLI `0.162.1`: generated its actual schema in an isolated
  directory, then performed an actual empty-profile device start and cancellation.
- Installed Rocky Linux arm64 CLI `0.153.4`: actual empty-profile device start and
  cancellation. Both returned the expected type/official URL and no auth file was
  created. These prove issuance/cancellation, not completed account authorization.
- Read-only local upstream source at `7c073196a1673a0b248b2e5edfb7548d2c2290ac`:
  `codex-rs/app-server/src/request_processors/account_processor.rs`,
  `app-server-protocol/src/protocol/v2/account.rs`,
  `login/src/device_code_auth.rs`, and `cli/src/login.rs`.

Version numbers describe evidence, not an allowlist. A successful standard
handshake is insufficient to advertise device login. The explicitly requested
native operation and response contract decide support. Method/variant rejection
(`-32601`/`-32602`) gives an update instruction; known other native rejection gives
a concise network/account-permission retry message. There is no silent API Key
or browser-flow fallback. Unknown transport outcomes remain unknown until
cancellation/reconciliation; they never authorize an automatic second start.

## Lifetime and security

- `Setup` owns one attempt under its existing operation lock. Repeated clicks and
  tabs sharing the same authenticated browser session read the same attempt.
  Unrelated completion notifications cannot adopt it.
- The ingress injects `api.WithSetupAccess` from the authenticated cookie's server
  identity. Request JSON cannot select an owner. Another paired browser sees only
  an other-session status and cannot read the code, cancel or replace the attempt.
  Native windows share the local native surface identity.
- The product attempt ID fences cancellation from stale tabs; the native login ID
  remains private. `InspectSetup` returns the live challenge only to its owner.
  No SSE/composer event carries it. HTTP remains authenticated, no-store, and
  subject to Host/Origin/CSRF checks, including loopback sessions.
- `SetupState.Login` is stripped before persisting Web operation receipts. A
  repeated operation ID returns its original receipt with no expired code; the
  UI then safely reads the live setup state. API key request bodies are not stored.
- A local 15-minute deadline bounds code display and cancellation. This is Bot's
  conservative lifetime, based on the inspected native poll bound; the RPC does
  not publish an issuer expiration field. Completion/cancellation clears code,
  URL and expiry from the in-memory view. No code is saved to Bot configuration,
  diagnostics or operation history.
- A browser disconnect, Back, or closed settings window does not stop authentication
  or any Worker. The setup client and native poll remain server-owned. On a lost
  start response without a native login ID, explicit Cancel reaps only the owned
  setup-only App Server process before a later user-requested retry. No task owner
  or shared daemon is stopped. Shutdown/expiry likewise clean only this owner.
- Process restart discards ephemeral codes; a fresh read reuses any authentication
  Codex actually completed. It never automatically starts login. No third-party
  Codex implementation or production account configuration was modified.

## Verification and integration seam

Go native-wire fixtures cover duplicate starts, owner isolation, stale cancel,
matching/early completion, failure, expiration, existing auth, unsupported/malformed
responses and lost starts. Web fixtures check authenticated owner propagation and
that receipt replay/disk never exposes a code. `script/codex-login-browser.cjs`
checks the mounted production component, including lost-response readback and
completion to model selection. Run its isolated server with
`node script/codex-login-preview.mjs`; it uses no account or real mutation authority.

Shared files: `RuntimeSettings.tsx` (only the Codex ceremony and now-unreachable
old key dialog), `CodexRuntimePanel.tsx`, `ConnectionsPage.tsx` (action fields),
generated `backend/contract.ts`, and bilingual `runtime.json`. The new CSS is
scoped to the shared Codex ceremony. Web navigation, `SurfaceBoundary`, chat
feedback mounting and `chat.json` remain untouched in this change. Their overall
alignment still awaits the separate shared UI worker.

Handoff for that worker: `Panel.tsx` still has the existing connection-recovery
`Login` / `CancelLogin` actions, backed by `codex.Session` browser-callback login.
They are outside the setup ceremony and were not rewritten in this patch.
The Web allowlist does not expose those old native actions. When aligning the
chat recovery bubble, direct the user to Runtime connection management and the
shared device ceremony rather than adding another login owner or broadly
allowlisting `Login`. Preserve any already-pending legacy native cancellation.

Actual visual acceptance used the isolated native Onboarding App and the Rocky
Web service at matching 640-pixel width, with a 390-pixel Web check. Both issued
real codes and cancelled them. Web refresh and a second tab read the same attempt
with exactly one start request. First-run direct settings navigation stayed in
setup. Screenshot codes were cancelled before the evidence was archived.

These are user-only setup interactions, not new Bot tools or runtime work
semantics. Existing bot-core surface/host guidance still applies; no Bot skill
update is required. Docker bubblewrap, actual WSL/Tailscale deployment, completed
real-account sign-in and full Worker workflows are not qualified by these checks.
