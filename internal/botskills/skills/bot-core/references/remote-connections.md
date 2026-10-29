# Connect a remote Worker environment

The destination must already run Bot. Use Settings → Remote machines → Add machine:

- Address: hostname/IP and Bot service port (default 9317), or explicit HTTP(S) URL.
- SSH: an existing local OpenSSH Host or manual hostname/IP, SSH port (default 22),
  username and user-selected password/key authentication. Bot port is separate.

`bot_tasks` `request.type:"ssh_hosts"` lists literal Host suggestions, not verified
connections. Existing Hosts retain system SSH configuration, agent and jump-host
behavior. Use those or manual controls; Bot does not manage SSH files, install
remote software or configure the destination's Runtime/account/model.

Passwords and passphrases belong in the temporary authentication prompt, never
chat, tasks, notes or logs. The key picker references a local key without importing
its bytes. Confirm host fingerprints; do not disable checking or delete known_hosts
to bypass a mismatch.

## Pair and recover

After transport connection, the destination's user approves the original A→B
pairing request. B→A needs a separate grant. Pending pairing is not an execution
environment. Check the original request later; do not poll for human approval or
recreate uncertain pairing. SSH success alone proves neither grant nor readiness.

Approved transient SSH failures recover automatically with capped backoff. Wait
for current environment health rather than resending work or pairing. Explicit
stop and authentication/host-key/configuration failures require correction and an
explicit Connect. If SSH works but Bot is unreachable, check destination service
and Bot port. Do not substitute local execution for requested remote work.

## Edit, delete or revoke

Editing saves local settings. A changed address applies only after Apply address
verifies the original peer; Check approval uses the original address until then.
Existing tasks keep their original route.

Deletion removes local settings, pairing proof and connection-only credentials;
it does not stop destination Workers, revoke its independent grant or delete
shared SSH keys/configuration. Historical tasks remain bound to the deleted ID
and unavailable, never redirected to a same-name machine. Re-adding requires a
new identity and authorization. Incoming grants have their own revoke action.

Reconcile unknown local edits/removals before another mutation. An expired receipt
proves neither success nor failure; inspect current settings before a new explicit
operation. New connections omit caller-chosen IDs; a nonempty ID targets an
existing connection only. Never recreate a deleted ID implicitly.
