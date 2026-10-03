# Development and verification

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

`make check` covers generated contracts, protocol hashes, public-tree/asset boundaries, frontend/i18n, native
lifecycle fixtures, Go vet/tests and shared-core portability. `make smoke` only handshakes the installed Codex
and checks assets; it does not create a conversation or call a model. `make build` creates an ad-hoc Dev app.
`make package` adds a verified read-only DMG/checksum, using pinned Python dmgbuild in `.cache/dmg-tools`.
First builds fetch pinned Sparkle and Desktop World artifacts; versions/hashes remain source-controlled.

## Native development


默认本地/PR 构建使用 **Caelis Bot Dev.app**，bundle ID 为 `dev.caelis.bot.dev`，
单实例与默认数据目录 `~/Library/Application Support/Caelis Bot Dev` 独立于生产版。
原生产数据与 TCC 授权不会自动迁移。正式 tag 构建（或显式 `BOT_BUILD_CHANNEL=release`）
才使用 `Caelis Bot.app` / `dev.caelis.bot`；开发启动只重启对应 bundle 路径。
默认签名仍为 ad-hoc。需要反复验收辅助功能、自动化和静态快照时，
可以显式选择本机已有的 **Apple Development** 身份。私钥留在登录钥匙串，
不导出、不修改钥匙串 ACL、不使用正式发行证书；开发包不因此成为已公证发行包。

1. 在 Xcode → Settings → Apple Accounts 中登录，选择团队 → Manage Certificates →
   ＋ → Apple Development。登录、创建和系统钥匙串确认由用户完成。
2. 用 `security find-identity -v -p codesigning` 核对可用的 Apple Development 身份，
   将其 40 位证书指纹保存到 `.development-signing-identity`（一行，无引号）。
   该文件已被 Git 忽略，只保存公开指纹，不保存凭据。
3. 用 `bash script/build_and_run.sh --verify` 构建并启动。构建会验证该身份类型、
   签署内部 Sparkle、Desktop World helper及应用、核验 Apple 签名链和应用标识。
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

稳定身份验证应比较两次不同代码的 `codesign -d -r-` designated requirement；
它应保持相同且不依赖 cdhash。签名一致并不等于真实权限/窗口截图已验收，仍需
在用户授权后的同一开发副本完成点击、最小化、恢复与卡片快照检查。

## Verification matrix

| Change | Required evidence |
| --- | --- |
| Shared behavior/recovery | Regression demonstrating failure, affected Go tests/race, full check |
| Runtime protocol/configuration | Public schema/hash, adapter lifecycle and isolated external Host/App Server |
| UI/character/permissions | Native `script/build_and_run.sh`, actual window observation, lifecycle/resource fixtures |
| Packaging | Dev build/package/mount, nested native component verification |
| Public release | Exact tag/source, signed public assets/feed, notarization/staples and independent Gatekeeper; see release.md |

Run native launches only through `script/build_and_run.sh`; use `CAELIS_BOT_DATA_DIR` for synthetic data.
`--bubble-preview` provides a long Markdown/streaming fixture. Its “审批恢复回归” button checks that completed
text and queued tails survive approval, review, notice and connection overlays in the mounted production Bubble;
the result is saved to `.cache/bubble-preview.png.replay.json` (run with reduced motion off).
The “展开 / 收起” control exercises the shared native tracking callback, rather than synthesizing
a DOM mouse event. `bubble-hover-native-test.sh` checks always-active tracking, resize, exit, hide
and detachment without taking key focus. These injected events do not qualify physical pointer
delivery: also hover the real bubble while another app is foreground, then leave and re-enter.
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

