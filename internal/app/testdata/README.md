# Native Codex Worker acceptance

The normal tests skip both opt-in gates. `TestNativeCodexPrimarySource` uses the
installed Codex App Server with an isolated `CODEX_HOME`, an actual Bot assembly
and native user submission, and a loopback-only synthetic Responses provider.
The source provider extracts the actual active native Thread/Turn; fixture code
never fabricates `WorkDispatchSource`. The same test binary serves the ordinary
private Bot MCP helper. Native IDs are hashed before diagnostic output.

`TestNativeCodexWorkerSSHGate` additionally requires an explicitly prepared
private harness configuration in `CAELIS_BOT_TEST_NATIVE_WORKER_GATE`. It runs
one real target task using the existing target login and **gpt-6-luna / medium**.
Do not enable it in CI or substitute a static source provider. The private file
contains `primaryBinary`, `ssh`, `directory`, and `nativeBinary`; its address and
paths are setup metadata, never task/model arguments. Prepare a fresh private
`/tmp/caelis-worker-gate-<16 hex digits>` with an exact built `node` helper and the
two Python files here. No login or token file is read or copied by these fixtures.

The foreground supervisor receives the actual persistent product Bot identity
and native origin pairing from the APP, writes only this dedicated target config,
and owns `serve-worker`. Its `proxy.py` observation helper drops one accepted
original start response, then reconnects to the same owner. The test verifies
canonical artifact bytes/hash, task ownership, changed intent/digest rejection,
original receipt reads after the primary native activation ends, and rejection
of fresh mutations after that activation. It never retries an unknown request
with a different ID. A same-pair `observe` supervisor invocation may restart the
owner and inspect the retained binding without dispatching a turn.

Explicit stop verifies the owned CLI's captured descendants using PID plus birth
identity across its `/proc/<pid>/task/*/children`; it does not scan the machine or
kill unrelated processes. Target-side evidence should return only approved
booleans, hashes, model and effort. Do not transfer raw private journals or native
responses. After stop, remove only the exact random fixture root. Preserve failed
logs separately from subsequent successful proof; never describe an initial
failed fixture as passing. This gate does not qualify manual approval, real
cancellation, GUI appearance, or automatic service startup.
