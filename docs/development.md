# Development and verification

Start with the [current status](status.md) and [documentation index](README.md).
macOS and Windows have independent publication paths. Windows Setup/ZIP packages
are available; the [adaptation ledger](windows-adaptation-issues.md) retains dated
GUI evidence and open gaps. A published package does not qualify every interaction.

On Windows use `script/check-windows.ps1`, `script/smoke-windows.ps1` and
`script/build-windows.ps1`; they use the same locked toolchain and retain native
platform checks. Launch through `script/build_and_run-windows.ps1` with an isolated
`-DataDirectory` for review. Setup/ZIP, release workflows and key reuse are in
[Windows releases](windows-release.md). Mac `make check/smoke/build` remain required
on a Mac; Darwin CGO-free cross-compilation does not replace AppKit acceptance.

Build from this public repository with pinned `toolchain.json`, `go.mod`, npm lockfiles and protocol manifests.
No private asset checkout or sibling Core imports. `script/env.sh` selects Node 24, local caches, GOWORK=off
and the macOS deployment target without changing the user's shell. Ordinary builds do not regenerate schemas.

```sh
make setup
make doctor
make check
make smoke
make build
```

`make check` covers generated contracts, protocol hashes, public-tree/asset boundaries, frontend/i18n,
focused Node contracts and Go tests. It does not launch desktop fixtures or request desktop permissions.
`make check-portability` runs CGO-free shared-core tests and Windows amd64/Linux arm64
cross-compilation separately; CI runs it weekly and on demand. `make smoke` only handshakes the installed
Codex and checks assets; it does not create a conversation or call a model. `make build` creates an ad-hoc Dev app.
`make package` adds a verified read-only DMG/checksum, using pinned Python dmgbuild in `.cache/dmg-tools`.

Codex reconnect fixtures exercise more than 64 ordered events, a stalled session
consumer with concurrent RPC responses, approval/completion delivery, original
receipt reconciliation, private-owner endpoint pinning, bounded retries and
shutdown cancellation. The transport queue and handshake buffer each cap at
512 events / 16 MiB; saturation reports `event_overflow` and leaves unresolved
work unknown for original-ID reconciliation. Diagnostic status exposes queue
depth/high-water, four event-kind counters, reconnect attempts and both
`transportGeneration` (process-wide) and `sessionEpoch` (per owner). These
fixtures do not measure the live Bot's producer burst, FD pressure, or a native
slow persistence path. A passing fixture or `make smoke` is not evidence that
the daily Bot instance was replaced or recovered.
First builds fetch pinned Sparkle and Desktop World artifacts; versions/hashes remain source-controlled.

