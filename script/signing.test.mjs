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
    assert.notEqual(verify('development').status, 0);
    assert.notEqual(verify('invalid').status, 0);
    const fd = openSync(binary, 'r+');
    try { writeSync(fd, Buffer.from('altered'), 0, 7, 128); } finally { closeSync(fd); }
    assert.notEqual(verify('adhoc').status, 0);
  } finally { rmSync(directory, {recursive: true, force: true}); }
});

test('local development signing pins a usable development identity; CI ignores it and missing keys fail closed', () => {
  const directory=mkdtempSync(join(tmpdir(),'bot-development-signing-'));
  try {
    const fingerprint='A'.repeat(40);
    writeFileSync(join(directory,'.development-signing-identity'),fingerprint+'\n');
    // Identity metadata only. No private key/certificate or actual signing.
    writeFileSync(join(directory,'security'),'#!/bin/sh\nprintf \'  1) '+fingerprint+' "%s: Fixture (ABCDE12345)"\\n\' "$FIXTURE_IDENTITY_KIND"\n',{mode:0o755});
    const run=(extra={})=>spawnSync('/bin/bash',['-c','set -euo pipefail; source "$1"; printf "%s %s" "$BOT_BUILD_SIGN_MODE" "$BOT_BUILD_SIGN_IDENTITY"','fixture',resolve('script/development-signing.sh')],{
      encoding:'utf8',env:{...process.env,PATH:directory+':'+process.env.PATH,BOT_ROOT:directory,CI:'',BOT_DEVELOPMENT_IDENTITY:undefined,FIXTURE_IDENTITY_KIND:'Apple Development',...extra},timeout:10000,
    });
    let result=run();assert.equal(result.status,0,result.stderr);assert.equal(result.stdout,'development '+fingerprint);
    result=run({CI:'true',FIXTURE_IDENTITY_KIND:'Developer ID Application'});assert.equal(result.status,0);assert.equal(result.stdout,'adhoc -');
    result=run({BOT_DEVELOPMENT_IDENTITY:'-'});assert.equal(result.status,0);assert.equal(result.stdout,'adhoc -');
    assert.notEqual(run({FIXTURE_IDENTITY_KIND:'Developer ID Application'}).status,0);
    assert.notEqual(run({BOT_DEVELOPMENT_IDENTITY:'B'.repeat(40)}).status,0);
    assert.notEqual(run({BOT_DEVELOPMENT_IDENTITY:'$(touch unexpected)'}).status,0);
    rmSync(join(directory,'.development-signing-identity'));
    result=run();assert.equal(result.status,0);assert.equal(result.stdout,'adhoc -');
  } finally {rmSync(directory,{recursive:true,force:true});}
});

test('release signing cannot silently fall back when credentials are absent', {skip: process.platform !== 'darwin'}, () => {
  const result = spawnSync('/bin/bash', [resolve('script/sign-release.sh')], {
    encoding: 'utf8', timeout: 10000,
    env: {...process.env, BOT_SIGNING_CERTIFICATE_BASE64: '', BOT_SIGNING_CERTIFICATE_PASSWORD: ''},
  });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Missing Developer ID PKCS12 secret/);
});
