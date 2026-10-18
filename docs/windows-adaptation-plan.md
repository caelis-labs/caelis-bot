# Windows implementation scope

Windows shares Bot's product identity, application services, frontend, runtime adapters and durable receipts with macOS. Platform adapters own native effects; there is no separate long-lived Windows product branch.

## Current boundaries

| Area | Contract |
| --- | --- |
| Desktop | Native tray, independently movable character, bubble and on-demand chat/settings. Closing or hiding a window does not cancel work. |
| Runtime | Included Caelis, external Codex discovery, shared model configuration and original-owner recovery. Existing Workers survive default-runtime changes. |
| Storage and IPC | Current-user private ACLs, atomic state replacement, local authenticated IPC, explicit rejection of foreign owners and redirected paths. |
| Tasks | Original task/receipt bindings and typed terminal arguments; visible console ownership is checked before focus or close. Unknown ownership never falls back to window-title guessing. |
| Capture | User-triggered capture, annotation and clipboard images. Draft/window identity survives unknown outcomes; no automatic image resend. |
| Computer Use | Reviewed Desktop World Plugin with one `exec` tool, runtime approval, private worker lifetime and disable/exit fencing. |
| Distribution | Unsigned NSIS Setup and ZIP, independent stable/preview publication and optional signed updates. MSIX remains a development fixture. |

Windows 10 22H2 x64 is the compatibility target. Windows 11 is the native GUI validation environment. Server Desktop Experience, multiple monitors, DPI combinations and graphics-driver behavior require their own checks. Windows ARM64 and Linux GUI are outside this distribution scope.

## Development workflow

Use the pinned toolchain and `GOWORK=off`. Run `script/check-windows.ps1` for source/platform contracts and `script/smoke-windows.ps1` for the local Codex handshake and resources. Start native review through `script/build_and_run-windows.ps1 -DataDirectory <isolated-directory>`.

PRs select product checks from changed content. Installer construction belongs to the appropriate release workflow, not a duplicate preview matrix on every PR. When changing packaging, use local Setup/ZIP verification before publishing; see [Windows release](windows-release.md).

## Remaining work

Maintain unresolved interactions and coverage in [Windows validation gaps](windows-adaptation-issues.md). Record exact source and artifact identity for new findings, and distinguish deterministic contracts, native components, GUI interaction and real-account behavior. CI or a historical demonstration cannot establish all-platform parity.
