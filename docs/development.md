# Development and verification

Read [product](product.md) and [architecture](architecture.md) before changing
behavior. [Release and installation](release.md) covers public packages and
publication; [content packs](content-packs.md) covers finished asset delivery.

## Toolchain and source boundaries

- `.node-version` is the canonical Node pin. Go and Wails are pinned in `go.mod`;
  the Windows Wails CLI uses that library version. Keep npm lockfiles committed.
- `internal/backend/codex/schema/manifest.json` pins schema generation, smoke
  reports and the adapter's tested Codex baseline. This is a regression baseline,
  not a user CLI allowlist; compatibility uses the standard App Server handshake
  and the protocol semantics Bot consumes.
- Use `GOWORK=off`. Never import private sibling Caelis source. Runtime integration
  uses public contracts and explicitly pinned released binaries and hashes.
- Public builds consume finished character resources. Modeling sources and
  authoring tools live under `art/` in this checkout. Blender is optional for app builds.
- `script/env.sh` selects the pinned Node major, local caches, `GOWORK=off` and the
  macOS deployment target from `resources/macos/Info.plist` without modifying the
  user's shell. Ordinary builds do not regenerate protocol schemas.

## Standard commands

From the public repository on macOS:

```sh
make setup
make doctor
make check
make smoke
make build
```

`make setup` installs locked npm and Go dependencies. First builds also fetch
pinned Sparkle and Caelis Runtime artifacts; their versions and hashes are in source.

| Command | Contract |
| --- | --- |
| `make check` | Generated contracts, schema hashes, public-tree/asset boundaries, frontend/i18n, Node contracts and Go tests; no native window launch or permission request |
| `make smoke` | Installed Codex handshake and asset checks; no conversation or model call |
| `make build` | Native development app, ad-hoc signed by default |
| `make package` | Local app plus verified read-only DMG/checksum; pinned dmgbuild tools use `.cache/dmg-tools` |
| `make check-portability` | CGO-free shared-core tests and Windows amd64/Linux arm64 cross-compilation |
| `make schema` | Explicit Codex schema regeneration from the pinned baseline |

