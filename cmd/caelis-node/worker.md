# Explicit Worker owner

`caelis-node serve-worker --config-file ABS_PRIVATE_JSON` starts only an existing
Codex Runtime's Worker connection. It creates no resident Bot, Notebook, Memory,
conversation thread or model turn on connection. This optional foreground entry
point does not change the default in-process local APP path and installs no
persistent supervisor or runtime.

The native target owner supplies this bounded version-1 nonsecret configuration:

```json
{
  "version": 1,
  "pair": {
    "Target": {"nodeId": "worker-node", "backend": "codex", "role": "worker"},
    "BotID": "bot-OPAQUE_PRODUCT_ID",
    "SourceNode": "local",
    "SourceBackend": "caelis"
  },
  "directory": "/home/user/private-node/worker",
  "binary": "/home/user/.local/bin/codex",
  "socket": "/home/user/private-node/worker/worker.sock",
  "execution": {"model": "gpt-6-luna", "effort": "medium", "serviceTier": ""}
}
```

The config is a regular file owned by the current target user with mode0600.
The canonical directory and its existing parent must be owned mode0700; it must
be dedicated to Worker state. Paths and the executable are explicitly configured,
never supplied by a model or wire request. Workspaces are allocated under
`directory/Tasks`. BotID uses the existing `productrpc.ProfileBotID` projection of
the actual persistent Bot identity; SourceNode/SourceBackend name its real native
activation source. The same pairing is durably bound before exposing the private
mode0600 socket. A different origin/target cannot reopen that journal. Existing
resident profiles, retained unpaired tasks and occupied sockets are rejected.
No login or credential file is read by node assembly; the standard native Codex
process uses its normal target-user setup. Credential preparation stays human.

Startup emits only version, fixed pairing and socket after successful secure
bind. Native request source checks and original receipts remain in the typed
Worker bridge. Observer loss never stops the native owner. SIGTERM/SIGINT ends
wire sessions and explicitly stops/reaps this owner's native process under a
bounded fresh cleanup context; unresolved cleanup remains an error.

`caelis-node proxy-worker --socket ABS_EXISTING_PRIVATE_SOCKET` is the target-local
strict-SSH helper. It accepts no pairing, source, runtime, credential or stop
options and never constructs a Worker. Closing its stdio detaches one observer.
Actual paid remote execution and originating Bot activation require their own
acceptance; the owned-process fixture exercises initialization, private pairing,
real proxy EOF/reconnect and exact signal shutdown without a model request.
Bot skill guidance is unchanged because these are native connection/lifecycle
entry points, not new model tools or authorization workflows.
