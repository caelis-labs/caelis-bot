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
- `make package`: compile and ad-hoc sign the app; build a compressed read-only DMG; mount it and verify the enclosed signature, executable and license.
- Keep the DMG and checksum as a seven-day CI artifact. These are development builds, not published releases.

`make smoke` additionally checks a locally installed Codex runtime without a model call. CI does not log into Codex or exercise a real conversation. Packaging is not interactive desktop acceptance; see [native acceptance](native-acceptance.md) and [backend acceptance](backend-acceptance.md). Intel and Windows GUI releases are not currently qualified.

Dependabot checks Actions, npm and Go dependencies weekly. Dependency PRs use the same required check and are not auto-merged. Update `toolchain.json` when changing the corresponding explicitly pinned toolchain; review Wails/Codex compatibility separately.

## Release flow

Use Conventional Commit PR titles, for example `feat: add reminders` or `fix: keep approvals visible`. The squash title becomes the commit title. `feat` and `fix` trigger versioning; `chore` and `docs` alone do not create a product release.

1. A main push runs `release-please`. It maintains one version PR, updating `package.json`, `package-lock.json`, `CHANGELOG.md` and `.release-please-manifest.json`.
2. Review and merge that PR after its native **product** check. Version PRs are not auto-merged.
3. release-please creates a `vX.Y.Z` tag and **draft** GitHub release. The reusable **Release DMG** workflow checks that the tag belongs to main and exactly matches `package.json`.
4. Re-run checks and build from that exact tagged commit without signing credentials. The application embeds its full release version and source SHA; its macOS short version stays numeric. A separate `macos-release` environment job verifies that embedded SHA, signs the app with Developer ID and a secure timestamp, enables hardened runtime, submits it to Apple and staples its notarization ticket.
5. Package the stapled app, mount the DMG and verify the enclosed app. Sign the DMG itself, submit it to Apple, staple its ticket, verify both its signature and Gatekeeper acceptance, then calculate the final SHA-256. Both the app and DMG carry tickets for offline validation.
6. Upload `Caelis-Bot-X.Y.Z-macos-arm64.dmg` and `.dmg.sha256`. Only after all signature, notarization, Gatekeeper and checksum gates succeed is the draft published. Stable tags become GitHub's latest release; tags containing a prerelease suffix stay prereleases.

A failed build remains a draft. Published release assets are never overwritten by the workflow. To recover a failed draft from the same tag, copy:

```sh
gh workflow run release.yml --repo caelis-labs/caelis-bot --ref main -f tag=v0.1.0
```

Substitute the failed draft's exact tag. The recovery workflow rejects a tag outside main, a version mismatch or a release that is already published. It can replace incomplete assets **in a draft**. The artifact retains the tagged source even if main has moved.

For the v0.1.0 promotion, set `prerelease` to `false` while retaining the prerelease versioning strategy; it promotes the current preview to its stable version. Keep version/package/changelog changes in the release-please version PR. Do not hand-edit published tags or bump the manifest independently.

## Credentials and least privilege

The existing **Caelis Character Publisher** GitHub App is installed only on `caelis-bot`, with Contents and Pull requests write permissions. Public Actions uses:

- repository variable `RELEASE_APP_ID`;
- encrypted repository secret `RELEASE_APP_PRIVATE_KEY`.

The private asset publisher holds its separately configured copy for asset PRs. Only the main-branch version job receives the public secret and mints a short-lived repository-scoped App token. This lets release PRs trigger normal CI without allowing `GITHUB_TOKEN` to create/approve PRs. No personal access token, admin permission, private modeling credential or deploy key is needed. Rotate the App key in both repositories when rotating this shared automation identity.

PR CI has read-only permissions and no App or Apple secrets. Both the build and signing/packaging jobs have read-only repository permissions; only the final publication job gets Contents write via its ephemeral `GITHUB_TOKEN`. Apple secrets are scoped to the `macos-release` environment, restricted to the `main` branch, and injected only into the signing step. Actions are pinned by commit SHA.

## Developer ID signing and Apple notarization

Public releases always require Developer ID Application signing and Apple notarization. There is no feature switch or ad-hoc fallback. An expired/wrong-type certificate, team mismatch, missing credentials/timestamp/hardened runtime, rejected or timed-out notarization, invalid staple, or Gatekeeper rejection stops publication. Existing preview releases remain historical, unnotarized artifacts.

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

The signing job downloads only the app built by its own preceding job; it does not install npm/Go dependencies or compile with certificate access. `script/sign-release.sh` imports the identity into a temporary keychain with a random password and codesign-only key access. It verifies the pinned public Apple G2 intermediate, stores validated notary credentials in the same temporary keychain, and removes all credentials on exit. An always-run workflow cleanup also handles cancellation. It never changes the user's default/login keychain or system trust policy. The hosted runner is discarded after the job.

`script/verify-signature.sh` checks Apple trust, the Developer ID Application certificate OID, team ID, artifact identifier, secure timestamp and (for the app) hardened runtime. The bundle currently contains one executable and no embedded helpers. It needs no hardened-runtime exceptions: WebKit runs out of process. New nested executable code requires an explicit signing plan; `--deep` is used for verification, never signing.

Notarization waits up to 20 minutes for each submission and records the submission ID/status and Apple diagnostic log. A timeout leaves the draft unpublished. Receipts are retained as a 14-day workflow artifact; credentials are not. The unsigned build artifact expires after one day. Only the final signed, stapled DMG and its final checksum enter the public release. If Apple is delayed, inspect the recorded submission status before recovering the same draft; never overwrite a published release.

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
