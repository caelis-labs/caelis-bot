# Changelog

## [0.10.0](https://github.com/caelis-labs/caelis-bot/compare/v0.9.1...v0.10.0) (2026-10-07)


### Features

* add macOS start at login ([20ea35f](https://github.com/caelis-labs/caelis-bot/commit/20ea35f7192832ac650e8d5de729162f0369ff22))
* polish onboarding, chat, and settings with the Caelis theme ([9225b67](https://github.com/caelis-labs/caelis-bot/commit/9225b67d31b4b6d5d92cbf276c5111c3536bd859))


### Bug Fixes

* clear unsaved Telegram token drafts when settings closes ([9225b67](https://github.com/caelis-labs/caelis-bot/commit/9225b67d31b4b6d5d92cbf276c5111c3536bd859))
* keep Runtime choice and connection states authoritative ([9225b67](https://github.com/caelis-labs/caelis-bot/commit/9225b67d31b4b6d5d92cbf276c5111c3536bd859))
* preserve approval cards and settle accepted attachments ([#106](https://github.com/caelis-labs/caelis-bot/issues/106)) ([51ed983](https://github.com/caelis-labs/caelis-bot/commit/51ed983c829ea9c165f541d149de17443e4d1453))
* restore Telegram typing after approvals without blocking on independent Worker approvals ([20ea35f](https://github.com/caelis-labs/caelis-bot/commit/20ea35f7192832ac650e8d5de729162f0369ff22))

## [0.9.1](https://github.com/caelis-labs/caelis-bot/compare/v0.9.0...v0.9.1) (2026-10-06)


### Bug Fixes

* keep Bot online with local IM and recover isolated failures (v0.9.1) ([4384ba8](https://github.com/caelis-labs/caelis-bot/commit/4384ba85e0aa2adf8067141049acbce58a01337f)), closes [#96](https://github.com/caelis-labs/caelis-bot/issues/96) [#97](https://github.com/caelis-labs/caelis-bot/issues/97) [#98](https://github.com/caelis-labs/caelis-bot/issues/98) [#100](https://github.com/caelis-labs/caelis-bot/issues/100)

## [0.9.0](https://github.com/caelis-labs/caelis-bot/compare/v0.8.0...v0.9.0) (2026-10-06)

### Release highlights / 发布要点

* Telegram now offers a time-limited reconnect button to the paired owner when automatic recovery has stopped. It checks the original Runtime owner before reconnecting and never resends an uncertain request. Telegram also shows typing while the connected main conversation is working, then stops for approvals, completion, uncertainty or disconnect. / 自动恢复停止后，已配对用户可通过限时按钮核对并重连原 Runtime；结果未知的请求不会重发。主对话工作时 Telegram 显示输入状态，并在审批、结束、结果未知或断线时停止。 ([#91](https://github.com/caelis-labs/caelis-bot/issues/91), [#92](https://github.com/caelis-labs/caelis-bot/issues/92))
* This release also retires idle worker subscriptions, bounds and orders Codex events, limits automatic recovery to the original owner, and renders native approval choices as Telegram buttons. / 本版还回收空闲 Worker 订阅、限定并保序处理 Codex 事件、在原连接上有限自动恢复，并将原生审批选项显示为 Telegram 按钮。 ([#85](https://github.com/caelis-labs/caelis-bot/issues/85), [#86](https://github.com/caelis-labs/caelis-bot/issues/86), [#87](https://github.com/caelis-labs/caelis-bot/issues/87))
* The published desktop build remains macOS arm64. Windows groundwork does not imply a qualified Windows GUI release. Caelis Core #98 is a separate repository change and is not bundled here. / 正式桌面包仍为 macOS arm64；Windows 基础代码不代表已验收的 Windows GUI 发行版。Caelis Core #98 属于独立仓库，不随此版发行。


### Features

* prepare cross-platform desktop foundation and independent releases ([#84](https://github.com/caelis-labs/caelis-bot/issues/84)) ([c02f4f1](https://github.com/caelis-labs/caelis-bot/commit/c02f4f1b65bb1e4b8ae0391dcdd391b9bf938051))


### Bug Fixes

* recover Codex event bursts and render native Telegram approvals ([#93](https://github.com/caelis-labs/caelis-bot/issues/93)) ([4ac48c2](https://github.com/caelis-labs/caelis-bot/commit/4ac48c2199ae32c865bf287d4e7c5e44610f9e7b))
* retire idle worker subscriptions ([#86](https://github.com/caelis-labs/caelis-bot/issues/86)) ([#89](https://github.com/caelis-labs/caelis-bot/issues/89)) ([ab00843](https://github.com/caelis-labs/caelis-bot/commit/ab00843028e14bcb4e71ebb90fec3bea2b6c497c))

## [0.8.0](https://github.com/caelis-labs/caelis-bot/compare/v0.7.0...v0.8.0) (2026-10-06)


### Features

* show Runtime-native approval choices on the desktop and as actionable Telegram buttons; retain original receipts through competing decisions and reconnects ([#83](https://github.com/caelis-labs/caelis-bot/pull/83)) ([8fb5cdb](https://github.com/caelis-labs/caelis-bot/commit/8fb5cdbecf670da0d4c92fd41cbb5c467cad0aa1))


### Other Changes

* simplify messaging setup, make screen capture optional, and keep internal task notices out of chat ([#81](https://github.com/caelis-labs/caelis-bot/pull/81))
* pin the macOS Desktop World SDK and bundled helper to public v0.1.0-rc.3, with dynamic app authorization and cooperative desktop input ([#82](https://github.com/caelis-labs/caelis-bot/pull/82))


### Bug Fixes

* chat lifecycle, clipboard attachments and Telegram user identity ([#80](https://github.com/caelis-labs/caelis-bot/issues/80)) ([5a43015](https://github.com/caelis-labs/caelis-bot/commit/5a43015d5694b08a2d46d331164bb2a7a9a21768))
* sticker input, chat image previews and disconnect diagnostics ([#78](https://github.com/caelis-labs/caelis-bot/issues/78)) ([9fb2448](https://github.com/caelis-labs/caelis-bot/commit/9fb244837b598f33fcd2bb638702e6a06d1ac980))

## [0.7.0](https://github.com/caelis-labs/caelis-bot/compare/v0.6.0...v0.7.0) (2026-10-05)


### Features

* add Telegram companion chat ([#71](https://github.com/caelis-labs/caelis-bot/issues/71)) ([69a35fe](https://github.com/caelis-labs/caelis-bot/commit/69a35fe67e696dfb38edfa95baa7bc58be16b25a))

## [0.6.0](https://github.com/caelis-labs/caelis-bot/compare/v0.5.0...v0.6.0) (2026-10-03)


### Features

* add activity-linked chat portraits and polish desktop interactions ([#42](https://github.com/caelis-labs/caelis-bot/issues/42)) ([c96a774](https://github.com/caelis-labs/caelis-bot/commit/c96a774e0e18b2434b545bc893f09ebf82453d07))
* connect remote runtimes and refine chat and settings ([#67](https://github.com/caelis-labs/caelis-bot/issues/67)) ([0d2752e](https://github.com/caelis-labs/caelis-bot/commit/0d2752e848c571f45c7d767911bb05cb12e18cea))
* coordinate SSH Workers and Notebook backup from a local Mac Bot ([#63](https://github.com/caelis-labs/caelis-bot/issues/63)) ([5789b32](https://github.com/caelis-labs/caelis-bot/commit/5789b328f4923ffc7e3cfb957fd3994bb208c33b))
* **desktop:** migrate Bot to Desktop World and retire legacy Computer Use ([#46](https://github.com/caelis-labs/caelis-bot/issues/46)) ([a64202f](https://github.com/caelis-labs/caelis-bot/commit/a64202f3ad157284c233b07b375d8998a29bcb63))
* smooth chat streaming and make idle Dream context-aware ([#41](https://github.com/caelis-labs/caelis-bot/issues/41)) ([b028136](https://github.com/caelis-labs/caelis-bot/commit/b028136e18a3d5f1e876626dcf71710c2d5c2ff6))


### Bug Fixes

* allow the first authenticated SSH Codex Worker connection ([#64](https://github.com/caelis-labs/caelis-bot/issues/64)) ([d35a2f8](https://github.com/caelis-labs/caelis-bot/commit/d35a2f81605226889816315e65f7b89423f924d7))
* **codex:** preserve cleanup during reference cancellation ([#48](https://github.com/caelis-labs/caelis-bot/issues/48)) ([08e94c3](https://github.com/caelis-labs/caelis-bot/commit/08e94c37507552328910ebf877671e14355f4f6f))
* restore actionable Computer Use across macOS apps ([#39](https://github.com/caelis-labs/caelis-bot/issues/39)) ([a894aa9](https://github.com/caelis-labs/caelis-bot/commit/a894aa9846181e9f9b58c0eb934c03469e1b0c7a))


### Reverts

* pause multi-node work and restore 08e94c3 ([#66](https://github.com/caelis-labs/caelis-bot/issues/66)) ([fc4d6c4](https://github.com/caelis-labs/caelis-bot/commit/fc4d6c423fb3745289d1200bc9da867a44f53dc7))

## [0.5.0](https://github.com/caelis-labs/caelis-bot/compare/v0.4.0...v0.5.0) (2026-09-28)


### Features

* add idle Dream and deferred context handoff ([#32](https://github.com/caelis-labs/caelis-bot/issues/32)) ([28119db](https://github.com/caelis-labs/caelis-bot/commit/28119db68ffb429054930a1ba88e441a4dd480cf))
* add runtime-aware Computer Use and isolated Dev packaging ([#37](https://github.com/caelis-labs/caelis-bot/issues/37)) ([0274933](https://github.com/caelis-labs/caelis-bot/commit/0274933347d2ff452847f94ac03bfeceff2e7740))
* simplify settings and first-run onboarding ([#34](https://github.com/caelis-labs/caelis-bot/issues/34)) ([a95e031](https://github.com/caelis-labs/caelis-bot/commit/a95e0312ca61a9afa680b028983d053233c04f6d))


### Bug Fixes

* improve care, runtime setup and Guardian approvals ([#38](https://github.com/caelis-labs/caelis-bot/issues/38)) ([81d89ad](https://github.com/caelis-labs/caelis-bot/commit/81d89adde07670835cb2913d70050e651e37d7cd))

## [0.4.0](https://github.com/caelis-labs/caelis-bot/compare/v0.3.0...v0.4.0) (2026-09-27)


### Features

* add adaptive task cards and reusable terminal management ([#28](https://github.com/caelis-labs/caelis-bot/issues/28)) ([3674b95](https://github.com/caelis-labs/caelis-bot/commit/3674b9547620a006d7d3787faa2a9d54c174d025))
* add screen capture, annotations and contextual Ask Bot ([#30](https://github.com/caelis-labs/caelis-bot/issues/30)) ([ed06443](https://github.com/caelis-labs/caelis-bot/commit/ed06443d7474f24d6d0166380a56fd1b2cbf6c6d))
* refine screen input and retain screenshot previews ([#31](https://github.com/caelis-labs/caelis-bot/issues/31)) ([7141336](https://github.com/caelis-labs/caelis-bot/commit/7141336ec337360300d7d3273bce5f6e1fa30339))
* show live tool activity and expandable Markdown bubbles ([#29](https://github.com/caelis-labs/caelis-bot/issues/29)) ([f46d65a](https://github.com/caelis-labs/caelis-bot/commit/f46d65aaf0c741ffc4e582ea88b61f44513ec290))


### Bug Fixes

* deliver approvals and support task workspaces with automatic pins ([#25](https://github.com/caelis-labs/caelis-bot/issues/25)) ([4cf1c1a](https://github.com/caelis-labs/caelis-bot/commit/4cf1c1a136f9981258820dd5836663e67abd4e0d))

## [0.3.0](https://github.com/caelis-labs/caelis-bot/compare/v0.2.0...v0.3.0) (2026-09-25)


### Features

* add proactive care, task watchlists, and terminal preferences ([#23](https://github.com/caelis-labs/caelis-bot/issues/23)) ([c4de5cd](https://github.com/caelis-labs/caelis-bot/commit/c4de5cd614aa43e31e98c5794745d496f568c335))

## [0.2.0](https://github.com/caelis-labs/caelis-bot/compare/v0.1.0...v0.2.0) (2026-09-24)


### Features

* add bilingual UI and complete Caelis Runtime workflows ([#22](https://github.com/caelis-labs/caelis-bot/issues/22)) ([2121b2c](https://github.com/caelis-labs/caelis-bot/commit/2121b2cf628f8ebf22d9c4679499f0662d126655))
* add Sparkle updates and notarized R2 releases ([#17](https://github.com/caelis-labs/caelis-bot/issues/17)) ([360cf9c](https://github.com/caelis-labs/caelis-bot/commit/360cf9c1f7c0acf6570ee40d4547225b93fd5fb9))
* integrate shared Runtime settings and authored character props ([#20](https://github.com/caelis-labs/caelis-bot/issues/20)) ([5ac2c04](https://github.com/caelis-labs/caelis-bot/commit/5ac2c04a6cfa3938914383ed6cffe6fa920ec2fe))


### Bug Fixes

* **assets:** update character pack to 0.1.5 ([#21](https://github.com/caelis-labs/caelis-bot/issues/21)) ([69d1756](https://github.com/caelis-labs/caelis-bot/commit/69d17561fa3cd657f4771f23b2f7153962a6e655))
* improve backend reliability and macOS desktop usability ([#19](https://github.com/caelis-labs/caelis-bot/issues/19)) ([4e2c751](https://github.com/caelis-labs/caelis-bot/commit/4e2c7514cf4e42ab8961f37f7b9cf40357f2925f))

## [0.1.0](https://github.com/caelis-labs/caelis-bot/compare/v0.1.0-preview.2...v0.1.0) (2026-09-23)


### Features

* prepare Caelis Bot v0.1.0 with signed and notarized releases ([#10](https://github.com/caelis-labs/caelis-bot/issues/10)) ([7b769a8](https://github.com/caelis-labs/caelis-bot/commit/7b769a8ce464dcf826602becc6613c8b25f20625))

## [0.1.0-preview.2](https://github.com/caelis-labs/caelis-bot/compare/v0.1.0-preview.1...v0.1.0-preview.2) (2026-09-23)


### Features

* add persistent Bot memory, runtime setup and content packs ([#8](https://github.com/caelis-labs/caelis-bot/issues/8)) ([a816110](https://github.com/caelis-labs/caelis-bot/commit/a81611006c48ad2ee086b49d3e747aad199bdea9))

## [0.1.0-preview.1](https://github.com/caelis-labs/caelis-bot/compare/55e6178053938aebb484717a929efb337f3cedfe...v0.1.0-preview.1) (2026-09-22)


### Features

* Initial macOS Apple Silicon preview: a menu-bar assistant with a 3D character, local Codex conversations, attachments and approvals.

* automate protected macOS preview releases ([#2](https://github.com/caelis-labs/caelis-bot/issues/2)) ([b6c6915](https://github.com/caelis-labs/caelis-bot/commit/b6c6915af934ac23f3a55fd5d9dc168a4191ee93))
