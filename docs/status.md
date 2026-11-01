# Features and limits

This guide describes the current source tree. Package contents are fixed by each release tag; merging a change does not update an installed app.

## What is available

- One ongoing Bot conversation with streaming text, files, approvals and editable Markdown memory.
- Included Caelis runtime or a separately installed Codex; independent conversation and new-work model choices.
- Background tasks on the local machine or an explicitly approved Bot peer. Pending connections are excluded from delegation; tasks retain their original native owner.
- Custom characters and avatars, manual capture and clipboard images, Telegram, and an experimental Weixin private-text channel.
- Computer use through Caelis Runtime's fixed Desktop World capability; native app approval and OS consent remain required. Codex does not receive DTW from Bot.

## Platforms

| Platform | Distribution | Boundary |
| --- | --- | --- |
| macOS Apple Silicon | Developer ID signed and notarized DMG | Bot deployment target is macOS 12; Caelis computer use requires macOS 14.2. Minimum-version clean-machine behavior is not established by compilation. |
| Windows x64 | Unsigned Setup and portable ZIP; stable and preview channels | Windows 10 22H2 is the compatibility target; native GUI evidence is from Windows 11. See [remaining checks](windows-adaptation-issues.md). |
| Linux | Bot headless server, amd64 and arm64 | No Linux desktop application. |

Intel Mac and Windows ARM64 packages are not published by the current workflows. Windows Server with an interactive desktop requires separate verification.

## Desktop World

The formal Caelis Runtime v0.76.0 supplies Desktop World v0.2.3 and its paired
Skill. Bot owns no separate DTW archive/pin, installation, proxy or enable switch;
resident and native managed Workers use the Runtime's normal capability and app
approval paths. Codex does not load DTW through Bot. Legacy Bot plugin files and
independent Core installs are preserved.

Core's release delivery includes verified embedded packages and one real macOS
Agent task. Bot's adapter checks use a synthetic model and real native read-only
MCP execution, not live-account judgment. Neither source/CI nor those checks
establish Windows DTW GUI, clean-machine TCC or minimum-OS acceptance.

## Reading verification claims

Source tests, synthetic fixtures, native desktop interaction, real-account model behavior and published-artifact verification answer different questions. A successful build does not prove permission prompts or GUI behavior. Historical results do not qualify a new dependency or signed artifact; use [development](development.md) for repeatable checks and the release's own metadata for exact bytes.
