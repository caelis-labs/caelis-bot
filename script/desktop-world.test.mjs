import {test} from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {resolve} from 'node:path';
import {readFileSync} from 'node:fs';
import {expectedVersion, verifyVersion} from './verify-desktop-world-version.mjs';

test('both platforms verify the complete version from pinned CUA metadata', () => {
  const pin=JSON.parse(readFileSync('resources/desktop-world/release.json','utf8'));
  assert.equal(expectedVersion(pin),'dtw 0.1.0 (CUA 0.35.4 source d74a9cab3fb2)');
  verifyVersion(expectedVersion(pin)+'\r\n',pin);
  assert.throws(()=>verifyVersion('dtw 0.1.0 (CUA source d74a9cab3fb2)',pin));
  assert.throws(()=>verifyVersion(expectedVersion(pin),{...pin,cua_version:'0.35.3'}));
});

test('slim Plugin staging rejects corrupt provenance, partial exports and unsafe package inventories', () => {
  const result = spawnSync(process.platform === 'win32' ? 'python' : 'python3', [resolve('script/test_desktop_world_staging.py')], {encoding:'utf8',timeout:30000});
  assert.equal(result.status, 0, result.stdout + result.stderr);
});