```sh
source script/env.sh
go test -race ./internal/backend/codex ./internal/backend/caelis ./internal/bot ./internal/care ./internal/desktopcontrol
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

## Current evidence and limits

The 2026-09-29 candidate passed check/smoke/build and affected race suites. Downloaded and checksum-verified
Core v0.65.0 darwin-arm64 passed NativeHost and GuardianHost integration, including real 90-second timeout,
upgrade renewal and replay identity. Streaming grapheme append, exact review accounting and preservation of
failed Guardian notices through quiet care/Dream projection have focused regressions.
Desktop World migration (2026-09-30): the Go module and downloaded darwin-arm64
helper are pinned to v0.1.0-alpha.2 / `cc50357f9f922712ef4e2bf4db4822381d10d71a`.
`resources/desktop-world/release.json` owns the archive checksum/revision;
`desktop-world-runtime.sh` verifies these before staging. The bundle contains the
independent helper and upstream NOTICE, with no Cua/Node runtime. This preview
has no open-source license grant and its upstream artifact is not notarized.
Bot development signing does not establish public release acceptance.
Product CI and release packaging now require the Desktop World payload, verify
its revision/protocol/NOTICE and nested signature, and reject missing payloads.
Product CI runs authority/receipt/projection race coverage and an opt-in test of
the actual packaged Go host/helper without grants or UI input. Legacy runtime,
focus adapters, AXorcist experiment and capability-based fallback are removed;
public-tree checks prevent their payloads returning. Build resources are freshly
staged so incremental builds cannot retain old runtimes. Historical app payloads
require their matching historical release tooling.

Alpha.2 uptake acceptance (2026-09-30): all three published assets were downloaded;
both archives matched SHA256SUMS and the actual helper reported the pinned
version/revision with `vcs.modified=false`. Normal `make check`, `make smoke` and
`make build` passed with the published Go module and helper, without a module
replacement. The built Bot binary records the alpha.2 SDK dependency; its nested
helper passed the repository manifest/signature/dependency checks and an
escalated deep strict signature check using the existing development identity.
The official SDK passed real AppKit numeric/string/redaction/receipt regressions
([value evidence](evidence/desktop-world-alpha2/values.txt)). The actual bundled
helper passed real slow-AX timeout/recovery and separate EndTurn revocation
([timeout evidence](evidence/desktop-world-alpha2/timeout.txt)); Bot controller
race tests, including packaged helper refusal/reconciliation, passed
([adapter evidence](evidence/desktop-world-alpha2/adapter.txt)).
These native probes were compiled inside Bot's unchanged normal module graph
from the published source archive; no local source dependency override was used.
A native Bot launch through `script/build_and_run.sh --restart` with an isolated
profile failed at Launch Services with `-10810`, before readiness. The existing
user Bot was untouched, and no trust/TCC settings were changed. This uptake does
not claim a fresh full Bot/Wails/model run, original WPS Save-dialog acceptance,
Windows native desktop acceptance, or public notarized Bot release acceptance.
See [upstream alpha.2](https://github.com/caelis-labs/desktop-world/releases/tag/v0.1.0-alpha.2)
and its `docs/ax-regressions.md` for scoped native Chrome and remaining limits.

Current migration evidence: `make smoke`, `make build`, all Go tests/vet and the
shared-core portability gate passed. The nested helper passed version/revision,
architecture, Apple Development identity and hardened-runtime checks. Race tests
cover exact observed app grants, revocation while data blocks, stable-ID
conflicts/deduplication, original receipt recovery and the 32 KiB output budget.
The real pinned Go host also ran through separate OS data/control pipes. The
actual packaged helper passed handshake, bounded read, invalid-input refusal and
receipt recovery without granting any application or sending UI input. Codex's
installed App Server passed progressive loading of the new guide. Caelis v0.65.0
NativeHost/GuardianHost acceptance passed, including progressive guide loading,
provider image transport, per-turn review/revocation, worker isolation and the
real 90-second reviewer timeout; native input in that fixture is synthetic.

After unlocking the Mac, the full `make check`, `make smoke` and signed
`make build` passed; this includes the native window hide/show check that had
timed out while locked. The development app was restarted through
`build_and_run.sh --restart` and its actual conversation surface inspected.
The installed Caelis v0.65.0 runtime (live turns used `deepseek-flash`) discovered
and executed the new tools through the real Bot/Wails application.

Live evidence is under `.cache/desktop-world-live-20260930/`. In the native
AppKit fixture, Bot used one two-step `set_value` + `invoke` plan to enter
`Desktop World 联调成功 🌍 2026-09-30` and submit once. Independent fixture JSON
confirmed the complete Unicode string and `submissions:1`; cursor sync returned
only the two changed objects. Chrome also completed a new-tab navigation to
`https://example.com`, preserving previous tabs and checking title, address and
page text. Neither task requested a screenshot. These are live input results;
the separate Guardian fixture still proves policy behavior with synthetic input.
Obsidian created the requested note in the existing vault: independent file
inspection confirmed the exact three lines and intact URL. The first title
write changed only the editable control; Bot detected the unchanged filename
and committed it with foreground Enter before reporting completion.

WPS exposed the New popover as 13 semantic buttons and reached the native save
dialog. Two overly broad dialog reads timed out; a narrower read recovered
without restarting the helper. The first saved document failed independent
OOXML inspection: WPS recovery content remained and both paragraphs were bold.
An unlabeled button had not established a blank-document action, and Home/End
selection assumptions were invalid. Bot corrected only the new acceptance file
through UI, using explicit captures where WPS AX exposed neither body text nor
formatting. Final OOXML inspection confirmed exactly the two requested paragraphs,
first bold and second explicitly nonbold. The failed first artifact is retained
separately. This is an assisted successful workflow, not a first-attempt autonomy
or usability pass. The guide now forbids guessing unnamed controls and requires
checking document identity, initial content and application-level commit state.

