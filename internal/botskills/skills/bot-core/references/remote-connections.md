# Remote connections

Load this guide when connecting or repairing a remote Worker environment.
The destination must already run Bot. A transport connection does not install
software, configure a Runtime, or authorize task execution.

## Choose the destination

Settings → Remote machines → Add machine offers two channels:

- **Address**: an IP address or hostname and the destination Bot service port
  (default 9317). An explicit HTTP(S) Bot address is also accepted.
- **SSH**: select an existing local OpenSSH Host, or enter a hostname/IP, SSH
  port (default 22), and username. Manual login supports a password or a local
  private key selected by the user. A key passphrase is requested when needed.
  The optional Bot port is separate from the SSH login port.

Use `bot_tasks` with `request.type="ssh_hosts"` for literal Host suggestions.
Existing Hosts retain system OpenSSH authentication, ssh-agent and jump-host
behavior. Suggestions do not execute SSH or prove reachability. Legacy private
settings remain compatible internally; Bot provides no SSH file manager.
Do not guide users through editing SSH configuration files as the app's setup
flow. Use the existing Host or manual connection controls.

Passwords and key passphrases belong only in the temporary SSH authentication
prompt. Never request them in chat, pass them through a task, or copy them to
notes, connection metadata or logs. The native key picker registers a local
reference; it does not import key bytes. Confirm host fingerprints with the user;
never disable host-key checking or delete known_hosts to bypass a mismatch.

## Authorize and recover

After SSH is ready, Bot sends the original pairing request to the destination.
The destination's user approves that A→B request. B→A is a separate grant.
Pending or unknown requests cannot be used as delegation environments. Read the
original receipt after uncertain delivery; do not create a replacement ID.
SSH success proves neither Bot authorization nor Worker readiness.

Editing saves local settings only. A changed address takes effect when the user
chooses Apply address and the original peer identity is verified. Check approval
continues to read the original address until that explicit change. Existing tasks
keep their original route. Deleting a connection permanently removes its local
settings, pairing proof and connection-only credentials. It does not contact B,
revoke B's independent grant, stop B's Workers, or delete shared SSH keys/config.
Existing tasks remain bound to the deleted ID and become unavailable; never
redirect them to a new same-name machine. Re-adding requires a new connection
identity and explicit authorization. Admitted network calls may finish; unused
SSH channels are reclaimed only after their last live/shared reference ends.
Incoming grants have their separate revoke action.

Old explicitly archived connections are upgraded to deletion locally. The v1
compatibility entry is also removed, so reopening cannot re-import it. Unrelated
legacy data and historical task routes are retained. This never grants authority.

After a transient SSH disconnect, approved connections retry automatically with
capped backoff, without an attempt limit. Wait for current environment health;
do not resend a task or pairing request, replace its ID, or repeatedly reconnect.
Successful transport recovery still checks the original grant and Runtime.
Explicitly stopped channels and authentication/host-key/configuration failures
need user action. Correct the selected host, login port, username or authentication
and explicitly Connect; never bypass host verification or renew revoked authority.
If SSH succeeds but Bot is unreachable, check that the destination Bot is running
and its service port is correct. Keep the local Bot available. Never substitute
a local Worker when the requested remote environment is unavailable.

If a local edit or removal did not reach the app, receipt reconciliation closes
that original operation before offering retry. A late delivery cannot apply it.
Do not bypass an uncertain operation with a new ID.

Terminal local mutation receipts compact to a bounded recent window. Retired IDs
are fenced by the original registry; an expired receipt is not proof of success
or failure. Review current settings before explicitly starting a new operation.

New connections omit a caller-chosen ID; a nonempty ID targets an existing
connection only. A deleted ID must never be recreated or replaced implicitly.
