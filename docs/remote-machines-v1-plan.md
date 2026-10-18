# Remote machines

Connect an SSH machine when you want Bot to run a specific task there. The local application keeps the same Bot identity; the remote machine runs its own native Caelis or Codex and a headless helper. No Linux desktop app is required.

## Set up

1. Open **Settings → Remote machines**, enter the address, port and username, and choose password, private key or SSH agent authentication.
2. Confirm the host fingerprint. Configure the remote machine's own runtime login and default work model; accounts and credentials are not copied from the local machine.
3. Explicitly name the machine when asking Bot to work there. Without a target, new work stays local.
4. Use the existing task card to inspect progress or open the original native TUI in your preferred external terminal.

Unsupported remote OAuth/ACP or advanced setup should be completed in the remote TUI. Caelis Team is optional. No automatic machine selection, credential synchronization, Bot migration or file synchronization is implied.

## Ownership and recovery

- Changing a machine's default runtime/model affects new work only. Existing tasks retain their original machine, runtime, native ID and operation receipts.
- Closing an observer terminal does not stop its Worker. Focus/reuse/close require the original terminal identity; an unknown window is not replaced by launching another task.
- Restart or connection loss resumes observation of the original owner. Unknown submissions are reconciled, not resent under a new ID.
- A task's remote files are not automatically local downloads. Transfer and artifact aggregation follow the runtime's available resource contracts.

## Development

`script/build-remote-helper.sh` builds Linux amd64/arm64 helpers. The helpers are packaged by Bot, contain no model runtime, and run in a private remote directory. Native runtimes must already be installed and authenticated on that machine.

See [remote-machine verification](development.md#remote-machine-acceptance) for opt-in SSH tests. Real inference requires an explicitly configured disposable profile and can incur model charges. SSH protocol and lifecycle tests do not prove the visual contents of every external terminal window.
