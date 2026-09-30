# Offline Bot memory transfer

`cmd/caelis-memory` exports a stopped profile into a private directory and imports
that directory into an **absent** target profile. It starts no Bot, model, scheduler
or SSH transport. Local app mode continues to open its normal profile directly.
This command moves durable memory and identity; native sessions, tasks, schedules,
Wake calls, provider connections and model/SSH credentials remain on the source.

Stop the source Bot and every direct Notebook writer, finish or reconcile pending
side effects and introductions, and disable automatic restart. Keep the source
stopped until the target is verified and selected for startup. The explicit flags
affirm these operational preconditions. Memory's owner lock additionally rejects a
live Memory store; it cannot fence an unrelated file tool or automatically enforce
single active Bot across machines. Export compares allowed source files before
and after Backup, but a write followed by a revert cannot be detected.

Build the standalone command with `GOWORK=off go build ./cmd/caelis-memory`. Example
paths below refer to isolated profiles, not the user's existing default profile:

```sh
./caelis-memory export \
  --source /absolute/stopped-profile \
  --bundle /absolute/private-parent/bundle \
  --source-stopped \
  --attachment assets/diagram.png

# Copy the completed private bundle with existing SSH/SFTP/scp/rsync tools.
# Preserve 0700 directory and 0600 file permissions. Keep an incomplete copy
# separate; never point a running Bot at a bundle or import staging directory.

./caelis-memory import \
  --bundle /absolute/private-parent/bundle \
  --destination /absolute/private-parent/new-profile \
  --source-stopped --destination-stopped
```

The destination parent must already exist. Existing destination profiles are
refused. The command returns the Bot ID, installed path and activation result;
on a later-stage failure it returns the retained inactive staging path and exits
unsuccessfully. Inspect or remove this staging manually after resolving the
failure. The completed bundle and original source remain available for rollback.
If a transfer lock remains after a crash, verify that no transfer is running before
removing that exact lock. Never start the source again until the target is stopped.

The versioned manifest contains each file's SHA-256 and size, Bot identity, its
derived scope, Memory schema, receipt/correction digests and forgetting diagnostics.
Missing, unexpected, altered, redirected, nonprivate or unsupported files fail
validation. These checks detect incomplete or damaged copying; the SSH connection
and local operator are responsible for authenticating the source bundle. Bundle
files contain private memory and should be handled as sensitive data.

The bundle contains:

- `bot.json`: Version, PersonalVersion and the existing stable ID, with empty
  schedules and no Wake state.
- `personal/index.json`: Version, BotID and the complete authoritative Receipts
  list. This index is required and is never rebuilt from the database.
- `personal/memory/memory.db`: a consistent snapshot from the public Memory
  Backup API, including corrections, tombstones, idempotency and topology.
- `Notebook/MEMORY.md` and all authored Markdown bodies. Generated `INDEX.md` is
  excluded and regenerated on the target.
- Explicitly allowed, referenced nonsecret attachments inside Notebook. Supported
  extensions are PNG, JPEG, GIF, WebP, PDF, TXT, CSV, MP3, MP4 and WAV. The operator
  must verify their content is nonsecret. Credential/token/secret filenames,
  hidden paths, symlinks, absolute local references and references escaping Notebook
  are refused. Ordinary Markdown inline links, reference definitions and quoted
  HTML `src`/`href` references are checked. Use URL-encoded paths for spaces and
  parentheses; other embedded/custom reference syntaxes require manual review
  and conversion to a supported local link before transfer.
- A version 1 Notebook migration marker, preserved or written after body validation
  to prevent retired notes/profile data being reimported.
- Only an actually accepted introduction marker, with its acceptance ID and no
  native Runtime binding. Missing/required introduction stays required. Pending,
  rejected, dispatching or unknown introduction must be resolved on the source.

No plaintext Memory management/steward tokens, issuer secrets, capabilities,
provider enrollment/model/SSH credentials, logs, caches, locks, sockets, WAL/SHM,
skills, native conversations or task/schedule state are copied. The target creates
its own local Memory credentials, closes its owner, calls public RestoreOwned and
CommitRestoreOwned, and opens the Bot store to rotate the issuer. Memory v0.6.1
rejects every public Open before restore commit, so commit occurs only inside new
inactive staging. Identity/scope, schema, receipts, correction chains, forgetting
diagnostics and normal Bot evidence reads are checked there before one directory
rename installs the new profile. A commit failure keeps the appliance rollback in
staging; a later product validation failure keeps the staging and source bundle.

There is a 512 MiB per-file and 1 GiB total bundle limit. Import checks available
space for the bundle, restore work and generated index. Filesystem errors leave
the target uninstalled. After a successful import, configure the target's own
Runtime/model login and start a new native session against the installed profile.
Cross-machine Mac → Linux → Mac acceptance remains an integration gate; local
public Memory round-trip tests and Linux builds do not prove SSH or remote login.
