import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, rmSync, writeFileSync, openSync, writeSync, closeSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawnSync} from 'node:child_process';

test('native signature gate rejects ad-hoc as Developer ID and rejects altered bytes', {skip: process.platform !== 'darwin'}, () => {
  const directory = mkdtempSync(join(tmpdir(), 'caelis-signature-'));
  try {
    const source = join(directory, 'fixture.c'), binary = join(directory, 'fixture');
    writeFileSync(source, 'int main(void) { return 0; }\n');
    const run = (command, args, env = {}) => spawnSync(command, args, {
      encoding: 'utf8', env: {...process.env, ...env}, timeout: 30000,
    });
    assert.equal(run('clang', [source, '-o', binary]).status, 0);
    assert.equal(run('codesign', ['--force', '--sign', '-', '--identifier', 'dev.caelis.bot', binary]).status, 0);
    const verify = (mode, env) => run('/bin/bash', [resolve('script/verify-signature.sh'), binary, mode], env);
    assert.equal(verify('adhoc').status, 0);
    assert.notEqual(verify('developer-id', {BOT_SIGNING_TEAM_ID: 'ABCDE12345'}).status, 0);
    assert.notEqual(verify('developer-id', {BOT_SIGNING_TEAM_ID: ''}).status, 0);
    assert.notEqual(verify('invalid').status, 0);
    const fd = openSync(binary, 'r+');
    try { writeSync(fd, Buffer.from('altered'), 0, 7, 128); } finally { closeSync(fd); }
    assert.notEqual(verify('adhoc').status, 0);
  } finally { rmSync(directory, {recursive: true, force: true}); }
});

test('release signing cannot silently fall back when credentials are absent', {skip: process.platform !== 'darwin'}, () => {
  const result = spawnSync('/bin/bash', [resolve('script/sign-release.sh')], {
    encoding: 'utf8', timeout: 10000,
    env: {...process.env, BOT_SIGNING_CERTIFICATE_BASE64: '', BOT_SIGNING_CERTIFICATE_PASSWORD: ''},
  });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Missing Developer ID PKCS12 secret/);
});