Shared Runtime startup recovery (#152) has Linux/macOS Unix socket and local
HTTP/CLI contracts in `shared_recovery_test.go`, plus coordinator and CLI tests
in `internal/sharedruntime` and `internal/caelisruntime`. Run these with the
adapter race tests. They cover original directory/receipt identity, shared
startup coalescing and cooldown, first-observer cancellation, live/unknown
service guards, selected Caelis restart after discovery removal, and observer
detach. Fake CLI starts and synthetic
protocol peers are not live Codex daemon/Caelis Host acceptance. Native macOS
0.162.1 daemon recovery, real in-flight Worker outcomes and Ghostty attach/detach
remain separate acceptance steps; Linux fixtures cannot establish those results.

Desktop World is pinned to [v0.1.0](https://github.com/caelis-labs/desktop-world/releases/tag/v0.1.0).
`resources/desktop-world/release.json` separately pins the slim Plugin archive,
standalone provenance, slim runtime, worker, BUILD and license inventory.
Staging verifies the archive before extracting only ordinary relative files and
directories, then checks every file against SHA256SUMS and the pinned native
binary hashes. The archive is never executed. Validation failure retains the
previous package. No DTW build toolchain or global installation is needed.

On macOS, staging verifies Developer ID signatures and the pinned team on the
slim runtime and worker before Bot signs its own nested payload. Upstream
notarization does not qualify the resulting Bot application: public Bot releases
still require their own immutable signing, notarization, stapling and Gatekeeper
gates. Windows uses the same archive verifier, remains unsigned upstream, and
requires the x64 VC runtime. Upstream Windows DPI, modal-provider and uncertain
focus limits are tracked in [DTW #36](https://github.com/caelis-labs/desktop-world/issues/36).
Historical evidence remains tied to its original version; it does not qualify
the v0.1.0 artifact.


## Native development

UI review uses `bash script/run-ui-review.sh` with an isolated cached profile.
Pass `--verify` to rebuild before launching. See [interface design](ui-design.md)
for the visual reference, surface map and functional boundary.

### Start at login

macOS 13+ uses [`SMAppService.mainApp`](https://developer.apple.com/documentation/servicemanagement/smappservice/mainapp) for the current app bundle. The General settings switch reads [the system status](https://developer.apple.com/documentation/servicemanagement/smappservice/status-swift.enum), registers or unregisters only after a user action, and never stores a separate enabled preference. A fresh installation is off. macOS can disable a registered item in System Settings; this appears as off with a System Settings action, and the app does not register it again on launch or status polling. macOS 12 and unimplemented native hosts report unsupported.

The local development and installable release bundle identifiers and default data directories remain separate. Register from the installed Caelis Bot app path; replacing that bundle in place preserves the system choice. After moving the app to a different path, turn the old login item off and re-enable it from the running copy so macOS targets that bundle. The app does not silently unregister and register an existing user choice during updates. A login-item launch is identified from Apple's [open-application Apple event](https://developer.apple.com/documentation/coreservices/1556410-launch_apple_event_constants/keyaelaunchedasloginitem); it starts the existing background owner and pet/menu bar without presenting first-run setup. Manual startup still presents incomplete setup. The existing single-instance lock and original request recovery remain the only execution owners.


默认本地/PR 构建使用 **Caelis Bot Dev.app**，bundle ID 为 `dev.caelis.bot.dev`，
单实例与默认数据目录 `~/Library/Application Support/Caelis Bot Dev` 独立于生产版。
正式发行版使用 **Caelis Bot.app** / `dev.caelis.bot` 和
`~/Library/Application Support/Caelis Bot`。本地 `Caelis Bot Dev.app` 不接收发行更新，
其数据与 TCC 授权不会自动迁移。正式 tag 构建（或显式 `BOT_BUILD_CHANNEL=release`）
才使用 `Caelis Bot.app` / `dev.caelis.bot`；开发启动只重启对应 bundle 路径。
默认签名仍为 ad-hoc。需要反复验收辅助功能、自动化和静态快照时，
可以显式选择本机已有的 **Apple Development** 身份。私钥留在登录钥匙串，
不导出、不修改钥匙串 ACL、不使用正式发行证书；开发包不因此成为已公证发行包。

1. 在 Xcode → Settings → Apple Accounts 中登录，选择团队 → Manage Certificates →
   ＋ → Apple Development。登录、创建和系统钥匙串确认由用户完成。
2. 用 `security find-identity -v -p codesigning` 核对可用的 Apple Development 身份，
   将其 40 位证书指纹保存到 `.development-signing-identity`（一行，无引号）。
   该文件已被 Git 忽略，只保存公开指纹，不保存凭据。
   同一 Git 仓库的 linked worktree 若没有自己的选择文件，会读取主 checkout 的
   `.development-signing-identity`。worktree 内的显式选择优先；删除主 checkout
   的选择后即回到 ad-hoc。不会因为钥匙串中存在证书而自动选择它。
3. 用 `bash script/build_and_run.sh --verify` 构建并启动。构建会验证该身份类型、
   签署内部 Sparkle、Desktop World 适配器、私有 CUA worker 及应用、核验 Apple 签名链和应用标识。
   身份失效/缺少私钥时停止，不静默退回 ad-hoc 或发行证书。

单次覆盖用 `BOT_DEVELOPMENT_IDENTITY=<指纹>`；显式 `-` 表示 ad-hoc。
CI 忽略本机选择和该覆盖，保持凭据无关的 ad-hoc 构建。正式发行仍由原有隔离
Developer ID / 公证流程完成。

首次从 ad-hoc 切换到开发签名仍需系统授权。之后保持证书、bundle identifier 和
应用路径稳定，可以避免每次代码哈希变化导致的身份切换；不能保证 macOS 永不
再次询问。证书过期、变更或系统权限政策变化仍可能需要重新授权。

授权后若当前进程仍报告未授权，使用 `bash script/build_and_run.sh --restart`：
先正常退出开发进程，再启动**同一份二进制**，不重新编译或签名。
`--recall` 仅唤回已有应用，不刷新进程权限。普通用户可从 Bot 菜单退出后再打开。
不要为解决“开关已开”而自动 reset 系统授权。

Desktop World 实机验收使用已签名 Dev 包、隔离的 `CAELIS_BOT_DATA_DIR`
及明确的 `CODEX_HOME` 或独立 Caelis Store；从 `script/build_and_run.sh`
启动后在插件设置中检查版本、开关、Skill 和 Runtime 的唯一 `exec`。
真实模型调用仍走 Runtime 原有审批，再对明确的临时 GUI 目标做一次读取、
操作和读回。核对 Bot 的辅助功能/屏幕录制归属及私有 worker 退出。
旧 `bot_desktop_*` 与 `--desktop-capture-smoke` 已退役；不要用直接运行
`dtw`、fixture 模型或系统权限开关状态替代这项验收。

`TestNativeDesktopWorldPluginDirectory` 是两 Runtime 的按需原生连接检查。
设置 `CAELIS_BOT_TEST_BOT_BINARY` 为已签名 Dev 包的可执行文件，
`CAELIS_BOT_TEST_DTW_STORE` 为隔离插件目录，
`CAELIS_BOT_TEST_DTW_ROOT` 为其中已安装的 v0.1.0 版本目录；分别给 Codex
测试设置 `CAELIS_BOT_TEST_CODEX`，给 Core 测试设置
`CAELIS_BOT_TEST_BINARY`。测试创建私有 App Server/Host 与本地合成模型，
验证 Skill、单 `exec` 和停用撤销。隔离插件目录必须有实际持锁的 Bot owner；
在 App 外运行的 Manager 不会发现内置包，不能仅凭 AcquireDesktopOwner 返回 nil
就认定 owner 已建立。默认不执行 GUI 操作。Codex 测试可显式设置
`CAELIS_BOT_TEST_DTW_FIXTURE` 为 Desktop World 的一次性 scene fixture 目录，
在活动、未遮挡的桌面上验证 PNG 图片描述、一次点击、独立计数读回和新观察。
Codex 的检查 API 不返回 PNG 字节；实际图片 payload 由 Core loader/直接 MCP
验收单独证明。这些本地合成模型测试不代表真实账号模型验收。

稳定身份验证应比较两次不同代码的 `codesign -d -r-` designated requirement；
它应保持相同且不依赖 cdhash。签名一致并不等于真实权限/窗口截图已验收，仍需
在用户授权后的同一开发副本完成点击、最小化、恢复与卡片快照检查。

## Verification matrix

| Change | Required evidence |
| --- | --- |
| Shared behavior/recovery | Regression demonstrating failure, affected Go tests/race, full check |
| Runtime protocol/configuration | Public schema/hash, adapter lifecycle and isolated external Host/App Server |
| UI/character/permissions | Native `script/build_and_run.sh`, actual window observation and focused manual native diagnostics |
| Packaging | Dev build/package/mount, nested native component verification |
| Public release | Exact tag/source, signed public assets/feed, notarization/staples and independent Gatekeeper; see release.md |
| Windows | PR: native shared/platform tests and pinned Plugin verification. Packaging changes: local Setup/ZIP and custom-directory install/update/uninstall checks; publication builds the requested channel. Real GUI, actual release bytes and account behavior need separate evidence; current packages are unsigned |

The required `product` gate selects source checks by changed platform; documentation and valid version-only changes use the lightweight path. PRs do not build a duplicate preview package. See [CI routing](release.md#required-checks-and-branch-policy).

Run native launches only through `script/build_and_run.sh`; use `CAELIS_BOT_DATA_DIR` for synthetic data.
An explicit development data directory also namespaces the native instance lock;
it can coexist with the daily Dev app. Release instances retain one product lock.
Use the [availability fault matrix](availability.md) for recovery changes: test
large unused tool/image frames followed by an approval/terminal fact, local IM
storage failures, original receipts and independent Telegram control lanes.
Recent conversation recovery reads the local IM database and optionally one
latest native summary; it must not traverse full Runtime or Worker history.
`--bubble-preview` provides a long Markdown/streaming fixture. Its “审批恢复回归” button checks that completed
text and queued tails survive approval, review, notice and connection overlays in the mounted production Bubble;
the result is saved to `.cache/bubble-preview.png.replay.json` (run with reduced motion off).
The “展开 / 收起” control exercises the shared native tracking callback, rather than synthesizing
a DOM mouse event. For physical pointer delivery, hover the real bubble while another app is
foreground, then leave and re-enter.
`--chat-preview` runs the production chat component
in a separate native WebKit window with small/large chunks, already-completed replies, a large final chunk and
history reopening. It also offers Dream/running/approval/completion controls and the production pet/bubble
surfaces to inspect napping, input priority and status cleanup. Its capture button saves `.cache/chat-preview.png` and `.cache/chat-preview.png.json`
(frame text/timing, control state, scroll position and final HTML). Both previews use synthetic data and never
connect to the daily Bot or call a model. `--terminal-smoke` uses synthetic scripts without
loading the Bot store/model. Observe physical focus/hit regions and light/dark appearance before claiming visual
acceptance. Avoid logging credentials or full private conversations. Generated `.cache` logs are local evidence,
not public reproducibility prerequisites.

The chat preview also offers thinking/search/read, concurrent stream+tool and completion controls.
“头像回归” runs the mounted production portrait player through these states and approval, verifies
moving pixels/static history/visible activity labels, and asserts that the waiting row disappears
during streaming even with a tool still reported. Save the capture to record `avatar.ok` and samples
in the same JSON. This is native WebKit fixture evidence, not a live backend run.
The regression also waits past completion expiry, requires a static poster with zero queued frame
callbacks, and checks that a later tool-only completion does not reanimate an older message.

“发送回归” exercises the production composer and polling handoff with delayed acceptance,
rejection, an unknown receipt, and accepted submission followed by a failed draft read.
Capture records `send.ok`, submission/draft-read counts and frame samples in the same JSON.
It checks one displayed user message, stable message height, no empty first-reply bubble,
preserved uncertain drafts, and blocked duplicate sending after draft synchronization fails.
Empty-history, recent-history, older-history and delayed-timestamp cases also
require the time boundary in the first visible user frame, stable divider labels
and message positions through native acknowledgement, and no divider for the
first message. Composer progress must stay inside its action button, preserve
Stop and add no height during background draft recovery.
The first visible frame must also have an empty editor/context, exactly one user
message and the Bot thinking indicator, with no separate Sending label. Attachment
acceptance/refusal cases include a stale selection event during dispatch; original
file IDs remain submitted once, disappear on acceptance and return on refusal.
Late authoritative rejection unlocks an uncertain draft without another Submit.
This browser fixture runs on Windows too; its synthetic bridge is not a live
Runtime or native WebKit acceptance. The send handoff changes presentation only;
Bot tools and the existing recovery guidance require no additional skill changes.
These are synthetic native WebKit failure paths; qualify actual model interaction separately
with an isolated Bot profile. Draft writes coalesce only pending full replacements, while
submission waits for the newest saved draft. Snapshot reads are fenced by request order and
visible-surface lifetime as well as backend revision.

Reduced-motion feedback can be inspected with `node script/reduced-motion-preview.mjs`
at `http://127.0.0.1:9247/acceptance`. The fixture mounts the production pet,
portrait and status styles with synthetic native facts, without reading Bot data.
Switch browser `prefers-reduced-motion` emulation while open; the current portrait
and pet must keep moving gently, history must stay still, and status indicators
must pulse without rotation or travel. Scroll indicators/portraits offscreen and
hide the page to verify pause/resume. `script/reduced-motion.test.mjs` covers
amplitude, authored neutral-pose preservation, policy switching and frame cleanup.
The fixture's state buttons cover `think`, `waiting`, `focus` and `companion`;
each reduced-motion portrait must use different state artwork while breathing.
`verifyReducedMotion(page)` from `script/reduced-motion-browser-check.mjs` accepts
an isolated Playwright Page and checks those pixels, incremental DOM observation,
visible production setup spinner, live media preference changes and offscreen recovery.
The fixture is included in TypeScript checking so invalid clip names fail the build.
Chrome media-query acceptance passed on Windows; this is not native WebView2 or
macOS AppKit acceptance. Native checks must additionally cover live system
preference changes and task indicators without changing execution state.

Settings separate Connections & models, Messaging, Remote machines,
Extras and Privacy & permissions. The model and AI account pages share one runtime
settings owner; native `runtime` and `connections` repair links still open AI accounts.
`telegram` opens Telegram detail, `chat` opens the channel overview and `capture`
opens Extras.
`runtime-settings-preview.html` uses the same components with
example data for narrow layouts, without modifying daily accounts or calling a model.
The development-only `settings-extras-preview.html` mounts production Settings
and first-run navigation with synthetic statuses for manual layout review. The old
WKWebView settings fixture and its screenshot assertion matrix were removed from
the default test path. Inspect the actual settings surface through
`script/build_and_run.sh` for native focus, resizing and persistence behavior.

```sh
source script/env.sh
go test -race ./internal/backend/codex ./internal/backend/caelis ./internal/bot ./internal/care
CAELIS_BOT_TEST_BINARY=/absolute/path/to/caelis make smoke-caelis
```

`smoke-caelis` launches its own external Host against temporary HOME/Store/Notebook and a synthetic provider;
it neither imports Core internals nor reads daily model credentials. It includes native tools, resources,
workers, hot configuration, environment, skills, Dream, Guardian callback/native continuation, exact identity,
restart replay and a real timeout. Missing external binary skips these tests in ordinary `make check`.
Installed Codex progressive-skill/context fixtures are under `internal/backend/codex/*live*test.go`; inspect their
explicit environment opt-ins. Synthetic provider results prove transport/behavior, not real model judgment.

`make smoke-caelis-live` is separately opt-in and billable: configure an isolated Store, then set
`CAELIS_BOT_LIVE_STORE`, `CAELIS_BOT_LIVE_BINARY`, `CAELIS_BOT_LIVE_MODEL` and optional alternate/fast models.
Never copy daily credentials into logs or use a shared Store for destructive fixtures.

## Platform limits

See [current status](status.md) for the release/source distinction and outstanding coverage. Bot's macOS deployment target is 12.0; bundled Desktop World requires arm64 and macOS 14.0. Compilation does not qualify minimum-OS permissions, Intel, multi-display/DPI behavior or real-account model judgment. Linux support is remote/headless only.

## Interface languages


The application language preference is `system`, `en` or `zh-CN`. Desktop owns
`language.json` in the application data directory and persists it atomically before
publishing a revisioned `language-changed` event to every surface. This preference
is independent of Runtime, models, conversation identity and execution policy.

`system` resolves the first supported language in macOS's preferred language list
at application launch. Chinese variants currently select Simplified Chinese;
unsupported lists fall back to English. Changing the OS language list takes effect
on the next application launch. An explicit in-app choice takes effect immediately
in React surfaces, application menus and future host notifications.
System-owned dialogs, Edit menus and Sparkle use macOS/framework localization and
may require an application restart or follow the OS language rather than this
preference. Do not replace their lifecycle or hand-translate OS controls.

## Shared catalog

`internal/i18n/locales/{en,zh-CN}/<namespace>.json` is the sole source of application
UI translations. Go embeds these files; TypeScript imports them with typed keys.
Namespaces are `common`, `settings`, `chat`, `runtime`, `connections`, `native` and
`host`. Entries use stable semantic keys, not the original sentence or line number.

```json
{
  "connectedAs": "Connected as {name}",
  "fileCount": {"one": "{count} file", "other": "{count} files"}
}
```

Use complete sentences, named `{parameters}` and cardinal `one`/`other` forms.
English plurals require both; Chinese requires `other`. Supply raw numeric `count`
for plural selection. Do not concatenate translated fragments or implement ICU
syntax, HTML translations or runtime string replacement dictionaries. React escapes
interpolated values. Unknown keys fall back to English, then the key; CI requires
matching languages so fallback does not excuse incomplete release translations.

```tsx
const {t, number, date} = useI18n();
// These illustrative keys must first be added to both namespace files.
t('runtime.connectedAs', {name});
t('chat.fileCount', {count: files.length});
number(bytes / 1024, {maximumFractionDigits: 1});
date(timestamp, {dateStyle: 'medium', timeStyle: 'short', timeZone});
```

`I18nProvider` wraps each application surface. Use the hook inside React components,
including effects that need localized content. Initialization and subscription
effects use `useEffectEvent` to read current translations without reloading drafts,
resetting connection flows or refetching configuration when the language changes.
Prefer storing error
keys/parameters in component state and translating at render time. Pure helpers
accept the translator or locale explicitly; no global translator singleton,
localStorage preference or independently inferred browser language.

Go uses `i18n.Text(locale, "namespace.key", args)`. Desktop reads
`LanguagePreferences().Locale`; non-desktop owners receive the live locale callback
through `app.Host.Locale`. A notification reads it when emitting new content, not
when starting the Runtime. Locale changes do not reannounce previous notifications.
Diagnostic codes/logs and protocol enums stay stable, normally English. Display
known errors with localized context; preserve unknown third-party error details.
Do not classify protocol outcomes by matching translated strings.

## Content boundaries

Never translate or rewrite user messages, task prompts, model responses, reasoning,
tool arguments/results, model/Agent/provider identifiers, file paths, commands,
credentials, stored history or diagnostic evidence. Interface language does not
change the model's reply language. Do not translate fixed model-facing instructions,
notebook templates or skills as a UI migration. Keep authority, approval scope,
unknown-result handling, silent reminder behavior and native receipts unchanged.

## Validation

- `npm run check:i18n` checks namespace/key parity, nonempty values, placeholders,
  plural forms, interpolation safety, locale formatting and stale revision handling.
  It is part of `make check`.
- `node script/i18n-inventory.mjs` lists candidate source lines for manual migration.
  Chinese comments, model instructions and user fixtures may be intentional; English
  literals also require review. The inventory is not a zero-Han-character gate.
- Go desktop tests cover persistence, invalid/failed saves, concurrent publication,
  restart restoration and corrupt-file preservation. Notification tests cover live
  language selection without replaying existing decisions.
- `frontend/i18n-preview.html` is a development-only view of the real language
  selector and provider with an in-memory bridge. It never changes daily preferences.
  Switching must preserve the editable draft and update both provider instances.

Conversation, settings, Runtime/Team and connection controls, native application
menus and host lifecycle notifications use the shared catalogs. Approval producers
mark Bot-owned titles, choice labels and explanation sections with presentation keys;
both chat and the pet bubble translate those keys at render time. The original
request ID, choice ID, scope, answers, command, permission payload and provider text
remain unchanged. A pending approval or failed-save draft survives language changes.
Task localization uses its own lock and invokes the host locale callback outside it.

Runtime-originated descriptions, connection progress and unknown error details may
retain the Runtime's language. User/model text, diagnostic evidence and macOS/Sparkle
controls follow the boundaries above. This is not a promise to translate external
content or every provider error.

For release acceptance, verify English, Chinese and Follow System in native windows,
language persistence, simultaneous surfaces, minimum-size layout, pending approvals,
failed-save drafts and connection progress. Run `make check`, owning race tests,
`make smoke`, `make build` and the signing/notarization gates in [release.md](release.md).

## Plugin connection acceptance

For GitHub browser authorization, `caelis-labs` owns a public GitHub App with
Device Flow enabled. The reviewed catalog contains its public Client ID and
installation URL; no client secret or user token is embedded. Each user installs
the app on selected repositories, then authorizes the device code in GitHub.
The resulting user token is limited by the app's repository permissions, the selected
repositories and the user's own access. GitHub's remote MCP service does not
offer dynamic client registration. Settings also offers the existing personal
access token path. Verify both the tool catalog and a safe repository read in
Dev Bot; authorization alone can succeed without repository installation.
The remote GitHub MCP selection uses its reviewed default endpoint. The app grants
Contents, Issues and Pull requests read/write, with Metadata read; other repository,
organization and account permissions are not requested. Existing installations
must approve GitHub's permission update before write tools can succeed. Verify
actual available tools before claiming the expanded capability, and use only safe
read operations for Dev acceptance unless an exact write is separately authorized.

For Obsidian, Bot Settings installs one reviewed Skill for the official desktop
CLI; it contributes no MCP server or credential connection. Enable **Command
line interface** under Obsidian Settings > General and keep Obsidian running.
The Skill instructs Bot to target an explicit user-chosen vault, verify its
name, and use the Runtime's normal command and approval flow. An unknown vault
can be reported on stdout with exit code zero, so the exact CLI output matters.
Verify that the Bot-private Skill is discoverable and loaded, then ask Dev Bot
for a safe read-only operation against a dedicated Dev vault. A standalone CLI
call or Skill fixture alone does not prove Bot execution. Ordinary sessions
and Workers must not inherit this Bot plugin.

Do not start two Dev Bot processes against the same data directory. When
checking the existing Dev profile, use `script/build_and_run.sh --restart` only
after the currently running Dev Bot has stopped, or use a separate disposable
`CAELIS_BOT_DATA_DIR`.

## Remote machine acceptance

`make build` embeds checksum-pinned Linux amd64/arm64 headless helpers in
`Contents/Resources/remote`. These remote helpers do not contain a Runtime;
the macOS app separately embeds the pinned local Caelis CLI described in
[runtime integration](caelis-integration.md). Native APP
acceptance still uses `script/build_and_run.sh` with an explicit disposable
`CAELIS_BOT_DATA_DIR`; never use the daily profile for connection/model experiments.
The remote account must have its own installed and authenticated native Runtime.

```sh
bash script/build-remote-helper.sh "$PWD/.cache/remote-helper"
CAELIS_BOT_TEST_SSH=fedora \
CAELIS_BOT_REMOTE_HELPER_DIR="$PWD/.cache/remote-helper" \
CAELIS_BOT_TEST_EVIDENCE="$PWD/.cache/remote-e2e" \
GOWORK=off go test ./internal/machines -run TestFedoraNativeWorkers -count=1 -timeout=5m -v
```

This opt-in test performs real inference on the selected machine, allocates new
private connection profiles, uses that machine's existing login, sets only the
profile-local Codex work model, and writes a single marker in each task's private
workspace. It verifies two native TUI observers, observer disconnect, controller
reopen, original native binding and exact remote file bytes. It then continues,
interrupts and resumes that same Worker and verifies the original native identity
and one-Worker count. It does not change
global model accounts or Team configuration. Raw evidence under `.cache` may contain
native target paths/identities; publish only redacted summaries. Detached owners and
native bindings remain on the target for original-task recovery.

The machine connection editor first offers **Existing SSH config** or **New connection**.
The existing route lists literal `Host` aliases from `~/.ssh/config`, the system config
and bounded `Include` files; wildcard and negated patterns are not offered as machines.
Search/refresh never edits those files. OpenSSH evaluates the selected alias and supplies
its user, port, identities and supported single-hop `ProxyJump`; manual form defaults do
not override it. Fingerprint confirmation and the app-owned known-hosts/control socket
remain in force. Optional passwords/key passphrases stay write-only and use the existing
explicit Keychain opt-in. `ProxyCommand` and multi-hop jumps remain unsupported.
For a non-billable real connection acceptance, set `CAELIS_BOT_TEST_CONFIG_SSH=fedora`
and `CAELIS_BOT_REMOTE_HELPER_DIR` to the built helper directory, then run
`GOWORK=off go test ./internal/machines -run '^TestFedoraSSHConfigConnection$' -count=1 -v`.
OpenSSH `-o` values are parsed again as config syntax: app-owned known-hosts paths
are quoted and percent-escaped, including the daily macOS `Application Support` path.
SSH host-verification failures use a bounded in-memory diagnostic and a typed error;
raw stderr is never returned or logged. Connection acceptance must include a path
with spaces, rather than only a temporary directory.

Saved machines offer **Delete machine** in the fixed footer in both connection and
work steps, including offline profiles. The confirmation only removes Bot registration
and its remembered credential; user SSH config, remote files and runtimes are kept.
Task-owned profiles still reject removal. Cancel/Escape retains the machine and draft.
This settings-only workflow adds no Bot tool or changes to task routing, so Bot skill
instructions do not need an update.

Native OpenSSH authentication acceptance uses an isolated loopback SSH fixture,
disposable keys/passwords and an independent agent. Set
`CAELIS_BOT_SSH_AUTH_PYTHON` to an explicit Python with Paramiko and run
`GOWORK=off go test ./internal/machines -run TestNativeSSHAuthentication -count=1 -v`.
Do not add Python dependencies to the APP. The macOS Keychain canary lifecycle is
opt-in via `CAELIS_BOT_KEYCHAIN_TEST=1`; it creates, updates and deletes only one
uniquely named disposable item.

The development-only `runtime-settings-preview.html?machine=login&lang=en` renders
the same settings components with visual fixtures. Scenarios include `missing`,
`offline`, `caelis`, and `caelis-advanced-error`; `lang=zh-CN` selects Chinese. The
`caelis-advanced-reopen` scenario holds advanced reads until the fixture releases
them. Inspect the native
settings on the real node in both languages and wide/narrow layouts. Resize must
keep the same draft; primary connection actions stay visible at the 860×640 minimum.
Check private-key fields, trust confirmation, error messages, the collapsed optional
Team section and its nested model dialog.

Model parameters use one compact picker for Bot, work and Team bindings. Selecting
the model opens a separate searchable list. Effort stops and Fast are projected
from native model capabilities; a catalog recommendation is never presented as
the user's configured default. The optional Team editor is a separate dialog, and
the external terminal preference belongs to General settings.

For a non-billable Fedora model/settings check, build the remote helper, then run
`CAELIS_BOT_TEST_MODEL_SSH=fedora CAELIS_BOT_REMOTE_HELPER_DIR=<absolute-helper-dir>
GOWORK=off go test ./internal/machines -run '^TestFedoraModelSettings$' -count=1 -v`.
This checks Codex/Caelis capabilities, configured-default reads, node-local
Effort/Fast save, reconnect and reset. It starts no inference and changes no global
runtime model, account or Team. `CAELIS_BOT_TEST_MODEL_STORE` may name an empty,
disposable controller directory retained for native settings QA; existing profiles
are rejected. This metadata check does not replace the billable Worker acceptance.

For an explicitly retained task in a disposable native acceptance profile, the
developer-only `--remote-terminal-smoke <absolute-profile> <original-task> <terminal>`
mode uses the production `WindowManager` and original SSH target without submitting
or stopping work. Run it through `script/build_and_run.sh`. It checks two observer
cycles, collapse/restore and retained bindings; inspect the actual external terminal
window during the cycle. This requires access to the terminal GUI and any normal OS
permissions. A launch request, window fixture, or remote tmux test alone does not
qualify real external-terminal behavior. Record terminal visual results separately from transport and lifecycle checks.

Worker-default switching acceptance uses one SSH profile with both native backends:
`CAELIS_BOT_TEST_SSH=fedora CAELIS_BOT_REMOTE_HELPER_DIR="$PWD/.cache/remote-helper" GOWORK=off go test ./internal/machines -run '^TestFedoraDefaultSwitch$' -count=1 -timeout=10m -v`.
It creates two private tasks and checks exact artifact bytes, controller reopen,
old-task continuation/interrupt and unchanged terminal bindings across both defaults.

Local native acceptance is opt-in and performs real inference in private WorkOwner
bindings: `CAELIS_BOT_TEST_LOCAL_WORKERS=1 CAELIS_BOT_TEST_LOCAL_CODEX_MODEL=gpt-6-luna GOWORK=off go test ./internal/app -run '^TestLocalNativeDefaultSwitch$' -count=1 -timeout=7m -v`.
The optional `CAELIS_BOT_TEST_LOCAL_CAELIS_STORE` and
`CAELIS_BOT_TEST_LOCAL_CAELIS_MODEL` select an already configured runtime/model;
no global account or model settings are changed. The explicit Codex model uses its
catalog default effort. Selecting an available, authenticated model is distinct
from proving task routing; an unusable runtime default can fail native inference.


## Documentation maintenance

Use the [documentation index](README.md). Keep current contracts, operator commands and explicit evidence
limits in the owner guide. Do not append dated handoff diaries, duplicate implementation plans or copy skills.
Historical decisions/acceptance remain in Git and linked PRs. When replacing a guide, update all repository
links and AGENTS instructions. Protocol/generated schemas, source tests and English Bot skills remain separate
sources of truth. A code-only regression fix that preserves the workflow needs no new skill instructions.

### Connection settings navigation

The Connections and models page separates model use (Bot conversation and new
background work) from runtime management (built-in and Codex). Both runtimes
can be prepared independently. Runtime details, program paths, provider setup and
built-in Team configuration are full pages with a back path; only short model
edits and destructive confirmations use dialogs. Team is an optional built-in
advanced setting, not a shared execution setting.

Model choices carry their runtime source; previously loaded catalogs remain available during a transient outage.
`SetupModels` inspects a profile without selecting it; `LocalWorkerSelection`
reads saved preferences without opening an offline worker and migrates empty or confirmed obsolete Worker choices to follow conversation. Temporary catalog failures preserve explicit choices. The selection
coordinator validates against the selected runtime's catalog before applying.
Existing work retains its original owner. `SaveSelectedModel` journals user
intent without waiting for catalog inspection, task completion or handshake.
`ModelSelections` distinguishes saving, pending, applying, recovering and applied
states. The coordinator retries bounded attempts and resumes pending choices on
restart; original receipts remain authoritative. Conversation handoff fences
pending sends internally, while Worker activity never blocks preference saves.
The UI keeps cached catalog choices during failed refresh and retries delivery
of selection intent without replaying any model/tool/approval operation.
Codex discovers its existing installation and authentication; provider setup is
exclusive to built-in. Magpie obtains model choices from its gateway.
Profile-scoped connection removal preserves admission, usage and revision checks.

Bot recovery guidance describes independent task ownership and automatic settings
recovery. Native acceptance must use the isolated Dev profile and
must not be inferred from the fixture/Go checks.

Codex settings distinguish missing installation, handshake failure, login and ready
states. Installation/update uses a blocking indeterminate progress dialog; only
confirmed completion advances the page. Program paths and Auto-Review are not
shown in connection settings; existing native defaults and saved policy remain
unchanged. This presentation change requires no Bot skill update.

Built-in settings separate runtime preparation/updates, provider connections and
optional Team configuration. Team presents breeze, orbit, zenith, tool-search and
a single guardian card. Guardian configures the Host's LLM approval binding;
an optional classifier entry configures guardian-screen separately. Uncertain
classifier results retain the Host's LLM review fallback. Eligible models and
Fast support come from Host capabilities. Opening an unbound role preselects a
model in the local draft only; saving is explicit. Other roles and saved binding
sets are not removed. No Bot skill update is needed: these are settings for
existing capabilities. Live classifier acceptance requires an eligible classifier.

Built-in provider forms use Host model catalogs. DeepSeek and Xiaomi use the
Host default endpoint and expose model checkboxes plus API key only. Multi-model
connection is sequential: each native receipt must complete before the next
model starts; failed/unknown outcomes stop the sequence with completed items
retained. Secrets remain transient write arguments. Runtime version management
is the last independent section; connection usage labels are presentation-only
and do not change removal guards. These settings do not add Bot capabilities or
require Bot skill guidance changes.

### Isolated first-run review

`BOT_BUILD_CHANNEL=onboarding BOT_DEVELOPMENT_IDENTITY=- make build` produces
`dist/Caelis Bot Onboarding.app` with a separate development bundle identity.
Launch with `bash script/run-onboarding.sh --restart`, never by opening the bundle
alone: the launcher supplies separate Bot data, Runtime HOME, CODEX_HOME and a
minimal runtime PATH. `BOT_ONBOARDING_ROOT=/absolute/disposable/root` selects a
review profile; the default `.cache/onboarding-experience` is for user acceptance.
Do not copy credentials or existing Runtime stores into it. macOS permissions
remain system-owned. Global `/opt/homebrew/bin` or `/usr/local/bin` Runtime installs
may still be discovered; inspect those before claiming a missing-Runtime scenario.

First-run order is Runtime detection/preparation and authentication/model selection,
optional features, then name/description. System permissions are requested at use.
Resident activation overlaps the later pages; only the final Start chatting action,
after real readiness, journals the introduction. Installation waits for its native
result; built-in download progress reports observed bytes and speed, with an
indeterminate animation when no total is known. Cancellation stops the installer
process group and the UI waits for the original request. Changes here are setup
presentation/installation only; Bot-facing skill guidance does not change.

Windows onboarding timing and validation for the shared preparation path are
recorded in [the performance check](windows-onboarding-performance.md). Native
macOS acceptance remains a separate gate from portable tests and cross-compilation.

The introduction uses the route screen's avatar/title and an open name/optional
description column, with borders only on inputs. Windows native review covered the existing-auth route, required
name/button state and retaining the draft through Back/Next. Notification observer
tests cover ordinary-reply silence, current-turn reminder excerpts, restored/quiet/
failed results, decision deduplication and resolution cleanup. The Windows native
toast fixture verified shell-history persistence and selective dismissal; it does
not stand in for popup/click acceptance or macOS native notification validation.

### Mounted recovery regression

Run `node script/runtime-recovery-preview.mjs` with the pinned Node runtime and open the printed loopback URL.
The “Run recovery regression” button mounts the production account wizard with disposable catalogs: rejected,
empty, removed choice, unavailable and transient failure. It checks finite reads, Back/retry, one authentication
start after recovery, absence of private error text, and recovery buttons that cannot submit an enclosing form. It also exercises first-run and artifact-preview read exhaustion and ensures local recovery stays within the affected panel.
The fixture has no account or real mutation authority.
`script/build_and_run.sh --chat-preview` also retains the mounted send/refusal/unknown/draft-read scenarios.
These browser/WebKit fixtures do not prove real-account authentication or Windows native behavior.

## Registered-event verification

Run `go test -race ./internal/eventplan ./internal/bot ./internal/plugins
./internal/botskills` under `script/env.sh`. Fixtures cover scripted collection,
MCP handshake/call and generation fences, declared output validation, CEL
filtering, native grant rejection, original wake receipt recovery, cancellation,
and terminal archive retention. Run `make check`, `make smoke`, `make build` for
the full preparation paths. These are source/fixture checks; they do not claim
live mailbox/calendar acceptance, universal model adoption, or Windows script
collector support. Current model-facing examples live in Bot-core's
`references/events.md`.
