# Releases and installation

See [development](development.md) for builds and [content packs](content-packs.md) for finished asset provenance.
Private modeling sources are not release dependencies.

## Required checks and branch policy

`main` requires a PR, the **product** check against the current base, resolved conversations and linear
history, including administrators. Force pushes and branch deletion are blocked. Only squash merges are
enabled; merged branches are deleted. The single-maintainer policy requires zero independent approvals. A tag
ruleset prevents updates/deletion of `v*` tags while allowing new tags.

Product CI selects jobs from the actual diff, not labels or authors:

| Change | Required jobs |
| --- | --- |
| README translations, docs or changelog only | Scope/public-tree guard and aggregate gate |
| Valid version-only metadata | Scope/metadata validation and aggregate gate |
| macOS-only source/scripts, optionally docs | macOS product |
| Windows-only source/scripts, optionally docs | Windows product |
| Shared code, dependencies, workflows or unknown paths | macOS, Windows and Linux headless |

Scope validates workflows/routing. Missing, failed, cancelled or unexpectedly skipped required jobs fail
`product`; dependencies/scripts cannot use the version-only path. The strict current-base gate checks combined
source before squash merge; main does not repeat it. PR/manual product checks do not publish packages.
macOS checks source/assets, Windows native shared/platform/Runtime contracts, and Linux ownership/recovery
race tests and both headless architectures. CI does not prove live model or GUI use.

Use Conventional Commit squash titles: `feat:` increments minor and `fix:` patch; docs/chore alone do not
trigger versions. Release-please maintains one root version PR. Merging it after exact-source required CI is
the release decision; no auto-merge. Its immutable `vX.Y.Z` tag/draft starts macOS publication. Windows/Linux
publish independently on that baseline. Cross-platform manual hardware acceptance is not a publishing
prerequisite; platform artifact checks and all macOS security gates remain mandatory.

Exact-tag build jobs use pinned toolchains/actions, read-only permissions and no signing credentials.
PR CI has no Apple/App secrets. Apple credentials stay in main-only `macos-release`; Windows/Linux update
keys stay in main-only `windows-release`/`linux-release`. Only publication gets ephemeral Contents write.
Release-please mints a scoped token with `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY`; asset PR automation
has its own configured copy. No private modeling credentials enter public builds.

## macOS

### Install and use

Published macOS packages are Apple Silicon (`arm64`). The app target is macOS 12.0; Caelis Computer Use
requires arm64/macOS 14.2. Intel packages are not published.

