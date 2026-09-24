# Development and releases

## Ownership

`caelis-bot` owns the application, finished character packs and macOS distribution. The private `caelis-bot-assets` repository owns modeling sources and opens product asset PRs. A product build uses checked-in finished files only; no private checkout or Blender is involved.

## Required checks and branch policy

`main` requires a pull request, the **product** GitHub Actions check against the current base, resolved conversations and linear history. Administrators are included; force pushes and deletion are blocked. The current single-maintainer setup requires zero independent approvals, so a sole maintainer can merge after checks. Only squash merges are enabled; merged branches are deleted automatically. A tag ruleset prevents updates/deletion of `v*` tags while allowing new release tags.

Product CI runs on native Apple Silicon (`macos-14`) for every PR and main push:

- Pinned actionlint validates workflow syntax and expressions.
- `npm ci` uses the lockfile; Node and Go come from repository pins.
- `make check`: public/private boundary, finished asset hashes, TypeScript/build, focused frontend contracts, Go tests/vet and shared-core portability guards.
- `npm run smoke:assets`: validate, parse and animate the delivered GLBs.
- `make package`: compile and ad-hoc sign the app; build a compressed read-only DMG; mount it and verify the enclosed signature, executable, license and Finder layout. The installer uses a 660×430 window, 128-point icons and a left-to-right Applications drag target. Pinned dmgbuild dependencies are isolated in `.cache/dmg-tools` (Python 3 required); no Finder automation is used.
- Keep the DMG and checksum as a seven-day CI artifact. These are development builds, not published releases.

`make smoke` additionally checks a locally installed Codex runtime without a model call. CI does not log into Codex or exercise a real conversation. Packaging is not interactive desktop acceptance; see [native acceptance](native-acceptance.md) and [backend acceptance](backend-acceptance.md). Intel and Windows GUI releases are not currently qualified.

Dependabot checks Actions, npm and Go dependencies weekly. Dependency PRs use the same required check and are not auto-merged. Update `toolchain.json` when changing the corresponding explicitly pinned toolchain; review Wails/Codex compatibility separately.

## Release flow

Use Conventional Commit PR titles, for example `feat: add reminders` or `fix: keep approvals visible`. The squash title becomes the commit title. `feat` and `fix` trigger versioning; `chore` and `docs` alone do not create a product release.

1. A main push runs `release-please`. It maintains one version PR, updating `package.json`, `package-lock.json`, `CHANGELOG.md` and `.release-please-manifest.json`.
2. Review and merge that PR after its native **product** check. Version PRs are not auto-merged.
3. release-please creates a `vX.Y.Z` tag and **draft** GitHub release. The reusable **Release DMG** workflow checks that the tag belongs to main and exactly matches `package.json`.
4. Re-run checks and build from that exact tagged commit without signing credentials. The application embeds its full release version and source SHA; its macOS short version stays numeric. A separate `macos-release` environment job uses the current workflow commit for operational signing/packaging tools, verifies the downloaded app’s embedded tag SHA, and signs the app with Developer ID and a secure timestamp, enables hardened runtime, submits it to Apple and staples its notarization ticket.
5. Package the stapled app, mount the DMG and verify the enclosed app. Sign the DMG itself, submit it to Apple, staple its ticket, verify both its signature and Gatekeeper acceptance, then calculate the final SHA-256. Both the app and DMG carry tickets for offline validation.
6. Upload `Caelis-Bot-X.Y.Z-macos-arm64.dmg` and `.dmg.sha256`. Only after all signature, notarization, Gatekeeper and checksum gates succeed is the draft published. Stable tags become GitHub's latest release; tags containing a prerelease suffix stay prereleases.

A failed build remains a draft. A notarization submission still `In Progress` is reported as a successful **pending checkpoint**, with `verified=false` and publication skipped. It is not reported as an artifact rejection. Published release assets are never overwritten by the workflow. To start or rebuild a failed draft from the same tag, copy:

