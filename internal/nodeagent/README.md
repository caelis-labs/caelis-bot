# Optional foreground node agent

`cmd/caelis-agent` is a headless Go entry point. It imports no APP, desktop,
Wails or GUI composition and builds with `CGO_ENABLED=0` on Darwin and Linux
amd64/arm64. The default APP continues to discover local Runtimes in process;
this agent is optional and never a local startup prerequisite.

Create an explicit private user-owned directory, then run `serve-agent
--directory ABS`. It exposes `agent.sock` with mode 0600 beneath that 0700
directory. `--stdio` instead serves the same length-prefixed product frame
protocol over one foreground stdin/stdout stream. EOF detaches observation;
admitted installation intents reconcile their original receipt. No daemon,
autostart, account enrollment, Runtime activation or model task is created.

`Catalog`, `Configuration`, `Manage`, `Reconcile` and native `ReadRuntimeProof`
are closed typed endpoints. There is no shell/RPC reflection endpoint or secret
field. Node identity is locally persisted, and revisions and original request
digests are checked at mutation admission. Installation uses the existing
checksum-verified Linux reviewed-release installer. Unknown operations retain
only their original references for recovery; a later refresh never resends.

Codex configuration uses the standard App Server setup client for model/auth
metadata, without creating a conversation or running a turn. Target-owned
`execution.json` records separate `conversation` and `worker` defaults and
original operation receipts. An assembled owner must consume these preferences
before using them; saving defaults is not evidence of active Runtime renewal.
Caelis reads the existing masked native configuration projection and changes
semantic settings with the original operation ID. Host credentials stay in the
native adapter on that machine. Missing native configuration returns its exact
node/backend guard with `configurationAvailable:false`; Linux installation
availability and reviewed versions remain independent.

Before authenticating a new node-owned Caelis Store, run the explicit native
command `caelis-agent prepare-owned-caelis-store --directory ABS --node-id ID`
(or the same subcommand on `caelis-node`). It requires the existing private
agent directory and its exact persisted node identity. The default Store is
`caelis-store` beneath that directory; `--store ABS` selects a different absent
Store under an existing user-owned parent. Preparation creates only a private
Store, its exact node ownership marker and `.native-home`. It rejects every
existing Store, including unmarked, shared, foreign or previously prepared
Stores, and rejects redirected paths. A failed or repeated preparation never
repairs or adopts that directory.

The JSON receipt gives the nonsecret `store` and `nativeHome` locations. Perform
human Caelis authentication and configuration on this machine using that Store
and both `HOME` and `XDG_CONFIG_HOME` set to the returned native home, matching
the owned Host's native environment. Configure `serve-agent --caelis-store` to
that same Store. Preparation reads no model credentials, launches no Host and
establishes no authentication, health or lease authority. Catalog and ownership
probes remain read-only; live readiness is a separate explicit native action.

A version command runs with a fresh empty HOME. Native authentication/health
inspection returns only known states, never credentials, account identifiers or
configuration paths. Installed binaries and a healthy shared Caelis Host do not
prove a controllable Bot owner. Bot eligibility requires the assembled managed
owner and admission fence; `RuntimeOwner` proof injection is independent of
catalog configuration. Uncontrolled/shared owners fail the proof endpoint.
Windows Codex installation does not establish Bot process ownership.

For an explicitly approved Add action, `ProbeArchitecture` resolves Linux
amd64/arm64; `PrepareNodeDirectory` selects a fixed target-user HOME directory;
`InstallVerified` checks an APP-owned artifact's source revision, SHA256 and ELF
architecture before transfer and again checks checksum on the target. The APP
must supply a reviewed bundled manifest/path, not a remote URL or inferred
release. `NewSSHForegroundClient` owns only a temporary SSH foreground agent.
No remote bootstrap or deployment is performed by construction.

For a target with no inbound SSH, an existing foreground agent can run
`join-agent --socket ABS --ssh-target USER@HOST --join-helper ABS
--join-directory ABS`. The destination helper first verifies its directory is
0700 and owned by the SSH user. SSH reverse-forwards exactly one private Unix
socket, with strict known-host checking, no agent/X11 forwarding, mode 0600 and
no socket replacement. Native `ssh -G` connection resolution preserves the
original hostname/user/port, authentication references and known-host metadata
in a temporary private configuration while excluding ambient forwardings and
control/session side effects. Executable ProxyCommand/ProxyJump,
KnownHostsCommand and SetEnv hooks are explicitly unsupported. Tailscale
addresses are ordinary SSH destinations; no Tailscale login/account management
is added. Ending the join only detaches forwarding.

OpenSSH documents that `ClearAllForwardings` clears command-line mappings too,
and `-G` evaluates Host/Match configuration:
[ssh_config(5)](https://man.openbsd.org/ssh_config#ClearAllForwardings),
[ssh(1)](https://man.openbsd.org/ssh#G).

Verification uses semantic installation replay/digest conflicts, framed private
stream fixtures, unmanaged/shared proof refusal and SSH configuration fixtures.
`testdata/foreground.py BINARY [CODEX_BINARY CAELIS_BINARY]` is an opt-in actual
foreground socket and framed CLI proxy check with version-only native probes;
it submits no model turn, reads no model credential itself and makes no SSH
connection. Linux cross-compilation does not establish remote deployment or
native Linux model acceptance. This native setup/transport feature changes no
Bot-facing tool capability or workflow, so the Bot memory skill needs no update.
