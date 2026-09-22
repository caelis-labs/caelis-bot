# Caelis Bot

## Product authority

- Read `docs/product.md` and `docs/architecture.md` before changing behavior.
- Caelis Bot is a restrained, agent-driven entry point for nontechnical users.
- Follow `docs/roadmap.md` for the product direction: a persistent personal assistant
  with contextual 3D expression and a basic Desktop/Dock/ActiveWindow model early.
  Use `docs/desktop-behavior.md` for idle, pointer/work interactions and independent props.
  Initial exploration targets developer daily use and distinctive real demos; broad
  user-retention proof and complex desktop semantics are not prerequisites. A model
  plus chat/status clips is an integration baseline, not the competitive endpoint.
- The product lives in the macOS menu bar with an independently visible, draggable,
  proportionally resizable desktop pet. Conversation and approval panels appear on demand.
- P1 replaced the titled foreground fixture; do not recreate a main dashboard.
  The optional chat/settings windows are contextual surfaces. Closing a panel or hiding
  the pet must not implicitly quit the app or cancel work.
- Bot has one long-lived product identity, no new-session/workspace navigation.
  User prompts and resident schedules activate work; internal worker threads stay hidden.
  Use `docs/bot-design.md` for surface ownership and wakeup semantics.
- Never expose Session/thread IDs, workspace trees, terminal panes, model controls,
  protocol details, or developer dashboards as default product navigation.
- Conversation, attachments, approvals, and contextual connection/plugin setup
  are the primary interaction surfaces. Do not recreate Caelis TUI or Caelis App.
- Backend capability is broader than visible UI. Current Caelis Bot Mode limits
  are implementation status, not the product's permanent capability ceiling.

## Architecture

- Ship a complete macOS release first. Preserve interfaces for a future Windows
  adapter, but do not implement it before macOS ships. Linux is outside the current plan.
- Codex App Server is the first backend; Caelis adapts to the same contract later.
- Discover the user's local Codex installation and connect through the standard
  App Server handshake. Do not bundle or silently install a Codex runtime.
- Codex uses auto_review by default. Auto-approval is scoped to explicitly listed
  built-in Bot MCP tools; keep native deferred discovery and other tool policies intact.
- Backend adapters own protocol projection. Native Go services own OS/process
  lifecycle and bytes; the renderer owns presentation and temporary interaction.
- Preserve native request targets, run/item identities, approval choices, and
  uncertainty. Never infer authority from generated prose or character animation.
- Character identity and assets are independent of conversation and task identity.
- Future embodied behavior separates native desktop context, local intent scheduling,
  native placement and renderer poses; see the planned boundaries in `docs/architecture.md`.
  The first local context/behavior/prop slice is implemented; the general intent API
  remains planned. Preserve authoritative backend facts,
  explicit window associations and user input priority; do not run an idle LLM loop.
- No private imports from the sibling Caelis repositories. `GOWORK=off` is required.
- Keep schemas, dependencies and the tested Codex baseline explicitly pinned for
  reproducible development. User CLI releases are not an allowlist: use the standard
  App Server handshake and consumed protocol semantics to decide compatibility.
- Do not turn a draft capability into an advertised feature before its full path works.

## Current preparation scope

- P1's native shell is implemented; keep remaining hardware/release gates explicit.
- P2's real Codex workflow is connected: conversation, tools, files, approvals,
  interrupt and recovery. Keep live evidence separate from fixture coverage.
- Follow the latest checkpoint in `docs/implementation-plan.md` and `docs/handoff.md`;
  do not restart P1/P2 from their historical plan. Keep `docs/preparation-status.md` honest
  about implemented behavior versus plans and previous verification.
- Character authoring belongs in the private `caelis-labs/caelis-bot-assets` repository.
  This public repository contains only finished resources, the renderer and public contracts.
  Read `docs/character-assets.md` before changing asset delivery. Never introduce Blender,
  authoring sources, private credentials or archived models as public build dependencies.
  Preserve code/character license boundaries and provenance. New costumes and characters
  use versioned finished variants; they do not change the Bot's conversation identity.
- Early pre-release uses ad-hoc signing and documented per-app trust steps.
  Developer ID and notarization are deferred by the user, not gates for this preview.
- Preserve unrelated changes. Commit and push require their own user authorization.

## Verification

- Use `make check`, `make smoke`, and `make build` for the affected preparation paths.
- Native launch goes through `script/build_and_run.sh`; inspect the actual window
  before claiming visual behavior. Add contract/lifecycle tests with real behavior.
- Never log credentials or full private conversation content in smoke output.
