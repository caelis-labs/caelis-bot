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
First builds fetch pinned Sparkle and Computer Use artifacts; versions/hashes remain source-controlled.

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
   签署内部 Sparkle、Computer Use 原生组件及应用、核验 Apple 签名链和应用标识。
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
`--chat-preview` runs the production chat component
in a separate native WebKit window with small/large chunks, already-completed replies, a large final chunk and
history reopening. It also offers Dream/running/approval/completion controls and the production pet/bubble
surfaces to inspect napping, input priority and status cleanup. Its capture button saves `.cache/chat-preview.png` and `.cache/chat-preview.png.json`
(frame text/timing, control state, scroll position and final HTML). Both previews use synthetic data and never
connect to the daily Bot or call a model. `--terminal-smoke` uses synthetic scripts without
loading the Bot store/model. Observe physical focus/hit regions and light/dark appearance before claiming visual
acceptance. Avoid logging credentials or full private conversations. Generated `.cache` logs are local evidence,
not public reproducibility prerequisites.

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
Cua App × Turn coverage substitutes only the underlying SDK UI fixture; it is not real WPS acceptance.
The same-day Computer Use repair additionally exercised the production Go driver/private Cua 0.30.2
helper on this Mac: Chrome local-page element typing, window typing/shortcuts, pixel click/double-click,
scroll and drag; Safari pixel typing, semantic checkbox toggling and right-click; Telegram visual search
typing/clearing; and Obsidian quick-switcher search/dismissal, including a field beyond the first element
page. Native exact-window focus was verified on Chrome, Telegram, Obsidian and WeChat. Browser fixtures
were disposable local pages; no messages were sent or notes edited. WeChat search focus worked, but
after test input its window changed to a sign-in/security notice. The input result and cause of that
state change are unconfirmed; testing stopped there. Do not advertise complete WeChat automation.

This live evidence used the product adapter and native focus port in a local acceptance owner, not an
LLM-driven session or the signed installed Bot's TCC identity. Host/Guardian/skill fixtures independently
cover Runtime delivery and authorization. `experiments/desktop-control/browser.html` provides the
reusable no-network browser fixture; its README describes the manual acceptance sequence. Screen
captures and private App content stay in ignored local artifacts. Native Cua `foreground` dispatch
alone did not reliably activate inactive apps: use the explicit authorized `focus` step and fresh feedback.
Images over the public limit are losslessly optimized first, then encoded at the highest fitting JPEG
quality without changing Cua's returned pixel dimensions. Compression and malformed-image rejection
have deterministic Go coverage; readable PNG/JPEG output was inspected during the live browser runs.
Earlier v0.61.0 MiMo/Luna live results cover that historical baseline only. Subsequent changes require their
own regression evidence, and subsequent Core releases need the same external acceptance suite.

Native bubble/Glass/F1 theme and setup preview were inspected on a single Retina Apple Silicon Mac.
Minimum macOS 12 deployment target and Intel/mixed-DPI/multi-display behavior are not qualified by compilation.
Cua/packaged Node requires macOS 13.5+; ScreenCaptureKit selection requires 14+. Older systems return unsupported
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

## Documentation maintenance

Use the [documentation index](README.md). Keep current contracts, operator commands and explicit evidence
limits in the owner guide. Do not append dated handoff diaries, duplicate implementation plans or copy skills.
Historical decisions/acceptance remain in Git and linked PRs. When replacing a guide, update all repository
links and AGENTS instructions. Protocol/generated schemas, source tests and English Bot skills remain separate
sources of truth. A code-only regression fix that preserves the workflow needs no new skill instructions.
