# Development and releases

## Ownership

`caelis-bot` owns the application, finished character packs and macOS distribution. The private `caelis-bot-assets` repository owns modeling sources and opens product asset PRs. A product build uses checked-in finished files only; no private checkout or Blender is involved.

## Required checks and branch policy

`main` requires a pull request, the **product** GitHub Actions check against the current base, resolved conversations and linear history. Administrators are included; force pushes and deletion are blocked. The current single-maintainer setup requires zero independent approvals, so a sole maintainer can merge after checks. Only squash merges are enabled; merged branches are deleted automatically.

Product CI runs on native Apple Silicon (`macos-14`) for every PR and main push:

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
3. release-please creates a `vX.Y.Z-preview.N` tag and **draft** GitHub release. The reusable **Release DMG** workflow checks that the tag belongs to main and exactly matches `package.json`.
4. Re-run checks and build from that exact tagged commit. The application embeds its full release version and source SHA; its macOS short version stays numeric.
5. Upload `Caelis-Bot-X.Y.Z-preview.N-macos-arm64.dmg` and `.dmg.sha256`. Only after checksum verification and successful upload is the draft published as a prerelease.

A failed build remains a draft. Published release assets are never overwritten by the workflow. To recover a failed draft from the same tag, copy:

```sh
gh workflow run release.yml --repo caelis-labs/caelis-bot --ref main -f tag=v0.1.0-preview.1
```

Substitute the failed draft's exact tag. The recovery workflow rejects a tag outside main, a version mismatch or a release that is already published. It can replace incomplete assets **in a draft**. The artifact retains the tagged source even if main has moved.

To move to stable releases later, make a reviewed configuration PR setting `prerelease` to `false` while retaining the prerelease versioning strategy; it promotes the current preview to its stable version. Complete the outstanding platform/distribution acceptance first. Do not hand-edit a published tag or bump the manifest independently after bootstrapping.

## Credentials and least privilege

The existing **Caelis Character Publisher** GitHub App is installed only on `caelis-bot`, with Contents and Pull requests write permissions. Public Actions uses:

- repository variable `RELEASE_APP_ID`;
- encrypted repository secret `RELEASE_APP_PRIVATE_KEY`.

The private asset publisher holds its separately configured copy for asset PRs. Only the main-branch version job receives the public secret and mints a short-lived repository-scoped App token. This lets release PRs trigger normal CI without allowing `GITHUB_TOKEN` to create/approve PRs. No personal access token, admin permission, private modeling credential or deploy key is needed. Rotate the App key in both repositories when rotating this shared automation identity.

PR CI has read-only permissions and no App secret. The DMG build has read-only permissions; only the final publication job gets Contents write via its ephemeral `GITHUB_TOKEN`. Actions are pinned by commit SHA.

## Local build and signing

```sh
make setup
make check
make smoke
make package
```

Local builds get a `-dev` / `.dev` suffix so they do not masquerade as a published release. For an exact release rebuild, check out its tag and pass that tag:

```sh
BOT_RELEASE_TAG=v0.1.0-preview.1 make package
```

The build rejects tag/package mismatches. `dist/releases/` contains the DMG and checksum; `dist/Caelis Bot.app` contains the local app. Build signing is `codesign --force --sign - --identifier dev.caelis.bot`, followed by strict signature validation. **Ad-hoc signing is neither Developer ID signing nor Apple notarization.** No Apple account/certificate is configured or required for preview CI.

User-facing first-launch commands and the scope of the quarantine exception are in [English installation](install.md) and [中文安装](install.zh-CN.md). The concise local-agent installation prompts are in both READMEs. Do not re-sign a downloaded app when checksum/signature verification fails.

## Automation references

[release-please manifest configuration](https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md) · [Release Please Action](https://github.com/googleapis/release-please-action) · [GitHub protected branches](https://docs.github.com/en/rest/branches/branch-protection) · [Apple first-launch guidance](https://support.apple.com/en-us/102445)
