import assert from 'node:assert/strict'
import test from 'node:test'
import {releaseVersion, validateTag} from './release-version.mjs'

test('release identity matches tag while macOS receives a numeric bundle version', () => {
  assert.deepEqual(releaseVersion('0.12.0', 'v0.12.0'), {version: '0.12.0', bundleVersion: '0.12.0'})
  assert.equal(releaseVersion('1.2.3').version, '1.2.3-dev')
  assert.throws(() => releaseVersion('0.1.0', 'v0.2.0'), /does not match/)
})
test('reject unsafe, ambiguous or malformed release identifiers before git or shell paths', () => {
  for (const tag of ['main', 'v01.0.0', 'v1.2.3-preview.1', 'v1.2.3+other', 'v1.2.3/../../main', 'v1.2.3\n', '--help']) {
    assert.throws(() => validateTag(tag), tag)
  }
})
