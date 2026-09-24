# Bilingual UI migration handoff

Target: complete English and Simplified Chinese interface coverage before the next
formal Caelis Bot release. Start from the i18n foundation on top of `23c6373`; retain
both commits when integrating. Do not publish, tag, bump versions or modify the
user's daily application/data as part of a translation assignment.

## Common prompt for each external agent

> Work in the Caelis Bot repository. Read AGENTS.md, docs/product.md,
> docs/architecture.md and docs/i18n.md. You are not alone in the codebase: edit only
> your assigned files/catalog namespace and do not revert other agents' changes.
> Use the existing i18n framework to extract remaining application UI strings and
> provide natural, concise English and Simplified Chinese translations. Preserve
> behavior, IDs, commands, authority and user/generated content. Include labels,
> aria-labels, tooltips, placeholders, errors, empty/loading states, confirmations,
> dates and counts. Keep text readable at the minimum window size. Do not add
> explanatory copy, new controls or unrelated refactors. Do not classify errors by
> localized text. Do not edit generated protocol code or add translation libraries.
> Run npm run check:i18n, npm run check and relevant owning tests; include both
> language screenshots where your surface can be rendered. Commit your assignment
> and report its SHA, changed files, verification and any untranslated exceptions.
> Stop at the commit; the coordinating agent owns integration and release.

## Exclusive assignments

Each namespace below exists in both `en/` and `zh-CN/`. Keys are prefixed at use sites
(e.g. `chat.send`) by the namespace, not duplicated in its JSON entries. Reuse existing
`common` keys; request additions through the coordinator rather than editing it.

| Agent | Source ownership | Catalog ownership |
| --- | --- | --- |
| A — conversation | `frontend/src/{Panel,Bubble,WorkingMessage,AttachmentMenu,MessageContent,BotSetup}.tsx`, `chat-presentation.ts`, their relevant frontend tests | `chat.json` |
| B — settings | `frontend/src/{Settings,AppearanceSettings,ShortcutSettings,ExecutionSettings,Maintenance}.tsx`, relevant frontend tests | `settings.json` |
| C — runtime and models | `frontend/src/RuntimeSettings.tsx`, `settings/runtime/{RuntimeWorkspace,TeamSettings,ModelPicker,SettingsDialog}.tsx`, `settings/runtime/state.ts`, relevant tests | `runtime.json` |
| D — connections | `frontend/src/settings/runtime/ConnectionWizard.tsx`, relevant tests | `connections.json` |
| E — native presentation | `internal/desktop/` visible errors/dialogs/titles, `frontend/src/main.tsx` pet accessibility text, `resources/macos/` localization metadata, relevant tests | `native.json` |
| F — host presentation | `internal/{app,backend,bot,tasks,updates,caelisruntime}/` host-generated display text and notifications, relevant tests | `host.json` |

The coordinator owns `internal/i18n/`, `frontend/src/i18n/`, language bridge contracts,
`common.json`, catalog registration, package/build/check files and these docs. Native
agent E preserves `desktop/language.go` and the language broadcast plumbing; request
changes to those from the coordinator. Catalog directories belong to the assigned
agent only for its two JSON files; the coordinator retains framework code ownership.

Read helpers used by your files, but ask the coordinator to assign any newly found
shared helper instead of changing another agent's files. `script/runtime-settings.test.mjs`
is shared across C/D: give test changes to the coordinator or add a uniquely named
focused test and report how to run it. Do not edit developer previews as product
strings; the coordinator updates their fixtures when needed.

Native/host migration must introduce localized presentation at the producing owner.
Pass the existing live locale callback where needed, retaining English or original
third-party diagnostic evidence. Never replace Chinese substrings at the UI boundary,
translate already persisted conversation items, or thread language into provider
request/model configuration. When a display error also carries a semantic contract,
retain a stable code and localize only its user-facing explanation. Record ambiguous
cases for review instead of changing execution contracts to satisfy an inventory.

## Integration and release acceptance

Return one commit per assignment with a short list of intentional exclusions and
screenshots/commands actually run. The coordinator reviews and integrates them in
A–F order, resolves shared-file changes, audits residual literals and runs:

1. `npm run check:i18n`, `make check`, affected Go race tests, `make smoke`, `make build`.
2. Real app at English, Chinese and Follow System: startup, persisted choice, reopen,
   simultaneous chat/settings, tray/pet menus, existing draft and active work unchanged.
3. Both languages at minimum settings/chat size and light/dark appearance. Check
   connection/OAuth states, update/reconnect success and failure, Team editing,
   attachments, approvals, task terminal entry and silent scheduled turns.
4. Host notifications use the current language only for new application text;
   no replayed alerts, no translation of actual model/user content. System-owned
   dialogs/framework controls are documented separately from application-owned text.
5. Existing native acceptance and signed/notarized/Gatekeeper gates in
   [release.md](release.md), against the exact final merged source.

No release until those gates and residual-text review pass. The published baseline
is v0.1.0; the requested `v0.0.2` is below that. Resolve the next version with the
maintainer before the version PR, and keep release-please metadata consistent.
