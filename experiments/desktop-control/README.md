# Desktop Control POC

This is an opt-in experiment, not a released feature. It uses the **real Cua
Driver 0.30.2** SDK and a disposable macOS native window. Default observation is
accessibility metadata and text; the tested sequence makes **zero screenshots**.
No browser profile, account, arbitrary app or system-menu action is exposed.
The current product adapter is separately implemented in `resources/computer-use`;
see the [integration record](../../docs/design/computer-use-integration.md).

The model-facing tools are `bot_desktop_observe {}` and
`bot_desktop_perform {observation, steps:[{op:"click",target}]}`. The current fixture
adapter admits only its filter checkbox. Each external mutation returns a fresh
observation and unexecuted steps. Character movement/pointing are not implemented.

## Reproduce the driver probe

From the repository root, with Node 24.21+:

```sh
source script/env.sh
npm ci --prefix experiments/desktop-control --ignore-scripts --no-audit --no-fund
./script/build_and_run.sh --cua-driver-preview
node experiments/desktop-control/smoke.mjs
```

The fixture is a real Cocoa application; Cua performs AX actions using native
element tokens, then verifies changed checkbox values and accessible task-count
text. Ten toggles restore the starting state. Evidence stays under ignored
`.cache/desktop-control/evidence.json`; do not commit desktop captures or content.
Run one live fixture probe at a time. Close the disposable window afterward.

Native AX access belongs to the launching host. A successful SDK probe under a
development terminal does not qualify the signed Bot app's TCC identity. Missing
permissions fail; neither the SDK adapter nor this probe changes permission grants.

## Bot and Caelis contract probe

With the fixture running and an external Caelis binary containing merged #82:

```sh
source script/env.sh
CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis \
CAELIS_BOT_CUA_NODE="$(command -v node)" \
CAELIS_BOT_CUA_HOST="$PWD/experiments/desktop-control/host.mjs" \
go test -race -count=1 -v -timeout 150s ./internal/backend/caelis \
  -run '^TestNativeHostIntegration$'
```

This test uses a real isolated Caelis Host and real Cua SDK/native fixture, but a
**synthetic model provider**. It proves the callback/structured-result path, not an
LLM's independent planning or a complete embodied product experience.

The Go host starts one SDK child over inherited pipes, passes no Bot/model
credentials, and closes it with the owner. It exposes two fixed resident-only tool
names. Worker tool/environment contracts remain unchanged. No listener or global
MCP registration is installed. Cancellation/timeouts close the channel and report
uncertainty, never restart or retry an input action automatically.

Native Bot opt-in uses `CAELIS_BOT_CUA_POC=1`, `CAELIS_BOT_CUA_NODE` and
`CAELIS_BOT_CUA_HOST` through `script/build_and_run.sh`. This must be a deliberate
development run with the fixture already open. The app's existing single-instance
policy applies within the independent `Caelis Bot Dev` identity. Select Caelis for
this host-tool probe: Codex's native capability tag suppresses all `bot_desktop_*`
tools even when experimental environment flags are set. Native ownership is not
overridden to repeat the historical Cua-in-Codex experiment.

The separate `CAELIS_BOT_DESKTOP_POC=1` switch enables the optional experimental
`bot_desktop_capture` supplement. It is not needed by the structured probe.

## Dependency and platform boundary

`package-lock.json` fixes package versions and integrity. These are separate from
the application's production dependencies and packaging.

| Package | Version | Declared license |
| --- | --- | --- |
| `@trycua/cua-driver` | 0.30.2 | MIT |
| `@trycua/cua-driver-darwin-arm64` | 0.30.2 | MIT AND MPL-2.0 |
| `@ubjs/core`, `@ubjs/node` | 0.31.0-3 | MPL-2.0 |

The dependency chain is **not purely MIT**. Before shipping, verify redistribution
notices/source obligations for the exact native artifacts and dependencies. No
license-risk-free or final distribution approval is claimed by this POC.
The SDK publishes Windows native packages, but this fixture and its coordinate
mapping are macOS-only evidence. Windows native behavior remains unverified.

References: [SDK](https://cua.ai/docs/reference/cua-driver/sdk-reference),
[hosting](https://cua.ai/docs/concepts/choose-a-cua-driver-integration),
[window SDK](https://cua.ai/docs/how-to-guides/driver/use-sdk-in-process).