The retired Cua audit explains the migration: the WPS turn used 33 calls in
790 seconds, 79 seconds inside tool intervals, 26 images, four identical images
and nine repeated AX states. Result text alone estimated 54,110 tokens. Most
elapsed time was outside the tools, so removing repeated payloads and model
round trips matters alongside native speed. Desktop World now exposes bounded
observations, native cursor deltas and 16-step local plans, with explicit capture
only. The first live fixture task took about 111 seconds with six desktop calls;
their combined intervals were 22.8 seconds, including 15.3 seconds for review,
while native execution of both input steps took 151 ms. This is not yet a fast
end-to-end experience: the resident turn carried roughly 230k tokens of prior
context. The Chrome task also exposed excessive outline pagination, a changed
continuation query, an expanded application name rejected by exact-name grants,
and a web AX tree with single-character text nodes. The guide now starts with
fewer fields, inspects only candidate capabilities, preserves continuation query
parameters, copies exact names and batches fragmented text instead of reading
one character per model call. Tool metrics record result bytes, approximate
UTF-8-bytes/4 token counts, call intervals and native sample/step timing; those
estimates are not the provider's exact tokenization. Reused pagination samples
must not be counted as new native scan time.

That run also reproduced a Bot presentation bottleneck: while the native turn
had completed, its UI still showed working. The Caelis feed handler rewrote the
entire connection record for every transient prose fragment; the live record
was 16.5 MB, of which 16.2 MB was retained callback receipts. A bounded spool
sample contained 3,827 transient events versus 22 canonical ones. Transient,
non-final message/thought chunks now checkpoint at most once per second and
publish at most every 50 ms; canonical/final events, approvals, callbacks and
stream boundaries still persist immediately. Cursor and projection are saved
atomically. Regression coverage simulates a crash between checkpoints and exact
replay without missing or duplicated text, plus immediate terminal persistence.
The rebuilt app recovered the completed WPS reply and subsequently presented a
new fixture task and a receipt-only turn as completed. Full checks, race tests,
signed build and installed Caelis NativeHost integration passed after this fix.

The historical alpha.1 live fixture task identified an upstream inconsistency:
Darwin `node()` stringifies numeric AX values for `value_preview`, but its `text`
operation falls back to the label for non-string AX values. The engine's `value`
predicate uses that text operation. Consequently, a checkbox click succeeded
while `value == "1"` timed out. Bot reconciled the same request without clicking
again and independently observed checked=1 / Visible tasks=2; Submitted remained
1. A later Bot turn recovered the same receipt without authorization, observation
or input. That alpha.1 guide avoided numeric `value` predicates and verified these
controls by fresh observation. Published alpha.2 fixes the numeric AX value/text
contract and rejects label fallback as value evidence; the current guide uses
that contract while preserving receipt-only recovery after uncertain delivery. See upstream
[Desktop World #1](https://github.com/caelis-labs/desktop-world/issues/1).
The other live findings are tracked as bounded-read improvements in
[#2](https://github.com/caelis-labs/desktop-world/issues/2) and fragmented browser
text usability in [#3](https://github.com/caelis-labs/desktop-world/issues/3).
Those two are improvement requests, not claims of a helper crash. WPS missing AX
body/formatting, shared input focus and the unimplemented managed JS bridge remain
explicit capability limits. Bot feed persistence was a downstream defect fixed here.

First integration intentionally uses the Go host contract. The upstream managed
JS bridge remains unimplemented; do not launch its separately authorized CLI
from the Bot. Alpha input still shares system focus and pointer. Unknown input
is reconciled using the original requestId; no restart or alternate backend
replays it. Helper history is process-local and capped at 4096 records.

Earlier v0.61.0 MiMo/Luna live results cover that historical baseline only. Subsequent changes require their
own regression evidence, and subsequent Core releases need the same external acceptance suite.

Native bubble/Glass/F1 theme and setup preview were inspected on a single Retina Apple Silicon Mac.
Minimum macOS 12 deployment target and Intel/mixed-DPI/multi-display behavior are not qualified by compilation.
Desktop World alpha requires macOS 14+ and arm64; ScreenCaptureKit selection requires 14+. Older systems return unsupported
for those capabilities. Windows shared-core cross-compilation is checked; native GUI/IPC/distribution is absent.
Linux is outside scope. Sparkle fixture tests do not prove two-version public install/relaunch with active work.

Real Guardian quality/cost, long-term Dream summary/memory quality and cache benefits remain usage evaluations.
Keep these limits separate from regressions, synthetic protocol coverage and public signature verification.

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

## Remote machine acceptance

`make build` embeds checksum-pinned Linux amd64/arm64 headless helpers in
`Contents/Resources/remote`. It does not bundle a Codex/Caelis Runtime. Native APP
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
`offline`, `caelis`, and `caelis-advanced-error`; `lang=zh-CN` selects Chinese. They
never access SSH or accounts and are not evidence of a real login. Inspect the native
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
qualify real external-terminal behavior. See the
[current acceptance record](evidence/remote-machines-v1/acceptance.md), which retains
this explicit gate instead of claiming all native window interactions passed.

## Documentation maintenance

Use the [documentation index](README.md). Keep current contracts, operator commands and explicit evidence
limits in the owner guide. Do not append dated handoff diaries, duplicate implementation plans or copy skills.
Historical decisions/acceptance remain in Git and linked PRs. When replacing a guide, update all repository
links and AGENTS instructions. Protocol/generated schemas, source tests and English Bot skills remain separate
sources of truth. A code-only regression fix that preserves the workflow needs no new skill instructions.
