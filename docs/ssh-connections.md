# Remote Bot connections

Add machine supports two network channels. Address uses a reachable IP or hostname
and the Bot service port (9317 by default). SSH forwards to that Bot service through
system OpenSSH. Neither channel installs a remote Bot, chooses its Runtime or model,
nor grants permission to run work. The target Bot must separately approve the source.

## SSH login

Existing Host selects an already configured OpenSSH alias. The inert Host index
never executes configuration; explicit Connect delegates authentication, agent,
ProxyJump, IdentityFile and effective options to system OpenSSH. The alias's SSH
port comes from that configuration. The separately labelled Bot port is the remote
service port, not the SSH login port.

Manual connection accepts host, SSH port (22 by default), username, and password
or key authentication. It uses `-F none` with explicit options rather than applying
an unrelated Host wildcard, ProxyCommand or local command. Key selection uses the
native desktop picker; the app saves only an opaque reference and a protected local
path mapping. OpenSSH reads the selected file directly. Passwords, keyboard-interactive
answers and key passphrases pass through the existing transient askpass broker. They
are not saved in connection settings, receipts, activity records or logs. The Web
surface can use existing Hosts or password login; selecting a new local key requires
the desktop app. No browser upload of private key bytes is provided.

Fingerprint confirmation, changed-host-key rejection and authentication failure
remain explicit. Only Connect starts SSH; viewing Hosts, editing a saved connection,
refreshing the machine list and removing a local entry do not start authentication.
SSH failure does not replace local Bot or Worker state. The owned channel lifecycle
is supported on macOS/Linux. Windows keeps direct Bot connections; native SSH
convenience remains disabled.

## Existing configuration compatibility

There is no SSH file manager, file preview, reveal or external-editor API in the
app. `bot_tasks {request:{type:"ssh_hosts"}}` returns only known Host names and an
index issue, never file contents or paths. The retired `ssh_config` request is rejected.

Existing private configuration is retained internally, without migration or deletion.
For existing Hosts, precedence stays private Bot configuration, user OpenSSH
configuration, then system configuration; OpenSSH decides effective options.
Literal Host discovery is bounded to 256 aliases, 64 files, depth 16 and 128 KiB per
file. Dynamic system Includes still fail explicitly rather than being silently
reinterpreted. Special files and accidentally included private-key data are refused.
The private file is not used for manual login.

## Local management and operation results

Edit saves the local name and, when changed, a candidate endpoint. It does not
reconnect or change the active proof/channel. A changed endpoint appears as saved,
awaiting Connect; explicit connection then checks the original peer identity.
Remove archives the local list entry, preserving historical routes, original proof
and running channels. Revoking an incoming peer's permission is a separate action.

Legacy version-1 registry entries are displayed as legacy, not as unsent requests.
The original registry bytes remain untouched; local edits and archived state are
stored in a version-2 companion registry with the original profile/task bindings.
This does not restore the retired helper protocol or fabricate new peer grants.

Local save/remove assigns one operation ID, commits its receipt atomically with the
effect, and returns typed success/failure. The UI retains the ID before dispatch.
A lost response causes bounded readback of that original receipt, never another
mutation. An unreadable result remains unknown and fences subsequent writes until
original-state readback succeeds. A successful operation followed by a list refresh
failure remains successful; list availability is reported independently.

## Verification

`make test-ssh-auth` runs system OpenSSH against an isolated SSH password server
and an isolated native sshd with generated unencrypted/encrypted keys. It changes
no OS account passwords and uses only temporary known-hosts files. The test server
has its own Go module, so its SSH server dependency is not shipped in the product.
These are real SSH transport tests against controlled fixtures, not external Fedora
or live external Runtime acceptance. UI screenshots identify their fixture/native
source separately.
