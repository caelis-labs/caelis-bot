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
keep their original route. Removing a connection hides its local list entry;
it does not revoke authorization or interrupt task-owned SSH channels. Unused
archived channels may be reclaimed when another connection starts. Incoming grants
are revoked through their separate permission action, using the original source.

Old connections without the current Bot authorization remain labeled as legacy.
Their original data and task routing are retained; editing or removing the list
entry never fabricates a new grant. Add a new connection for current delegation.

If SSH fails, correct the selected host, login port, username or authentication.
If SSH succeeds but Bot is unreachable, check that the destination Bot is running
and its service port is correct. Keep the local Bot available. Never substitute
a local Worker when the requested remote environment is unavailable.

If a local edit or removal did not reach the app, receipt reconciliation closes
that original operation before offering retry. A late delivery cannot apply it.
Do not bypass an uncertain operation with a new ID.
