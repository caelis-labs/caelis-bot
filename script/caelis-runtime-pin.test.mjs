import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import test from 'node:test';
import {pinRequests, publishedChecksums, selectRelease} from './caelis-runtime-pin.mjs';

const current = {
  version: 'v0.9.0',
  targets: {'darwin-arm64': {}, 'windows-amd64': {}, 'linux-amd64': {}, 'linux-arm64': {}},
};
const names = [
  'caelis_0.10.0_darwin_arm64.tar.gz',
  'caelis_0.10.0_windows_amd64.tar.gz',
  'caelis_0.10.0_linux_amd64.tar.gz',
  'caelis_0.10.0_linux_arm64.tar.gz',
];
const hash = 'a'.repeat(64);
const checksumFile = Buffer.from(names.map(name => `${hash}  ${name}\n`).join(''));
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const asset = (name, digest = hash) => ({name, state: 'uploaded', size: 100, digest: `sha256:${digest}`});
const release = {
  tag_name: 'v0.10.0',
  draft: false,
  prerelease: false,
  published_at: '2026-11-28T00:00:00Z',
  assets: [...names.map(name => asset(name)), asset('checksums.txt', digest(checksumFile))],
};

test('selects a complete numerically newer stable release without downgrading', () => {
  const candidate = selectRelease(current, release);
  assert.equal(candidate.tag, 'v0.10.0');
  assert.deepEqual(Object.values(candidate.targets), names);
  for (const version of ['v0.10.0', 'v0.10.1']) {
    assert.match(selectRelease({...current, version}, release).message, /^No upgrade:/);
  }
});

test('waits for publication and all expected digested assets', () => {
  for (const incomplete of [
    {...release, draft: true},
    {...release, prerelease: true},
    {...release, published_at: null},
    {...release, assets: release.assets.slice(1)},
    {...release, assets: release.assets.slice(0, -1)},
    {...release, assets: [...release.assets.slice(1), {...release.assets[0], state: 'new'}]},
    {...release, assets: [...release.assets.slice(1), {...release.assets[0], digest: null}]},
  ]) {
    assert.match(selectRelease(current, incomplete).message, /^Waiting/);
  }
  assert.throws(() => selectRelease(current, {...release, tag_name: 'v0.10.0-rc.1'}), /stable semantic/);
});

test('requires the checksum file digest and one matching checksum per target', () => {
  const candidate = selectRelease(current, release);
  assert.deepEqual(publishedChecksums(checksumFile, candidate), Object.fromEntries(names.map(name => [name, hash])));
  assert.throws(() => publishedChecksums(Buffer.from('altered bytes'), candidate), /file digest mismatch/);

  for (const bytes of [
    Buffer.concat([checksumFile, checksumFile]),
    Buffer.from(checksumFile.toString().replace(hash, 'b'.repeat(64))),
    Buffer.from(checksumFile.toString().split('\n').slice(1).join('\n')),
  ]) {
    assert.throws(() => publishedChecksums(bytes, {
      ...candidate, assets: {...candidate.assets, 'checksums.txt': {sha256: digest(bytes)}},
    }), /missing, duplicated or different/);
  }
});

test('defers to existing main-targeted pin PRs based on changed files', () => {
  const repo = 'caelis-labs/caelis-bot';
  const pull = (number, name, fullName = repo, base = 'main') => ({
    number, head: {ref: name, repo: {full_name: fullName}}, base: {ref: base},
  });
  const branch = 'automation/caelis-runtime-pin';
  const owned = pull(11, branch);
  const release = pull(12, 'release-please--branches--main');
  const manual = {...pull(13, 'custom-branch-name'), draft: true};
  const fork = pull(14, branch, 'contributor/caelis-bot');
  const docs = pull(15, 'docs', repo, 'feature');
  const paths = new Map([
    [12, ['CHANGELOG.md']],
    [13, ['resources/caelis-runtime/release.json', 'internal/compatibility.go']],
    [14, ['protocol/caelis/manifest.json']],
  ]);
  const filenames = number => {
    assert.ok(paths.has(number), `unexpected file read for PR #${number}`);
    return paths.get(number);
  };
  assert.deepEqual(pinRequests([owned, release, docs], filenames), {owned, competing: undefined});
  assert.deepEqual(pinRequests([owned, release, docs, manual], filenames), {owned, competing: manual});
  assert.deepEqual(pinRequests([release, fork], filenames), {owned: undefined, competing: fork});
  assert.throws(() => pinRequests([pull(11, branch, repo, 'other')], filenames), /Resolve the open/);
});
