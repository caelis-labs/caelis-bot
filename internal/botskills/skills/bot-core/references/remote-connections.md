# Remote connections

Load this guide only when helping connect or repair a remote Worker environment.
A remote environment is another running Bot. SSH supplies a network channel, not
Worker execution, installation, account access, or authorization. Never reinstall
a helper or manage B's models or credentials.

## Find a connection

Use `bot_tasks` with `request.type="ssh_config"` to obtain the actual Bot SSH file
path, its revision, read-only user/system paths and literal Host suggestions. This
read does not execute SSH configuration. The file is the same one edited in
Settings → Remote machines → Add → SSH → SSH configuration. No second JSON model
of SSH settings exists. Suggestions omit patterns and dynamic Includes; an exact
Host alias may still be entered manually.

OpenSSH uses the first applicable value: Bot's private configuration first,
`~/.ssh/config` next, system configuration last (`/etc/ssh/ssh_config` on macOS
and Linux). `Host`, `Include`, `Match`, `ProxyJump`, `IdentityFile`, and ssh-agent
are interpreted by system SSH at connection time. Without an applicable `User`,
SSH uses the local OS username. The Bot UI's optional port is B's **Bot** port
(default 9317); SSH's port belongs in the standard Host configuration.

## Edit the shared file

Only modify Bot's private file when the user has authorized this configuration.
Use native file tools with their normal permissions, first reading the current
file. Write ordinary OpenSSH text, for example:

```sshconfig
Host fedora
    HostName 192.168.1.25
    User alice
    # ProxyJump bastion
    # IdentityFile ~/.ssh/id_ed25519
```

Keep specific entries above general defaults. Never copy private key bytes,
passwords, access tokens or a user's entire SSH directory into this file, a
Notebook note, chat, or tool output. Reference an existing key path or ssh-agent;
do not change user/system config files as a shortcut. If an external editor has
changed the file, reload and reconcile the new text before writing. The UI saves
atomically against the revision it read and refuses stale drafts.

The UI performs bounded syntax diagnostics without executing `Match exec`,
ProxyCommand or local commands. That is not proof that a live login, executable
condition, dynamic Include or jump is usable. Do not run `ssh -G` on an unreviewed
configuration as an automatic preview: `Match exec` can execute even with `-G`.
Executable SSH configuration is local code; actual connection requires the
user's requested connection scope. A direct native file edit is picked up on
refresh and the next new channel; it does not change a live SSH process.

## Connect and recover

Have the user select the Host and connect. Native SSH may ask for a password,
key passphrase, or confirmation of a host fingerprint. These go through the
local SSH prompt and remain transient. Never approve a fingerprint on the user's
behalf or set `StrictHostKeyChecking=no`. A changed key needs independent
verification; do not delete known_hosts entries to make the error disappear.

Once SSH is connected, A sends the ordinary Bot pairing request. B's user must
approve it in B's pairing requests. This grants A→B only; B→A requires a separate
request. Pending/unknown requests are not delegation environments. Read the
original request after uncertain delivery; do not create a new ID or substitute
another peer. SSH success proves neither Bot authorization nor Worker readiness.

If SSH fails, check the selected Host/configuration and the original prompt.
If SSH works but B is unreachable, verify B is running and its actual Bot port;
Bot does not install or upgrade the remote service. Explicitly reconnect after
a bounded retry budget is exhausted. Closing Settings does not close a live
channel. Removing/disconnecting closes only Bot's own SSH process, not another
terminal, shared SSH session, Worker, or B's application. Never fall back to a
local Worker when the selected remote environment is unavailable.
