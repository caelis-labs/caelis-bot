# Documentation

| Guide | Scope |
| --- | --- |
| [Install / 安装](install.md) · [中文](install.zh-CN.md) | Download, verification, first launch and updates |
| [Product](product.md) | User experience, persistent identity and direction |
| [Architecture](architecture.md) | Ownership, durable state, recovery, tasks, care and desktop contracts |
| [Availability](availability.md) | Local IM, component isolation, original-receipt recovery and fault verification |
| [Runtime integration](caelis-integration.md) | Codex/Caelis compatibility, environment, Guardian and Core release conditions |
| [Development](development.md) | Setup/signing, tests, native verification, localization and evidence limits |
| [Content packs](content-packs.md) | Creator format, official asset delivery, licenses and runtime responsibilities |
| [Release](release.md) | Protected release automation, credentials, recovery and public artifact acceptance |
| [Windows 11 handoff issue](windows-11-handoff.md) | Ordered native implementation tasks and exact acceptance gates after the Mac foundation PR |

These owner guides replace historical design plans, handoffs and repeated acceptance diaries. Earlier records
remain in Git (`git show 43314d5:docs/<old-path>`), with implementation history in the corresponding PRs.
Current contracts live here; executable schemas/tests and Bot-facing English skills keep their own source
locations. Do not treat a historical acceptance result or a planned capability as current release evidence.

Current implementation: [Remote machines v1 / 远端机器首版方案](remote-machines-v1-plan.md).
The [acceptance record](evidence/remote-machines-v1/acceptance.md) separates verified
native work and external-terminal lifecycle from the remaining terminal visual gate;
no shipped release is claimed.
