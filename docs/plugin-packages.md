# Bot plugin packages and directory adapter

Bot loads [Agent Plugins 1.0 portable packages](https://agent-plugins.org/specification): root `plugin.json`, optional `skills/<name>/SKILL.md`, and optional root `mcp.json`. Package resources and executable CLI files can be carried in the verified inventory. Bot does not run lifecycle hooks or install scripts. A plugin contributes Skills and MCP servers only to the persistent Bot owner; Workers retain their own runtime configuration.

The production source remains the build-owned, SHA-256 reviewed catalog in `internal/plugins`. Adding a package requires a vetted inventory and a Bot release. Installation and activation are separate from reading a listing. The installer verifies every file, rejects unexpected paths and symlinks, and stages immutable package bytes before Runtime projection. On POSIX hosts, reviewed executable file modes are retained; nonexecutables cannot silently gain execute permission in the private installed copy.

`NormalizeMarketplace`/`OpenReviewedMarketplace` are an offline source adapter for the documented [Codex local marketplace `marketplace.json`](https://developers.openai.com/plugins/build/plugins#marketplace-metadata) form: `plugins[].name`, `source: {source: "local", path: "./..."}`, policy, and optional `interface.displayName`. The adapter reads portable packages from a supplied filesystem, verifies a separately approved hash inventory for every byte, then feeds the same Bot manager. Two different packages can be listed and installed without page-specific code. The index alone does not confer trust or install permission; `INSTALLED_BY_DEFAULT` is never automatically applied. Git and arbitrary URL sources need a separate pinned fetch, review, and distribution owner before they can be exposed in Settings. Bot does not register these packages in Codex's global or user marketplace.

| Surface | Current support |
| --- | --- |
| Portable package identity and publisher | Verified `plugin.json` `name`, `version`, description and `author`; optional reviewed display text. |
| Skills | Verified metadata in snapshots; bounded, sanitized body read only when details open. |
| MCP service directory | Service names from verified `mcp.json`. An enabled service is inspected only when its detail opens. |
| Codex tool directory | Private resident thread's public `mcpServerStatus/list`, narrowed by `serverName` and `toolsAndAuthOnly`; names, descriptions and present annotations are displayed. |
| Core v0.67.1 tool directory | Public `mcp-status` provides ready/failed state and tool names. Descriptions and annotations are omitted because that contract does not provide them. |
| Connection actions | No Bot-owned OAuth/credential proxy is shipped. A package can report unconfigured, authentication required or failed, but Settings does not offer an inoperative login action. |
| OpenAI hosted connector mappings | `.app.json`, registered connector IDs and OpenAI-hosted OAuth are outside portable package activation. They are not imported or represented as Bot connections. |

Tool annotations are server claims, not permission grants. Discovery never invokes `tools/call`; built-in automatic approvals remain scoped to the existing built-in services. The detail cache is bounded to 32 entries and 15 seconds, keyed by Bot catalog revision, package version, Runtime connection generation and service identity. Refresh bypasses it. Disabled or uninstalled services are answered locally without Runtime discovery.

Future authenticated remote MCP support needs a Bot-private connection record, provider registration where necessary, a local secret proxy or equivalent credential reference, and explicit connection lifecycle methods on both Runtime adapters. Token material must stay out of Core profiles, app-server shared configuration, snapshots and logs. The marketplace source adapter does not supply any of those capabilities.
