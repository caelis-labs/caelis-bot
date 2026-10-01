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
Local public Memory round-trip tests and Linux builds do not prove SSH or remote
login. The opt-in synthetic native fixture below covers a stopped source and a
fresh target; real account setup and paid-model behavior remain separate gates.

## Notebook roaming snapshots

The host-only `internal/memorytransfer` Notebook codec uses the separate
`caelis.bot-notebook.v1` format. It does not change the CLI or the complete cold
Memory bundle above. `ExportNotebook` returns canonical bounded bytes and a
`nodeplane.SnapshotRef`; `ValidateNotebookPayload` checks those bytes before they
enter the coordinator cache. The descriptor binds the stable Bot ID, publisher
epoch, positive decimal version and SHA-256 of the complete payload. Each sorted
file entry also contains its size and SHA-256. The snapshot is a complete file
set, so deletions are carried by absence rather than merging older generations.

This format contains sanitized `bot.json` identity with empty schedules, an
accepted introduction marker when present, the migration barrier, authored
Notebook Markdown and explicitly allowlisted referenced attachments. It excludes
generated `INDEX.md` and consume-once `HANDOFF.md`. No periodic snapshot converts
or revives a handoff. Pending introductions or wake effects must be reconciled
before export. Source Memory SQLite, mutation receipts, provider bindings,
authentication, tasks, native history and machine paths are not copied. Supported
local references follow the same Notebook attachment checks as the cold bundle.

`ApplyNotebook` stages a new **absent** profile generation with a target-owned
empty Memory database and an empty matching `personal/index.json`, then rebuilds
Notebook INDEX. The caller must hold the actual stopped/standby writer fence and
provide a commit callback that checks the coordinator's exact latest descriptor
under its CAS lock before invoking the atomic installation. Validation at the
start of staging alone is insufficient: a stale node must resynchronize the
latest full snapshot before becoming eligible. The lifecycle owner adopts the
new generation only after successful installation and creates its own native
conversation; old generations are preserved locally and never merged. Failed
staging or commit returns the retained inactive path for recovery.
The installed `notebook-snapshot.json` receipt binds the imported descriptor to
`bot.json`; `ReadInstalledNotebookRef` checks that binding. This receipt supplies
installation evidence only. A node still needs the latest broker descriptor and
an execution lease before admitting work.

Periodic payloads are limited to 16 MiB of canonical JSON, 10 MiB of decoded
file data, 8 MiB per file and 4,096 entries. Core MEMORY remains within the
128 KiB new-session context limit. Use clean canonical absolute local paths;
receiver paths, including existing ancestors, reject symlinks. These limits and
the stopped writer fence are independent of the larger offline bundle limits.
The codec starts no model, scheduler, transport or persistent service.

## Synthetic native acceptance

`internal/productrpc/testdata/memory_headless_fixture.py` extends the isolated
public Core headless fixture with **offline import before Bot startup**. It needs
`caelis`, `caelis-node`, `caelis-memory`, `memorycheck` and `inbound-bundle` in a
fresh private `/tmp/caelis-bot-issue47-product-<16 hex digits>` directory. All
HOME/XDG/Store data, loopback model configuration and credentials are generated
there. The fixture imports into an absent profile and checks public Memory,
correction receipts, forgotten tombstones, authored Notebook/attachment and
regenerated INDEX before creating any native execution binding.

Build the test-only public-API helper from the normal module:

```sh
GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -o memorycheck ./internal/productrpc/testdata/memorycheck
```

Prepare source metadata from the fixture's `fixture-ready.json`, adding its
existing strict SSH `target`, owned `root` and `Stage: "source"`. Run one native
synthetic turn, explicit Bot stop, stopped public Memory verification and fixture
Host shutdown with:

```sh
CAELIS_BOT_MEMORY_HEADLESS_FIXTURE=/private/source-metadata.json \
CAELIS_BOT_MEMORY_NATIVE_EVIDENCE=/private/source-evidence.json \
GOWORK=off go test -race ./internal/productrpc \
  -run '^TestNativeStoppedMemoryHeadless$' -count=1
```

Wait for the owned `fixture-stopped.json`, export that stopped profile with the
production memory CLI and its referenced attachment allowlist, then copy and
validate **only the bundle**. Keep the source stopped. Prepare a second absent
target profile through the same fixture with its own HOME/Store/credentials.
Target metadata uses `Stage: "target"`, `SourceBotID`, `SourceGeneration` and
`SourceNative` from the source evidence; run the same test against that metadata.

The target test requires the same logical identity and ProfileBotID projection,
a different native Session and service generation, no source command receipt or
conversation and no Worker bindings. It executes a fresh synthetic turn, stops
the target and rechecks the public Memory/Notebook facts. Native Session IDs and
credentials remain on the fixture host; evidence contains only hashes and
booleans. [Recorded synthetic evidence](evidence/issue47-memory-native/summary.json)
contains no SSH destination, profile path, credential or native Session ID.
