import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {test} from 'node:test';

test('Desktop World payload validates source, binary metadata, notice and native-only contents', () => {
  const directory = mkdtempSync(join(tmpdir(), 'desktop-world-manifest-'));
  const pin = JSON.parse(readFileSync('resources/desktop-world/release.json', 'utf8'));
  const manifest = {
    version: pin.version, 'vcs.revision': pin.revision, 'vcs.modified': 'false',
    os: 'darwin', arch: 'arm64', protocol: 'desktop-world/helper-v0.1',
    host_control: 'desktop-world/host-control-v0.1', minimum_macos: pin.minimum_macos,
    license: 'no-open-source-license-granted',
  };
  const write = (value = manifest) => writeFileSync(join(directory, 'manifest.json'), JSON.stringify(value));
  const verify = (binary = manifest) => spawnSync(process.execPath,
    [resolve('script/verify-desktop-world-manifest.mjs'), directory, JSON.stringify(binary), '--bundled'],
    {encoding: 'utf8', timeout: 10000});
  try {
    mkdirSync(join(directory, 'bin'));
    writeFileSync(join(directory, 'bin', 'desktop-world'), 'fixture');
    writeFileSync(join(directory, 'NOTICE'), 'No open-source license is granted.');
    write();
    let result = verify();
    assert.equal(result.status, 0, result.stderr);
    for (const [key, value] of Object.entries({version: 'v9.0.0', 'vcs.revision': '0'.repeat(40),
      'vcs.modified': 'true', arch: 'amd64', protocol: 'wrong', host_control: 'wrong',
      minimum_macos: '12.0', license: 'MIT'})) {
      write({...manifest, [key]: value});
      assert.notEqual(verify().status, 0, `manifest ${key}`);
      write();
    }
    assert.notEqual(verify({...manifest, 'vcs.revision': '0'.repeat(40)}).status, 0, 'actual binary drift');
    writeFileSync(join(directory, 'NOTICE'), 'Different notice');
    assert.notEqual(verify().status, 0, 'notice must preserve upstream license boundary');
    writeFileSync(join(directory, 'NOTICE'), 'No open-source license is granted.');
    writeFileSync(join(directory, 'bin', 'node'), 'retired runtime');
    assert.notEqual(verify().status, 0, 'unexpected runtime must not ship');
    rmSync(join(directory, 'bin', 'node'));
    writeFileSync(join(directory, 'host.mjs'), 'retired host');
    assert.notEqual(verify().status, 0, 'unexpected host must not ship');
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});
