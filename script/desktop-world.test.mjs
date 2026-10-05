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
    license: 'MPL-2.0', signing: 'ad-hoc', notarized: false,
  };
  const write = (value = manifest) => writeFileSync(join(directory, 'manifest.json'), JSON.stringify(value));
  const verify = (binary = manifest) => spawnSync(process.execPath,
    [resolve('script/verify-desktop-world-manifest.mjs'), directory, JSON.stringify(binary), '--bundled'],
    {encoding: 'utf8', timeout: 10000});
  try {
    mkdirSync(join(directory, 'bin'));
    mkdirSync(join(directory, 'source', 'host'), {recursive: true});
    writeFileSync(join(directory, 'bin', 'dtw'), 'fixture');
    writeFileSync(join(directory, 'NOTICE'), 'This Source Code Form is subject to the terms of the Mozilla Public License');
    writeFileSync(join(directory, 'LICENSE'), 'Mozilla Public License\nVersion 2.0');
    writeFileSync(join(directory, 'source', 'go.mod'), 'module github.com/caelis-labs/desktop-world\n');
    writeFileSync(join(directory, 'source', 'host', 'client.go'), 'func (c *Client) Declare(');
    writeFileSync(join(directory, 'THIRD_PARTY_NOTICES.md'), 'MIT License\nPermission is hereby granted\nTHE SOFTWARE IS PROVIDED');
    rmSync(join(directory, 'LICENSE'));
    assert.notEqual(verify().status, 0, 'MPL license must ship');
    writeFileSync(join(directory, 'LICENSE'), 'Mozilla Public License\nVersion 2.0');
    rmSync(join(directory, 'source'), {recursive: true});
    assert.notEqual(verify().status, 0, 'corresponding source must ship');
    mkdirSync(join(directory, 'source', 'host'), {recursive: true});
    writeFileSync(join(directory, 'source', 'go.mod'), 'module github.com/caelis-labs/desktop-world\n');
    writeFileSync(join(directory, 'source', 'host', 'client.go'), 'func (c *Client) Declare(');
    write();
    let result = verify();
    assert.equal(result.status, 0, result.stderr);
    for (const [key, value] of Object.entries({version: 'v9.0.0', 'vcs.revision': '0'.repeat(40),
      'vcs.modified': 'true', arch: 'amd64', protocol: 'wrong', host_control: 'wrong',
      minimum_macos: '12.0', license: 'MIT', signing: 'developer-id', notarized: true})) {
      write({...manifest, [key]: value});
      assert.notEqual(verify().status, 0, `manifest ${key}`);
      write();
    }
    assert.notEqual(verify({...manifest, 'vcs.revision': '0'.repeat(40)}).status, 0, 'actual binary drift');
    writeFileSync(join(directory, 'NOTICE'), 'Different notice');
    assert.notEqual(verify().status, 0, 'notice must preserve upstream license boundary');
    writeFileSync(join(directory, 'NOTICE'), 'This Source Code Form is subject to the terms of the Mozilla Public License');
    rmSync(join(directory, 'THIRD_PARTY_NOTICES.md'));
    assert.notEqual(verify().status, 0, 'third-party notice must ship');
    writeFileSync(join(directory, 'THIRD_PARTY_NOTICES.md'), 'MIT License\nPermission is hereby granted\nTHE SOFTWARE IS PROVIDED');
    writeFileSync(join(directory, 'bin', 'node'), 'retired runtime');
    assert.notEqual(verify().status, 0, 'unexpected runtime must not ship');
    rmSync(join(directory, 'bin', 'node'));
    writeFileSync(join(directory, 'host.mjs'), 'retired host');
    assert.notEqual(verify().status, 0, 'unexpected host must not ship');
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});
