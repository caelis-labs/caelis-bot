import {test} from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {resolve} from 'node:path';
import {readFileSync} from 'node:fs';
import {expectedVersion, verifyVersion} from './verify-desktop-world-version.mjs';

test('both platforms verify the complete version from pinned CUA metadata', () => {
  const pin=JSON.parse(readFileSync('resources/desktop-world/release.json','utf8'));
  assert.equal(expectedVersion(pin),'dtw 0.1.2 (CUA 0.37.0 source 2d2f99228d4d)');
  verifyVersion(expectedVersion(pin)+'\r\n',pin);
  assert.throws(()=>verifyVersion('dtw 0.1.2 (CUA source 2d2f99228d4d)',pin));
  assert.throws(()=>verifyVersion(expectedVersion(pin),{...pin,cua_version:'0.35.3'}));
});

test('slim Plugin staging rejects corrupt provenance, partial exports and unsafe package inventories', () => {
  const result = spawnSync(process.platform === 'win32' ? 'python' : 'python3', [resolve('script/test_desktop_world_staging.py')], {encoding:'utf8',timeout:30000});
  assert.equal(result.status, 0, result.stdout + result.stderr);
});
