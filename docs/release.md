# Development and releases

## Ownership

`caelis-bot` owns the application, finished character packs and macOS distribution. The private `caelis-bot-assets` repository owns modeling sources and opens product asset PRs. A product build uses checked-in finished files only; no private checkout or Blender is involved.

## Required checks and branch policy

`main` requires a pull request, the **product** GitHub Actions check against the current base, resolved conversations and linear history. Administrators are included; force pushes and deletion are blocked. The current single-maintainer setup requires zero independent approvals, so a sole maintainer can merge after checks. Only squash merges are enabled; merged branches are deleted automatically. A tag ruleset prevents updates/deletion of `v*` tags while allowing new release tags.

Product CI runs on code PRs and manual requests. The required **product** check
joins the selected macOS (`macos-15`), Windows (`windows-2025`) and Linux headless results; failed, cancelled or
unexpectedly skipped jobs cannot pass it. The strict current-base PR gate checks
the combined source before merging, so a main push does not repeat that suite.

Documentation-only changes under `docs/`, `README.md` and `CHANGELOG.md` use a
lightweight check, retaining the public-tree boundary guard. Release metadata uses
the same path only when the actual diff
changes version values in `package.json`, `package-lock.json` and the manifest,
with matching valid versions and no other JSON changes. Dependencies, scripts,
Bot skills, workflow changes and unknown paths always receive full checks. Bot
author, labels and branch name do not determine this routing.

Full checks retain:

- Pinned actionlint validates workflow syntax and expressions.
- `npm ci` uses the lockfile; Node and Go come from repository pins.
- `make check`: public/private boundary, finished asset hashes, TypeScript/build, focused frontend contracts and Go tests.
- `npm run smoke:assets`: validate, parse and animate the delivered GLBs.
- `make build` and the packaged Desktop World contract test: compile and ad-hoc sign the Dev app, verify native dependencies and exercise the delivered helper without starting a desktop fixture.
- Linux native ownership/recovery race tests and both helper architectures.

Routine PR checks no longer run native window fixtures, a second macOS race pass,
an extra shared Go job, Windows frontend tests/build or the updater install fixture.
Native desktop acceptance is manual via `script/build_and_run.sh`; release builds
still compile the tagged app and run updater checks. Go cache restoration uses the same `.cache/go-build` directory as project scripts;
the cache key includes `go.sum` and `script/env.sh` to refresh the old cache layout.
The release build shares that layout, and its main-branch cache can seed later PRs.

Packaging-related changes also build and mount a compressed read-only Dev DMG,
verifying the enclosed signature, executable, license and Finder layout. Other
PRs do not create/upload an installer. To request one, manually run **Product
checks** with `preview=true`; its DMG and checksum are retained for seven days.
These are development builds, not published releases. The installer uses a
660×430 window, 128-point icons and a left-to-right Applications drag target.
Pinned dmgbuild dependencies are isolated in `.cache/dmg-tools` (Python 3 required);
no Finder automation is used.

`make smoke` additionally checks a locally installed Codex runtime without a model call. CI does not log into Codex or exercise a real conversation. Packaging is not interactive desktop acceptance; see [native acceptance](development.md) and [backend acceptance](caelis-integration.md). Intel and Windows GUI releases are not currently qualified.

Dependabot checks Actions, npm and Go dependencies weekly. Dependency PRs use the same required check and are not auto-merged. Update `toolchain.json` when changing the corresponding explicitly pinned toolchain; review Wails/Codex compatibility separately.

## Release flow

Use Conventional Commit squash titles. With the current `release-please` default versioning and the 0.11.0 manifest, `feat:` increments the minor version (`0.12.0` → `0.13.0`) and `fix:` increments the patch (`0.12.0` → `0.12.1`). A version PR is the human release decision: review it and merge it only when the change is ready. `chore:` and `docs:` alone do not trigger a product version. There is no installable Dev or preview release channel.