1. Compare [GitHub Latest](https://github.com/caelis-labs/caelis-bot/releases/latest) with the
   [macOS manifest](https://releases.caelis.dev/caelis-bot/latest.json); pause on mismatched tags/missing assets.
2. Download `.dmg` and same-named `.dmg.sha256`; run `shasum -a 256 -c EXACT-FILENAME.dmg.sha256`.
3. Quit the old app, open the DMG, copy **Caelis Bot.app** to Applications and open it normally.
   Retain `~/Library/Application Support/Caelis Bot` when replacing the app.
4. App and DMG carry Developer ID signatures and notarization staples. On verification failure, obtain a fresh
   official copy; do not remove quarantine, disable Gatekeeper or re-sign it.

Click the desktop character for chat or use menu-bar settings. Closing panels/hiding the pet leaves work
running; Quit ends the app. Choose bundled Caelis or compatible local Codex and configure your model/account.
Permissions are requested when needed; notifications/start-at-login are opt-in.

Settings → Updates controls daily/manual checks. Confirm installation; active/uncertain work postpones
relaunch until safe handoff. Dev builds have separate identity/data and no public updater.
Releases predating updater support need a manual upgrade.

### Signing and publication

Public releases require **Developer ID Application**, hardened runtime, secure timestamps, notarization,
valid app/DMG staples, mounted-DMG verification and independent Gatekeeper checks. Invalid identity/team,
missing credentials, rejected/pending/unknown notarization or verification failure stops publication.
No ad-hoc fallback; Apple Development/ad-hoc builds are not notarized releases.

Configure `APPLE_TEAM_ID` and these secrets in the protected `macos-release` environment:
`APPLE_DEVELOPER_ID_P12_BASE64`, `APPLE_DEVELOPER_ID_P12_PASSWORD`, `APPLE_NOTARY_APPLE_ID`,
`APPLE_NOTARY_PASSWORD`. Export only the selected encrypted Developer ID identity. Use a dedicated
app-specific notary password; never store an Apple primary password or two-factor code in CI. Credentials
enter only signing.

The build embeds tag/source SHA; signing verifies it. After security checks, pinned Sparkle signs the final
DMG/appcast. Ed25519-signed `latest.json` binds source/tag, DMG size/hash and appcast hash.
Keep `SPARKLE_PUBLIC_KEY` embedded and persistent `SPARKLE_PRIVATE_KEY` backed up; pass the private key
via stdin. Never generate a new update identity per build.

Publication reconciles exact GitHub names/bytes and the immutable platform receipt before publishing the
draft. R2 verifies signatures/source/receipt, uploads and reads back immutable `caelis-bot/releases/vX.Y.Z/`
objects, then updates the stable feed under `caelis-bot/feeds/macos/arm64/stable/` and installed-client
aliases `caelis-bot/appcast.xml`, `latest.json`, `latest.json.sig`. Only after this succeeds does GitHub
Latest move. Mutable feeds must retain `no-cache, max-age=0, must-revalidate`. Existing clients read the
appcast alias.

Reuse organization R2 secrets `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_ENDPOINT`, selected for required
repositories and scoped to `caelis-releases`. Organization storage alone does not enforce environment isolation;
workflows consume them in protected publication jobs. Keep secrets out of logs/artifacts and rotate trust deliberately.

### Recover publication

Never move tags or replace published bytes. Reconcile lost upload responses by original name/hash and reuse
original artifacts. Pending notarization retains signed bytes/submission IDs for 14 days. Inspect the original
run, query `notarization-status.yml`, verify its checkpoint and resume that completed run. Do not resubmit
known Apple work or infer failure from missing evidence.

```sh
gh workflow run release.yml --ref main -f tag=vX.Y.Z -f resume_run_id=COMPLETED_RELEASE_RUN_ID
gh workflow run sync-r2.yml --ref main -f tag=vX.Y.Z
```

Manual `release.yml` recovers an existing draft tag; it does not create a new release decision. R2-only
failures use `sync-r2.yml`, which revalidates the original assets/receipt. Recognized transient reads have
bounded retry; authentication, TLS, signature/digest failures and notary submissions do not enter that read
loop. Fix blocking public versions with higher SemVer. Exceptional owner-authorized feed rollback requires
verification/readback of the older signed feed, DMG, manifest and receipt; immutable bytes/tags stay intact.
Normal publication rejects rollback. Sparkle does not downgrade an already newer installation.

## Windows

### Install and update

Windows x64 targets Windows 10 22H2+ with WebView2; review is on Windows 11. No ARM64/Server Core claim.

1. Choose `windows-amd64-stable-Setup.exe` or `windows-amd64-stable.zip` and its `.sha256` from
   [GitHub Releases](https://github.com/caelis-labs/caelis-bot/releases). Latest may contain macOS only;
   use `vX.Y.Z-windows.preview.N` only for Preview testing.
2. Compare PowerShell `(Get-FileHash -Algorithm SHA256 -LiteralPath 'PACKAGE').Hash` with the checksum's
   first field; stop on mismatch.
3. Run Setup or extract the complete ZIP and launch `caelis-bot.exe`. Keep Runtime/resources/licenses together.
4. Packages are **unsigned**. If blocked, verify the source/contact your administrator; do not disable
   protections. Open the tray/character and configure Caelis or local Codex.

Stable, Preview and Dev have separate identities/data/update subscriptions; Stable does not migrate Preview
data. ZIP and Setup in the same channel share persistent data under `%LOCALAPPDATA%`; deleting ZIP or
uninstalling Setup retains it. Setup owns its registered notifications/protocol/shortcuts/startup entries; ZIP
does not register these. Foreign/legacy entries are not removed implicitly.

Use Settings → Updates only for releases with valid signed update metadata, or install newer official Setup
into the existing directory. Quit before manual ZIP replacement and retain user data. Setup remembers its
location; changing it needs uninstall/reinstall. Automatic update manifests and Authenticode are separate: a
signed update manifest does not make Setup/ZIP Authenticode-signed.

### Publish and recover

Main-only `windows-preview.yml` takes `vX.Y.Z-windows.preview.N` or `vX.Y.Z` at exact merged source and
matching base version. It checks/builds only that unsigned Setup/ZIP channel and verifies source/version,
clean build and artifact hash/length. Stable appends assets without changing Mac assets/Latest; Preview
creates a prerelease. MSIX is not a release path.

```sh
gh workflow run windows-preview.yml --ref main -f tag=vX.Y.Z-windows.preview.N
gh workflow run windows-preview.yml --ref main -f tag=vX.Y.Z
gh workflow run sync-r2.yml --ref main -f platform=windows -f tag=TAG
```

Updates are optional: `publish_updates=true` requires the existing `WINDOWS_UPDATE_PRIVATE_KEY` matching
`internal/windowsupdate/public-key.txt` in `windows-release`; failure cannot fall back to unsigned updates.
The signature binds source/tag/channel/platform and both artifacts' sizes/hashes. Back up this identity;
replacing it breaks old clients' trust. Reuse the original build artifact on retry and reconcile existing
assets; never overwrite different bytes.

R2 uses `caelis-bot/releases/windows-amd64/TAG/`; its download `latest.json` is a link/hash index, not update
authority. Retention follows Windows' latest successful baseline independently; stable supersedes previews of
that baseline. Readback failure retains the old baseline. Historical assets remain on GitHub.

Update handoff preserves original receipts, waits for the owner to close and commits before replacement. ZIP
replaces only owned files on the same volume with private staging/backup and rollback; NSIS failure may
require manual reinstall. Waiting/installing/missing receipts stay unknown rather than replaying effects. CLI
management uses a private stable host-workspace outside the program directory. An already shared Host may
retain its old directory until it exits; Bot exit and uninstall do not authorize killing that Host or its
active work.

## Linux headless

### Install and access

Linux amd64/arm64 and WSL use complete signed archives: Caelis, Web assets and launcher included; no Go/Node
needed. Require an ordinary user, Python 3, Ed25519 OpenSSL, bubblewrap/unprivileged namespaces and a running
systemd user manager, including WSL; no foreground installer fallback. Linux desktop, Web terminals and
Linux/WSL Computer Use are unavailable. No official Docker image or Runtime compatibility claim follows from
its source build.

```sh
curl -fsSL https://releases.caelis.dev/caelis-bot/install.sh | sh
# Upgrade/launcher repair; retain the profile:
curl -fsSL https://releases.caelis.dev/caelis-bot/install.sh | sh -s -- --upgrade
systemctl --user status caelisbot.service
systemctl --user stop caelisbot.service
systemctl --user start caelisbot.service
```

Save/inspect the bootstrap if preferred. It pins production trust and verifies the signed architecture index
and every file's hash/size before execution; checksums cannot replace signatures. No sudo, weakened sandbox,
linger or firewall changes. Foreign paths/units and incomplete installs are refused, not deleted.

Launcher: `~/.local/bin/caelisbot-launcher`; immutable installation: `$XDG_DATA_HOME/caelisbot-install`;
private profile: `$XDG_DATA_HOME/caelisbot` (default base `~/.local/share`). Open `http://127.0.0.1:9317`.
Direct loopback needs no pairing; exact Host/Origin and CSRF checks remain. Caelis is bundled; Codex uses
explicit Install/Sign in actions and native device authentication. Model URLs and paths refer to the service
machine, not the browser computer.

For remote/private proxies, configure exact `--origin` and `--require-pairing` on the upstream. The latter
explicitly disables direct-loopback trust when a forwarder makes the original peer indistinguishable. Example
unmanaged/development server:

```sh
caelisbot serve --require-pairing --listen 127.0.0.1:9317 \
  --origin https://your-machine.your-tailnet.ts.net
# Host-only SSH forwarding; use http://127.0.0.1:9317 as the allowed origin instead:
ssh -N -L 127.0.0.1:9317:127.0.0.1:9317 user@guest
```

Managed installations record these options at launcher `init`; do not launch a second managed-version process. Proxies must preserve allowed Host, SSE and unbuffered responses.
Private network membership is not browser authority; forwarding headers never grant local trust. Pairing uses
a private, one-use 10-minute code; `caelisbot pair --data-dir /same/private/profile` renews it. Do not place
codes in URLs/logs. Remote cookies expire after seven idle days and retain their original receipt ownership.
Tokens are profile-scoped; browser grants do not authorize the separately authenticated Bot-peer endpoint.
An optional `cmd/caelisbot-local-proxy` uses private proof for host-loopback access; never expose it remotely.

### Update ownership, publication and rollback

Settings → Updates checks/downloads signed offers and installs through the original idle handoff after
confirmation. `--upgrade` uses that handoff and replaces the launcher only after candidate health; a busy
owner may leave installation scheduled. External/manual directories are operator-managed; container images are
image-managed. Never share a live profile or create one service per version. A same-user launcher owns the
private root, profile lock, atomic selector and immutable version directories.

`linux-release.yml` builds both architectures from exact stable main-tag source without credentials.
`LINUX_RELEASE_ED25519_PRIVATE_KEY` in `linux-release` signs against `internal/managedupdate/release-key.txt`;
QA keys never authorize production. Signed manifests bind source/version/platform, launcher/state
compatibility, archive size/hash and every allowed member's path/mode/size/hash. Links, traversal, extra
members and expansion beyond declared limits fail before old-owner shutdown. Publication reconciles exact
GitHub bytes and mirrors architecture assets under `caelis-bot/releases/linux-<arch>/<tag>/`, reads them back,
moves signed download pointers, then publishes `install.sh`. Retention is architecture-scoped and cannot
delete another platform's assets. Reuse original signed bytes on recovery.

Accepted commands are not installation success: reconcile the original command receipt and read back the
expected signed identity and new boot. Preserve original operation IDs/receipts; do not delete unknown journals
or replay effects. Unknown effects, approvals and running work block unsafe handoff. Candidate health checks local state/listener/lock and applicable Runtime
handshake, not model authentication or task success. A failed candidate restarts the previous Bot once; failed
rollback stops as unknown. Code fallback never rolls back user data or downgrades a shared Caelis Host. Core
owns idle Host replacement; bundled and actually running Host versions remain distinct until authenticated
readback.