```sh
gh workflow run release.yml --repo caelis-labs/caelis-bot --ref main -f tag=v0.1.0
```

Substitute the failed draft's exact tag. The recovery workflow rejects a tag outside main or a version mismatch. The final publication job, which alone has Contents write permission, verifies that the release is still a draft before uploading anything; read-only build tokens cannot see unpublished GitHub release drafts. It can replace incomplete assets **in a draft**. The app retains the tagged source even if main has moved. Recovery can apply a reviewed fix to signing/packaging tools from the immutable workflow commit without rebuilding app code from main or moving the release tag. Packaging checks the app version against the requested tag; the credential-free build job checks that tag against its own package.json.

When a run retained a `notarization-checkpoint`, resume its exact signed artifacts and Apple submission IDs instead of signing or uploading them again:

```sh
gh workflow run release.yml --repo caelis-labs/caelis-bot --ref main -f tag=v0.1.0 -f resume_run_id=COMPLETED_RELEASE_RUN_ID
```

The source run must be a completed main-branch Release DMG or release-please run in this repository, from a commit in main's history. Recovery verifies the checkpoint's tag, source SHA and team; the native signature; the submitted bytes' SHA-256; and the digest in Apple's acceptance log. The tagged build check still runs, but its new unsigned app is replaced by the retained signed app. Existing App/DMG submissions are queried and, if necessary, waited on; they are not resubmitted. Stapling modifies only copies of the submitted archives. A missing/expired checkpoint cannot reconstruct the old signed bytes from a submission ID alone; inspect that submission's result before starting a fresh build.

For the v0.1.0 promotion, set `prerelease` to `false` while retaining the prerelease versioning strategy; it promotes the current preview to its stable version. Keep version/package/changelog changes in the release-please version PR. Do not hand-edit published tags or bump the manifest independently.

## Automatic updates and R2

Updater-enabled stable releases embed **Sparkle 2.10.0**, downloaded from its official
release and verified against the SHA-256 pinned in `script/sparkle.sh`. The framework,
Autoupdate, Updater.app and both XPC helpers are signed inside-out with the app's
Developer ID before notarization. Local builds sign them ad-hoc and disable updating.
Sparkle's license is included in the app. `CFBundleVersion` follows the numeric stable
version; the older constant `1` was never used by an enabled updater.

The signed app uses `https://releases.caelis.dev/caelis-bot/appcast.xml`, requires a
signed feed and validates archives before extraction. It checks daily by default;
settings can disable checks. Downloads/installations require confirmation. Active,
uncertain or delegated work postpones relaunch; user and scheduled admission are
fenced before backend cleanup. Previews remain manual and never replace the stable
feed. Existing v0.1.0 installations need one manual upgrade to the first updater-enabled
release. Already published apps are never modified.

Configuration reuses Caelis core's bucket, endpoint and credential names. Store these
in the **caelis-labs organization**, with access limited to the selected repositories
below. Secrets from a sibling repository are not inherited:

| Name | Location | Purpose |
| --- | --- | --- |
| `SPARKLE_PUBLIC_KEY` | Organization variable; `caelis-bot` only | Persistent Ed25519 public key embedded by the credential-free build job |
| `SPARKLE_PRIVATE_KEY` | Organization secret; `caelis-bot` only | Exported Sparkle private seed, passed to signing tools through stdin |
| `R2_ACCESS_KEY_ID` | Organization secret; `caelis`, `caelis-bot` | R2 object read/write access limited to `caelis-releases` |
| `R2_SECRET_ACCESS_KEY` | Organization secret; `caelis`, `caelis-bot` | Corresponding R2 secret |
| `R2_ENDPOINT` | Organization secret; `caelis`, `caelis-bot` | Existing HTTPS S3 endpoint |

Generate the persistent signing identity **once** and retain its Keychain backup.
Do not create a key per CI run. This one-time setup keeps the private value out of
command arguments, terminal output and the public tree:

```sh
/bin/bash <<'CONFIGURE'
set -euo pipefail
source script/env.sh
source script/sparkle.sh
key_dir=$(mktemp -d)
trap 'rm -rf "$key_dir" "$BOT_SPARKLE_DIR"' EXIT
umask 077
"$BOT_SPARKLE_DIR/bin/generate_keys" --account caelis-bot
"$BOT_SPARKLE_DIR/bin/generate_keys" --account caelis-bot -p > "$key_dir/public"
"$BOT_SPARKLE_DIR/bin/generate_keys" --account caelis-bot -x "$key_dir/private"
gh variable set SPARKLE_PUBLIC_KEY --org caelis-labs --visibility selected --repos caelis-bot --body "$(cat "$key_dir/public")"
gh secret set SPARKLE_PRIVATE_KEY --org caelis-labs --visibility selected --repos caelis-bot < "$key_dir/private"
CONFIGURE
```

Set the three R2 organization secrets through GitHub Settings or `gh secret set NAME
--org caelis-labs --visibility selected --repos caelis,caelis-bot` using its hidden
prompt. Organization administration with GitHub CLI requires `admin:org`; obtain
that permission explicitly before setup. Repository/environment secrets with the same
names override organization secrets; remove obsolete overrides only after verifying
the organization configuration. Release workflows consume these secrets only in their
main-only `macos-release` jobs, but organization storage itself does not restrict them
to an environment. Keep Apple credentials in that protected environment.
The bucket is `caelis-releases`; its public domain
must serve `caelis-bot/*` without overriding mutable objects' `no-cache, max-age=0,
must-revalidate` headers. No new bucket or Worker is required.

On 2026-09-24, the organization configuration above was installed and its selected
repository access verified. Sparkle's persistent identity uses the local Keychain
account `caelis-bot`; its key pair and organization public key were checked together.
The account token `caelis-org-release-publisher-20260924-r2` grants only object
read/write on `caelis-releases` and expires on **2027-08-24**. Renew it before that date
and update both R2 credential secrets together. Core's obsolete repository overrides
were removed; its earlier Cloudflare token was not revoked. This setup verification
does not establish a successful production upload or app update.

After `verified=true` (App + DMG notarization, staples and Gatekeeper), the package job
uses `generate_appcast` with one version and no deltas. It signs the final stapled DMG,
feed and an independent manifest binding tag, source SHA, DMG hash/size and feed hash.
These files are attached to GitHub. Only after publication can the R2 job run, with
read-only GitHub access and R2 credentials scoped to that step.

The publisher verifies signatures/source, checks GitHub latest, uploads under
`caelis-bot/releases/vX.Y.Z/`, and downloads each object to verify its full SHA-256.
It rechecks GitHub latest, switches `caelis-bot/appcast.xml` and `latest.json`/signature,
then removes older objects strictly within the Bot release prefix. It never deletes
core's `releases/` or `latest.txt`. Publishers share one CI concurrency group, refuse
rollback, preflight every deletion key, and refuse changed immutable bytes. Failures
preserve old artifacts; a failure after switching the feed can temporarily leave both
versions until retry. Clients with a cached older feed may need to check again after
pruning. GitHub release history remains available; R2 keeps only the latest version.

If only R2 failed, do **not** rerun notarization or republish a non-draft. This dedicated
retry downloads and verifies the latest release's signed artifacts:

```sh
gh workflow run sync-r2.yml --repo caelis-labs/caelis-bot --ref main
```

`make check` covers publication ordering, signatures, tamper rejection, rollback and
prefix ownership with fixture storage. `make check-updater` uses the pinned framework,
an isolated AppKit host, disposable keys and an ad-hoc DMG to test the native API,
postponed/cancelled installation and real feed signing. Neither substitutes for a
two-version Developer ID installation/relaunch test from public R2. Run that after
credentials and the first updater-enabled releases exist; verify preserved data and
active-work deferral. Integration follows [Sparkle's official documentation](https://sparkle-project.org/documentation/).

## Credentials and least privilege

The existing **Caelis Character Publisher** GitHub App is installed only on `caelis-bot`, with Contents and Pull requests write permissions. Public Actions uses:

- repository variable `RELEASE_APP_ID`;
- encrypted repository secret `RELEASE_APP_PRIVATE_KEY`.

The private asset publisher holds its separately configured copy for asset PRs. Only the main-branch version job receives the public secret and mints a short-lived repository-scoped App token. This lets release PRs trigger normal CI without allowing `GITHUB_TOKEN` to create/approve PRs. No personal access token, admin permission, private modeling credential or deploy key is needed. Rotate the App key in both repositories when rotating this shared automation identity.

PR CI has read-only permissions and no App or Apple secrets. Both the build and signing/packaging jobs have read-only repository permissions; only the final publication job gets Contents write via its ephemeral `GITHUB_TOKEN`. Apple secrets are scoped to the `macos-release` environment, restricted to the `main` branch, and injected only into the signing step. Actions are pinned by commit SHA.

## Developer ID signing and Apple notarization

Public releases always require Developer ID Application signing and Apple notarization. There is no feature switch or ad-hoc fallback. An expired/wrong-type certificate, team mismatch, missing credentials/timestamp/hardened runtime, rejected or pending notarization, invalid staple, or Gatekeeper rejection stops publication. Pending Apple processing preserves a resumable checkpoint; rejection, unknown status, credential and transport errors fail the job. Existing preview releases remain historical, unnotarized artifacts.

The `macos-release` GitHub environment is restricted to `main`. Configure:

| Name | Type | Value |
| --- | --- | --- |
| `APPLE_TEAM_ID` | Environment variable | The 10-character Apple team ID |
| `APPLE_DEVELOPER_ID_P12_BASE64` | Environment secret | Base64 of one password-protected Developer ID Application identity, including its private key |
| `APPLE_DEVELOPER_ID_P12_PASSWORD` | Environment secret | PKCS12 export password |
| `APPLE_NOTARY_APPLE_ID` | Environment secret | Developer's Apple Account email |
| `APPLE_NOTARY_PASSWORD` | Environment secret | A dedicated app-specific password for notarization |

A paid individual Apple Developer membership supports this distribution. App Store publication and App Store Connect API access are not needed. Generate an app-specific password at [Apple Account](https://account.apple.com/) under Sign-In and Security. The certificate and private key perform signing; the separate account credentials authorize Apple notarization. Revoke the dedicated password when retiring the workflow and rotate its environment secret after replacement. No Apple Account primary password or two-factor code belongs in CI.

Export the selected Developer ID identity as an encrypted `.p12`. Do not export unrelated keychain identities. The following commands read secret values from a file or hidden prompt; never put private values in command arguments, chat, commits or artifacts:

```sh
base64 -i '/absolute/path/DeveloperIDApplication.p12' | gh secret set APPLE_DEVELOPER_ID_P12_BASE64 --repo caelis-labs/caelis-bot --env macos-release
gh secret set APPLE_DEVELOPER_ID_P12_PASSWORD --repo caelis-labs/caelis-bot --env macos-release
gh secret set APPLE_NOTARY_APPLE_ID --repo caelis-labs/caelis-bot --env macos-release
gh secret set APPLE_NOTARY_PASSWORD --repo caelis-labs/caelis-bot --env macos-release
gh variable set APPLE_TEAM_ID --repo caelis-labs/caelis-bot --env macos-release --body 'YOURTEAMID'
```

The signing job downloads only the app built by its own preceding job; it does not install npm/Go dependencies or compile with certificate access. `script/sign-release.sh` imports the identity into a temporary keychain with a random password and codesign-only key access. It verifies the pinned public Apple G2 intermediate, stores validated notary credentials in the same temporary keychain, and removes all credentials on exit. An always-run workflow cleanup also handles cancellation. It temporarily adds only that keychain to the user search list, preserving existing entries; deletion removes the temporary entry. It never changes the default keychain or system trust policy. The hosted runner is discarded after the job.

`script/verify-signature.sh` checks Apple trust, the Developer ID Application certificate OID, team ID, artifact identifier, secure timestamp and (for the app) hardened runtime. Sparkle's framework, Autoupdate executable, Updater.app and XPC helpers are signed explicitly before the enclosing app. The app needs no hardened-runtime exceptions: WebKit runs out of process. New nested executable code requires an explicit signing plan; `--deep` is used for verification, never signing.

Notarization saves the upload receipt before waiting up to **60 minutes per submission**. The signing/packaging job allows **150 minutes** for the App and DMG waits plus packaging, validation and checkpoint upload; its temporary keychain remains unlocked for that bounded job lifetime. After waiting, the same submission is queried again: `Accepted` continues even if the wait command timed out, `Invalid`/`Rejected` prints Apple's log and fails, and `In Progress` preserves a pending checkpoint and skips publication. The signed App ZIP, signed DMG when available, byte hashes, release context, receipts and Apple logs are retained together for **14 days**; credentials never enter the artifact. The unsigned build artifact expires after one day. Only the final signed, stapled DMG and its final checksum enter the public release.

If Apple is delayed, query the existing submission before recovering the draft:

```sh
gh workflow run notarization-status.yml --repo caelis-labs/caelis-bot --ref main -f submission_id=APPLE_SUBMISSION_UUID
```

Replace `APPLE_SUBMISSION_UUID` with the ID from the release log or receipt. This main-only workflow reads the existing submission with the two notarization secrets in `macos-release`; it does not receive the signing identity, upload another artifact or publish a release. Its run summary and `notarization-status` artifact contain the current status. If the result is `In Progress`, leave the draft unpublished; use `resume_run_id` to wait again with the retained artifacts. Once Apple finishes, inspect any rejection log or resume the same checkpoint after `Accepted`. Never overwrite a published release.

## Local build and signing

```sh
make setup
make check
make smoke
make package
```

Local builds get a `-dev` / `.dev` suffix so they do not masquerade as a published release. For an exact release rebuild, check out its tag and pass that tag:

```sh
BOT_RELEASE_TAG=v0.1.0 make package
```

The build rejects tag/package mismatches. `dist/releases/` contains the DMG and checksum; `dist/Caelis Bot.app` contains the local app. Development build signing remains `codesign --force --sign - --identifier dev.caelis.bot`, followed by strict signature validation. **Ad-hoc signing is neither Developer ID signing nor Apple notarization.** Ordinary PR CI does not require an Apple account/certificate. After explicitly signing an existing bundle, use `BOT_SIGNING_MODE=developer-id BOT_SIGNING_TEAM_ID=YOURTEAMID bash script/package.sh --skip-build`; rebuilding would replace that signature with the development signature.

For native acceptance of an already signed app in `dist`, run `BOT_SIGNING_TEAM_ID=YOURTEAMID bash script/build_and_run.sh --verify-signed`; this preserves the signature instead of rebuilding. It checks native startup, not Apple notarization. Inspect the actual desktop/chat/settings surfaces separately. Use an isolated `CAELIS_BOT_DATA_DIR` for test conversations.

User-facing download and installation steps are in [English installation](install.md) and [中文安装](install.zh-CN.md). Do not remove quarantine or re-sign a downloaded release to conceal a verification failure.

## Automation references

[release-please manifest configuration](https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md) · [Release Please Action](https://github.com/googleapis/release-please-action) · [GitHub protected branches](https://docs.github.com/en/rest/branches/branch-protection) · [Apple Developer ID certificates](https://developer.apple.com/help/account/certificates/create-developer-id-certificates) · [GitHub macOS certificate handling](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications) · [Apple first-launch guidance](https://support.apple.com/en-us/102445)