1. A push to `main` makes release-please maintain a version PR with `package.json`, its lockfile, `CHANGELOG.md` and the manifest. The version PR is not auto-merged.
2. Merging that PR makes release-please create an immutable `vX.Y.Z` tag and draft GitHub release. Its `release_created` output starts the macOS arm64 release workflow automatically.
3. The build job checks out that tag's commit, checks its version, runs the updater and asset checks, and builds `Caelis Bot.app` without Apple credentials. The app embeds the tag and source SHA. The separate `macos-release` job verifies that SHA, signs with Developer ID, notarizes and staples both app and DMG, mounts the DMG, and checks Gatekeeper and SHA-256. Any failure leaves the draft unpublished.
4. The package job signs a one-version Sparkle appcast and a manifest for the final DMG. The publisher reconciles exact GitHub assets and its immutable platform receipt, then publishes the draft. R2 verifies the signed bytes and receipt before writing the stable feed. Only after R2 succeeds does GitHub Latest move to that version. GitHub Latest is global release metadata; `--latest=false` during asset publication prevents it from pointing ahead of the usable macOS feed.

The installable product is `Caelis Bot.app`, bundle ID `dev.caelis.bot`, with the existing `Application Support/Caelis Bot` data. Local and PR builds use `Caelis Bot Dev.app`, bundle ID `dev.caelis.bot.dev`, with separate development data. Those local builds are not public releases and do not check the release feed. Currently only macOS Apple Silicon has an installable release workflow; Windows needs its own signed package and native checks before publication.

Before merging a version PR, the owner should check the relevant blocking bugs and run the affected real user paths on an appropriate installed build: launch/relaunch, conversation and unresolved work recovery, chat input/reply, Telegram, plugins and update/data preservation where those paths changed. Record the result in the version PR or issue. CI and fixtures do not prove real desktop acceptance; this guidance uses the ordinary version PR rather than a second candidate workflow or external acceptance token.

If signing or notarization is pending or fails, inspect the draft and the original run. A retained `notarization-checkpoint` carries the signed bytes, Apple submission IDs, tag and SHA for 14 days. Query a pending submission with `notarization-status.yml`; resume the **same** completed run using `release.yml`'s `resume_run_id` only after verifying its receipt. The workflow checks the original tag/source/team and does not resubmit known Apple work. A missing checkpoint cannot reconstruct the submitted bytes; inspect its original result before a fresh build.

```sh
gh workflow run release.yml --repo caelis-labs/caelis-bot --ref main -f tag=vX.Y.Z
gh workflow run release.yml --repo caelis-labs/caelis-bot --ref main -f tag=vX.Y.Z -f resume_run_id=COMPLETED_RELEASE_RUN_ID
```

Use manual `release.yml` only to recover an existing draft tag, not to create a new release. `publication.mjs` downloads and hashes any existing GitHub asset, uploads only missing names, and reconciles unknown uploads against the same bytes. It never silently replaces a differing asset. If only R2 fails, use `sync-r2.yml` for the already published tag; it verifies GitHub assets, signature, source and receipt, and then reconciles R2 and GitHub Latest.

For a blocking published version, fix forward with a higher SemVer tag. An exceptional feed rollback requires the owner to verify an older release's DMG, checksum, signed appcast, manifest and receipt, copy only the signed mutable feed files to the existing stable aliases, and read them back. Keep immutable versioned DMGs and tags unchanged. Sparkle will not downgrade an installed higher version; affected clients need a higher fixed version or explicit manual reinstall. Normal `publish-r2.mjs` rejects rollback.

## Automatic updates and R2

Installable releases embed SHA-256-pinned Sparkle 2.10.0 and the persistent public key. The bundle uses numeric `CFBundleVersion=X.Y.Z` and reads the existing signed `https://releases.caelis.dev/caelis-bot/appcast.xml`. The app checks daily unless the user disables automatic checks; installation needs confirmation. Active or uncertain work postpones relaunch until the original owner is safely closed. Existing v0.1.0 installations need one manual upgrade to the first updater-enabled release.

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

After `verified=true` (Developer ID, notarization, staples and Gatekeeper), `generate_appcast` signs the final DMG and feed. `latest.json` binds tag, source SHA, DMG hash/size and appcast hash and has its own Ed25519 signature. GitHub assets include those bytes and the stable receipt `Caelis-Bot-X.Y.Z-macos-arm64-stable.publication.json`.

