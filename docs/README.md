# Documentation

Start with the [English README](../README.md) or [中文 README](../README.zh-CN.md).
These documents describe durable contracts of the checked-out source.

| Document | Owns |
| --- | --- |
| [Product](product.md) | Product identity, surfaces, user interaction and platform boundaries |
| [Architecture](architecture.md) | Component ownership, runtime authority, persistence, tasks, channels and recovery |
| [Development](development.md) | Tool versions, build commands, isolated native development and verification boundaries |
| [Release](release.md) | Installation, required CI, signing, publication and update trust |
| [Content packs](content-packs.md) | Finished asset formats, validation, provenance, versioning and license boundaries |

Field definitions, dependency pins, protocol hashes and executable behavior stay with their owning schemas,
manifests and code. Bot-facing workflow guidance stays in [the packaged Bot skill](../internal/botskills/skills/bot-core/SKILL.md).
Design proposals belong in issues or pull requests; dated acceptance records belong with their original CI
runs and artifacts. Superseded plans and historical documents remain available in Git history.
