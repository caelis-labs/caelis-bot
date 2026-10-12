import {test} from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {resolve} from 'node:path';

test('single-binary staging rejects corrupt provenance, partial exports and unsafe package inventories', () => {
  const result = spawnSync(process.platform === 'win32' ? 'python' : 'python3', [resolve('script/test_desktop_world_staging.py')], {encoding:'utf8',timeout:30000});
  assert.equal(result.status, 0, result.stdout + result.stderr);
});