R2 uploads immutable assets under `caelis-bot/releases/vX.Y.Z/` and writes the signed macOS stable feed at `caelis-bot/feeds/macos/arm64/stable/` plus the existing `caelis-bot/appcast.xml`, `latest.json` and `latest.json.sig` aliases used by installed clients. Both paths describe the same release; the app reads the legacy alias. The publisher reads back every uploaded object, rejects same-version byte changes and rollback, and retains older immutable DMGs for cached appcasts. It never deletes another product's bucket keys.

```sh
gh workflow run sync-r2.yml --repo caelis-labs/caelis-bot --ref main -f tag=vX.Y.Z
```

`make check` covers signatures, publication receipt recovery, R2 ownership and rollback using fixtures. `make check-updater` exercises a disposable signed feed and native postponed installation. Neither proves a Developer ID installation or real relaunch from public R2; verify that separately when needed. See [Sparkle's documentation](https://sparkle-project.org/documentation/).

## Credentials and least privilege

The existing **Caelis Character Publisher** GitHub App is installed only on `caelis-bot`, with Contents and Pull requests write permissions. Public Actions uses:

- repository variable `RELEASE_APP_ID`;
- encrypted repository secret `RELEASE_APP_PRIVATE_KEY`.

The private asset publisher holds its separately configured copy for asset PRs. Only the main-branch version job receives the public secret and mints a short-lived repository-scoped App token. This lets release PRs trigger normal CI without allowing `GITHUB_TOKEN` to create/approve PRs. No personal access token, admin permission, private modeling credential or deploy key is needed. Rotate the App key in both repositories when rotating this shared automation identity.

PR CI has read-only permissions and no App or Apple secrets. Both the build and signing/packaging jobs have read-only repository permissions; only the final publication job gets Contents write via its ephemeral `GITHUB_TOKEN`. Apple secrets are scoped to the `macos-release` environment, restricted to the `main` branch, and injected only into the signing step. Actions are pinned by commit SHA.

## Developer ID signing and Apple notarization

Public releases always require Developer ID Application signing and Apple notarization. There is no feature switch or ad-hoc fallback. An expired/wrong-type certificate, team mismatch, missing credentials/timestamp/hardened runtime, rejected or pending notarization, invalid staple, or Gatekeeper rejection stops publication. Pending Apple processing preserves a resumable checkpoint; rejection, unknown status, credential and transport errors fail the job.

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

Local builds get a `-dev` suffix so they do not masquerade as a published release. For an exact release rebuild, check out its tag and pass that tag:

```sh
BOT_RELEASE_TAG=v0.1.0 make package
```

The build rejects tag/package mismatches. `dist/releases/` contains the DMG and checksum; `dist/Caelis Bot Dev.app` contains the default local app; tagged release builds use `Caelis Bot.app`. Development builds use the isolated Dev identity and strict signature validation; see [development](development.md). **Ad-hoc signing is neither Developer ID signing nor Apple notarization.** Ordinary PR CI does not require an Apple account/certificate. After explicitly signing an existing bundle, use `BOT_SIGNING_MODE=developer-id BOT_SIGNING_TEAM_ID=YOURTEAMID bash script/package.sh --skip-build`; rebuilding would replace that signature with the development signature.

For native acceptance of an already signed app in `dist`, run `BOT_SIGNING_TEAM_ID=YOURTEAMID bash script/build_and_run.sh --verify-signed`; this preserves the signature instead of rebuilding. It checks native startup, not Apple notarization. Inspect the actual desktop/chat/settings surfaces separately. Use an isolated `CAELIS_BOT_DATA_DIR` for test conversations.

User-facing download and installation steps are in [English installation](install.md) and [中文安装](install.zh-CN.md). Do not remove quarantine or re-sign a downloaded release to conceal a verification failure.

## Automation references

[release-please manifest configuration](https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md) · [Release Please Action](https://github.com/googleapis/release-please-action) · [GitHub protected branches](https://docs.github.com/en/rest/branches/branch-protection) · [Apple Developer ID certificates](https://developer.apple.com/help/account/certificates/create-developer-id-certificates) · [GitHub macOS certificate handling](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications) · [Apple first-launch guidance](https://support.apple.com/en-us/102445)
