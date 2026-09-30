This opt-in fixture exercises the production Linux `serve-bot` owner and
`proxy-product` client against an existing Codex CLI 0.159.2. It uses a generated
private profile, fresh HOME/CODEX_HOME, an allowlisted environment and a synthetic
loopback Responses provider with `requires_openai_auth=false`. It does not read
user authentication, install a runtime, forward ports or make paid model calls.

Prepare a new user-owned mode-0700 directory named
`/tmp/caelis-bot-issue47-codex-product-<16 lowercase hex characters>` on the target.
Copy a Linux `caelis-node` build there as `caelis-node`, and this helper as
`fixture.py`. Run `python3 fixture.py ROOT serve ABSOLUTE_EXISTING_CODEX_BINARY`
in the foreground under the existing strict SSH connection. Read its readiness
JSON, add the existing SSH destination and root as `target` and `root`, and save
that metadata in a private local file. The product authentication file is
generated on the target; its contents never belong in the local metadata.

Run:

```
CAELIS_BOT_CODEX_PRODUCT_SSH_FIXTURE=/absolute/private/metadata.json \
  GOWORK=off go test -race ./internal/productrpc \
  -run '^TestNativeLinuxCodexHeadlessProduct$' -count=1 -v
```

The test checks native initialization/thread/turn facts, a dropped accepted
product response and original receipt lookup, observer detach/reconnect, one
local Worker invoked through the native MCP namespace, and explicit owner stop.
The synthetic reviewer returns valid JSON only for this exact authorized Worker;
the production native approval policy remains unchanged. Native rollout facts
bind that MCP invocation to the resident thread and activating turn. Local
admission retains the existing local semantics; this fixture does not claim a
foreign Worker grant or cross-node route.

After the test, run `python3 fixture.py ROOT stop`. Wait for the foreground
supervisor to exit and inspect `fixture-stopped.json`. Remove only that root after
`owner_stopped` and `owned_children_reaped` are both true. Startup/test failure
must retain the owned process manifest until cleanup is confirmed. Native IDs
are represented only by hashes in the summary. Mac desktop rendering, paid model
entitlement and Linux-to-Linux SSH routing are separate acceptance gates.
