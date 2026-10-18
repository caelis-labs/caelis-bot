import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {pathToFileURL} from 'node:url';

export function expectedVersion(pin) {
  assert.match(pin.version, /^v\d+\.\d+\.\d+(?:-[\w.]+)?$/);
  assert.match(pin.cua_version, /^\d+\.\d+\.\d+$/);
  assert.match(pin.cua_revision, /^[a-f0-9]{40}$/);
  return `dtw ${pin.version.slice(1)} (CUA ${pin.cua_version} source ${pin.cua_revision.slice(0,12)})`;
}
export function verifyVersion(output, pin) {
  assert.equal(output.trim(), expectedVersion(pin), 'Desktop World runtime or CUA provenance mismatch');
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const pin = JSON.parse(readFileSync(new URL('../resources/desktop-world/release.json', import.meta.url), 'utf8'));
  assert.ok(process.argv[2], 'native Desktop World executable required');
  verifyVersion(execFileSync(process.argv[2], ['version'], {encoding:'utf8', timeout:15000}), pin);
  console.log('Desktop World runtime and CUA version match the release pin.');
}
