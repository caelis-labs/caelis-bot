# Features and limits

This guide describes the current source tree. Package contents are fixed by each release tag; merging a change does not update an installed app.

## What is available

- One ongoing Bot conversation with streaming text, files, approvals and editable Markdown memory.
- Included Caelis runtime or a separately installed Codex; independent conversation and new-work model choices.
- Background tasks on the local machine or an explicitly selected SSH machine. Existing tasks keep their original owner.
- Custom characters and avatars, manual capture and clipboard images, Telegram, and an experimental Weixin private-text channel.
- Optional Computer Use through Desktop World. Plugin selection, runtime approval and OS consent are separate decisions.

## Platforms

| Platform | Distribution | Boundary |
| --- | --- | --- |
| macOS Apple Silicon | Developer ID signed and notarized DMG | Bot deployment target is macOS 12; bundled Computer Use requires macOS 14. Minimum-version clean-machine behavior is not established by compilation. |
| Windows x64 | Unsigned Setup and portable ZIP; stable and preview channels | Windows 10 22H2 is the compatibility target; native GUI evidence is from Windows 11. See [remaining checks](windows-adaptation-issues.md). |
| Linux | Remote/headless helpers, amd64 and arm64 | No Linux desktop application. |

Intel Mac and Windows ARM64 packages are not published by the current workflows. Windows Server with an interactive desktop requires separate verification.

## Desktop World

The source pins [Desktop World v0.1.0](https://github.com/caelis-labs/desktop-world/releases/tag/v0.1.0) using a dedicated slim Plugin archive. The [pin](../resources/desktop-world/release.json) owns the archive, runtime, worker, BUILD and licensing digests. Bot v0.15.2 predates this dependency change; inspect the pin at the installed Bot tag when comparing versions.

The v0.1.0 release includes `ACCEPTANCE.json` and checksums. Its macOS native checks ran on macOS 27.0.1; upstream CI used macOS 26. Clean macOS 14 first-launch permissions remain unverified and were explicitly excluded from the initial release scope. Bot's own CI uses `macos-15`; neither CI runner substitutes for that clean-machine test.

Bot's local Core/Codex integration checks cover Plugin discovery, the sole `exec` tool, disable revocation and isolated native operation. Synthetic-model checks are not real-account model acceptance. Codex's inspection API exposes PNG image envelopes without image bytes; actual PNG payloads require a separate Core/MCP check.

Windows upstream DPI scaling, modal-provider stalls and errors after an input may have taken effect are tracked in [Desktop World #36](https://github.com/caelis-labs/desktop-world/issues/36). They do not block the v0.1.0 release and do not imply verified Windows GUI parity. Elevated applications, secure desktop and mixed-DPI input are not advertised as verified. Uncertain input must be reconciled using its original receipt, never blindly repeated.

## Reading verification claims

Source tests, synthetic fixtures, native desktop interaction, real-account model behavior and published-artifact verification answer different questions. A successful build does not prove permission prompts or GUI behavior. Historical results do not qualify a new dependency or signed artifact; use [development](development.md) for repeatable checks and the release's own metadata for exact bytes.