Use `make check`, `make smoke` and `make build` for affected preparation paths.
Check protocol generation only when changing its inputs; commit the manifest and
consumed generated contracts together. CI routing and exact-source release gates
are described in [branch policy](release.md#required-checks-and-branch-policy).

### Worker completion delivery

The opt-in product acceptance uses the actual Bot `serve` entry and an installed
Codex App Server with disposable stores and local synthetic model responses.
It observes the original Worker terminal turn, task execution key, report ID,
resident input and durable delivery receipt in both healthy and faulted event
storage. It checks the real event write on expiry, restores the obstructed
document, and verifies that the original resident receives both the Worker
report and the recovered scheduled wake exactly once.

```sh
source script/env.sh
go build -tags production -o .cache/worker-report-bot .
CAELIS_BOT_TEST_PRODUCT="$PWD/.cache/worker-report-bot" \
  CAELIS_BOT_TEST_CODEX="$(command -v codex)" \
  node script/worker-report-resident.mjs
```

The script writes a summary containing only synthetic identities under
`.cache/worker-report-resident`.

## Native macOS review

Launch through `script/build_and_run.sh`. Use synthetic conversations and a
separate profile; an explicit development data directory also namespaces the
native instance lock so review can coexist with the daily Dev app.

```sh
mkdir -p "$PWD/.cache/native-review"
CAELIS_BOT_DATA_DIR="$PWD/.cache/native-review" \
  bash script/build_and_run.sh --verify
```

The Bot profile does not isolate every external Runtime store. For a fully
isolated integration test, also use separate HOME/CODEX_HOME or the Runtime's
explicit Store configuration. Never point destructive fixtures at daily data.

Default local/PR builds use `Caelis Bot Dev.app`, `dev.caelis.bot.dev` and
`~/Library/Application Support/Caelis Bot Dev`. Release builds use
`Caelis Bot.app`, `dev.caelis.bot` and `~/Library/Application Support/Caelis Bot`.
Dev builds do not use the public updater; data and system permissions do not
migrate automatically between those identities. Release instances keep one
product lock. Closing a panel or hiding the pet does not quit the application.

Desktop assistant replies can include A2UI Express blocks for comparisons, grouped
records, editable local drafts and questionnaires. The chat panel renders complete
blocks offline. Copy and history retain the original Markdown. A root `Form` offers
an explicit submit button for the visible fields, using ordinary user-message
admission and receipts while preserving the composer. Hiding the chat panel
releases preview and local-draft state. Questionnaires retain edits while that
panel is mounted. A matched receipt restores submission status from history.
The pet bubble, Telegram and Weixin show the prose. A completed block-only reply
points the reader to desktop chat. Unsupported WebViews show the exact block source.

The chat catalog in `frontend/src/chat-a2ui-catalog.ts` drives compilation,
rendering and the bundled bot-core reference. After updating it, regenerate the
reference with `node script/generate-chat-a2ui-reference.mjs` using the pinned
Node toolchain. `make check` verifies the generated file and the Markdown tests.
For native panel coverage, including hiding and reopening with history retained,
run the chat preview with `BOT_PREVIEW_FIXTURE=a2ui`
and `BOT_PREVIEW_A2UI_CASE=comparison`, `collection`, `draft`, `exam` or `exam-error`.

Inspect the actual desktop/chat/settings windows before claiming visual behavior.
`bash script/run-ui-review.sh --verify` supplies an isolated review profile.
The native launch script also supports `--bubble-preview`, `--chat-preview` and
`--input-menu-preview` for focused production-component fixtures. Synthetic
fixtures prove their controlled behavior, not live Runtime or OS input acceptance.
Check real focus, hit regions, display scaling and permissions when affected.

### Development signing and permissions

Ad-hoc is the default and remains the CI mode. For stable local permission
identity, explicitly choose an existing **Apple Development** certificate:

1. Create/manage the certificate in Xcode → Settings → Apple Accounts. The user
   handles account login and system Keychain confirmation.
2. Run `security find-identity -v -p codesigning`; put the selected 40-character
   fingerprint on one line in the Git-ignored `.development-signing-identity`.
3. Run `bash script/build_and_run.sh --verify`. The build checks the certificate
   type and private key, signs nested components, and verifies Apple identity.
   An invalid selection fails instead of falling back to another identity.

A linked worktree inherits the main checkout's selection unless it has its own.
`BOT_DEVELOPMENT_IDENTITY=<fingerprint>` overrides one invocation; `-` explicitly
selects ad-hoc. CI ignores these local selections. Keep private keys in Keychain;
do not export them, change Keychain ACLs or use release credentials for development.
Apple Development signing is not Developer ID distribution or notarization.

Changing from ad-hoc requires fresh system permission approval. Stable certificate,
bundle ID and path can reduce repeated prompts but cannot override macOS policy.
If a running process has stale permission state, use
`bash script/build_and_run.sh --restart` to quit normally and reopen the same
binary without rebuilding. `--recall` only recalls it. Do not reset system
permissions automatically. Login-item registration targets the installed bundle;
after moving it, disable the old item and explicitly enable the new location.

For an already Developer ID-signed app, use
`BOT_SIGNING_TEAM_ID=TEAMID bash script/build_and_run.sh --verify-signed`.
This preserves the signature and checks startup; it does not establish notarization.

## Native Windows development

Use PowerShell 7 and the repository's Node/Go pins on Windows:

```powershell
./script/check-windows.ps1
./script/smoke-windows.ps1 -CodexBinary 'C:/absolute/path/to/codex.exe'
./script/build-windows.ps1
./script/build_and_run-windows.ps1 -SkipBuild -DataDirectory 'C:/private/bot-review'
```

For installer changes, build into a fresh non-development output directory and
package the complete bundle:

```powershell
./script/build-windows.ps1 -Channel preview -PreviewRevision 2 -OutputDirectory dist/windows-preview-2
./script/package-nsis-windows.ps1 -SkipBuild -Channel preview -ApplicationDirectory dist/windows-preview-2
./script/package-zip-windows.ps1 -SkipBuild -Channel preview -ApplicationDirectory dist/windows-preview-2
```

Without `BOT_RELEASE_TAG`, candidates carry `-local`, use Dev identity/data and
cannot use public updates. Tagged release builds require clean exact source.
Test custom paths, upgrade and uninstall when changing installers. NSIS Setup
and ZIP are the distribution paths; MSIX is a development fixture only.
Windows native checks and cross-builds do not prove interactive GUI behavior.
See [Windows](release.md#windows) for channel, registration and shared-Host rules.

## Headless development

Linux headless uses the shared application and Web UI; no display, Wails or GTK
is needed. Run with an ordinary user, bubblewrap and working unprivileged user
namespaces. The bundled Runtime must retain its sandbox boundary.

```sh
make setup
make build-headless
# On the Linux target, retain the complete architecture directory:
./dist/headless/linux-arm64/caelisbot serve --data-dir /absolute/private/profile
```

The build defaults to the host architecture; set `BOT_HEADLESS_ARCH=amd64` or
`arm64` explicitly for a cross-build. Keep Helpers, Resources and licenses with
the executable. The listener defaults to `127.0.0.1:9317`; the profile must be
private and must not be shared with another running desktop/headless owner.
Service installation, origins, pairing and update ownership are in
[Linux headless](release.md#linux-headless).

`make build-headless-image` builds `caelisbot:local`; override the tag with
`BOT_HEADLESS_IMAGE`. It passes `NODE_VERSION` from `.node-version` and
`GO_VERSION` from `go.mod`. The Dockerfile has no toolchain defaults.
This is a development image, not a published/accepted Docker distribution.
Docker's outer sandbox may prevent the Runtime sandbox from starting; do not
use privileged mode, unconfined system paths or Runtime sandbox bypasses.
Image updates belong to the operator, not the managed Linux updater.

## Verification boundaries

| Change | Evidence to collect |
| --- | --- |
| Shared behavior, recovery or persistence | Targeted failure/recovery regression, affected Go/race tests and full check |
| Runtime contract or configuration | Pinned schema/hash, adapter lifecycle tests and isolated external Runtime |
| UI, character or permissions | Native launch and actual window/input observation on the affected OS |
| Packaging | Complete local package, component verification and affected install/update/uninstall lifecycle |
| Public distribution | Exact-source CI and platform artifact/security/publication checks in the release guide |

`make smoke-adapter` uses an installed Codex; `make smoke-workflow` makes real
model/tool calls. Inspect each smoke's requirements before running it.
`CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis make smoke-caelis` runs an
external Caelis Host with temporary stores and a synthetic provider.
Set `CAELIS_BOT_TEST_PREVIOUS_BINARY` to the callback-capable v0.85.0 release
to also verify that upgrading the same Store preserves the resident Session and
settles the original pending callback receipt.
For a worktree under the platform temporary directory, set
`CAELIS_BOT_TEST_EFFECT_DIRECTORY` to a disposable writable directory outside
that boundary so the Guardian file-write cases exercise native approval.
`make smoke-caelis-live` is separately opt-in and billable; it requires an
isolated configured `CAELIS_BOT_LIVE_STORE` and `CAELIS_BOT_LIVE_MODEL`.
A skipped external-binary test is not evidence of native integration.

Tests, fixtures, compilation, live model work, native GUI observation and published
artifact acceptance establish different facts. Cross-platform manual hardware
acceptance is not a publication prerequisite. Platform requirements and mandatory
artifact checks live in [release](release.md); compilation does not establish
minimum-OS, permission, display/DPI or real-account acceptance.

For recovery changes, verify bounded retries, preserved drafts/original receipts,
no duplicate effects, no technical warning flashes and no credential leakage.
Keep credentials and full private conversation content out of smoke output.
For every feature/behavior change, inspect `internal/botskills/skills/bot-core/`:
update English Bot-facing guidance if capabilities, tools or workflows changed;
otherwise record why no skill update is needed. Keep documentation factual and
current; source tests and contracts carry detailed regression cases.

### Private Caelis Host fixtures

Run the focused macOS lifecycle checks with a released CLI that includes Desktop World.

```sh
CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis GOWORK=off \
  go test ./internal/backend/caelis -count=1 -v -timeout 90s \
  -run '^(TestPrivateHostReadinessFailureCleanup|TestNativePrivateHostLifecycle)$'
```

The checks use an isolated Store, a synthetic local provider and native Host and
DTW processes. They report scoped PIDs, cleanup elapsed time and the size of
regular files sampled in the private fixture. The reconnect case verifies the
original Session on a new managed Host. The readiness case uses a subprocess
that stays alive without publishing discovery.

Set `CAELIS_BOT_TEST_EVIDENCE_DIR` to a private directory on the same filesystem
to retain a failed fixture after verified process exit. Default cleanup removes
the fixture. If teardown cannot confirm exit, the test reports the cleanup
error and leaves the original path available for diagnosis.
