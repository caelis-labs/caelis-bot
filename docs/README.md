# Documentation

Start with the [English README](../README.md) or [中文 README](../README.zh-CN.md).

## Use Caelis Bot

| Guide | What you will find |
| --- | --- |
| [macOS installation](install.md) · [中文](install.zh-CN.md) | Download, verify, connect a model and update |
| [Windows installation](install.windows.md) | Setup/ZIP, channels, first launch and updates |
| [Linux / WSL installation](install.linux.md) | Signed complete bundles, systemd user service, Web authentication and updates (first publication pending) |
| [Features and limits](status.md) | Current source, published packages and platform boundaries |
| [Content packs](content-packs.md) | Create and import characters, outfits and avatars |
| [Remote machines](remote-machines-v1-plan.md) | SSH setup, machine-specific models and task ownership |

## Build and maintain

| Guide | Scope |
| --- | --- |
| [Development](development.md) | Toolchain, isolated Dev apps and verification commands |
| [Product](product.md) | User behavior and future direction |
| [Architecture](architecture.md) | State, tasks, channels, tools and ownership |
| [Runtime integration](caelis-integration.md) | Caelis/Codex discovery, compatibility and model configuration |
| [Plugin packages](plugin-packages.md) | Reviewed packages, credentials and lifecycle |
| [Availability](availability.md) | Recovery without replaying uncertain work |
| [Interface design](ui-design.md) · [Polish principles](uiux-polish-principles.md) | Shared UI and native presentation |
| [Async questions](design/async-question-lifecycle.md) | Answer, expiry and original-request ownership |
| [Weixin channel](weixin-channel-poc.md) | Experimental private-text adapter and its limits |
| [Release](release.md) | CI routing, macOS signing, publication and recovery |
| [Windows release](windows-release.md) | Stable/preview packaging and update signing |
| [Windows scope](windows-adaptation-plan.md) · [Open validation gaps](windows-adaptation-issues.md) | Supported boundaries and remaining checks |

Current guides describe the checked-out source. A feature reaches users only in a published release containing that source. Historical acceptance diaries and superseded plans are kept in Git history, not maintained as current instructions. Protocol schemas, release pins, tests and Bot-facing Skills remain authoritative at their own paths.
