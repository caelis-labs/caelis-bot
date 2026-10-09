import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, mkdirSync, rmSync, writeFileSync, openSync, writeSync, closeSync} from 'node:fs';
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

test('linked worktree inherits only the main checkout explicit development pin', () => {
  const directory=mkdtempSync(join(tmpdir(),'bot-signing-worktree-'));
  try {
    const main=join(directory,'main'), linked=join(directory,'linked'), fingerprint='A'.repeat(40);
    const git=(...args)=>spawnSync('git',args,{encoding:'utf8',timeout:10000});
    assert.equal(git('init','-q',main).status,0);
    writeFileSync(join(main,'marker'),'fixture\n');
    assert.equal(git('-C',main,'add','marker').status,0);
    assert.equal(git('-C',main,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','base').status,0);
    writeFileSync(join(main,'.development-signing-identity'),fingerprint+'\n');
    assert.equal(git('-C',main,'worktree','add','--detach',linked,'HEAD').status,0);
    writeFileSync(join(directory,'security'),'#!/bin/sh\nprintf \'  1) '+fingerprint+' "%s: Fixture (ABCDE12345)"\\n\' "$FIXTURE_IDENTITY_KIND"\n',{mode:0o755});
    const run=(extra={})=>spawnSync('/bin/bash',['-c','set -euo pipefail; source "$1"; printf "%s %s" "$BOT_BUILD_SIGN_MODE" "$BOT_BUILD_SIGN_IDENTITY"','fixture',resolve('script/development-signing.sh')],{
      encoding:'utf8',env:{...process.env,PATH:directory+':'+process.env.PATH,BOT_ROOT:linked,CI:'',BOT_DEVELOPMENT_IDENTITY:undefined,FIXTURE_IDENTITY_KIND:'Apple Development',...extra},timeout:10000,
    });
    let result=run();assert.equal(result.status,0,result.stderr);assert.equal(result.stdout,'development '+fingerprint);
    result=run({CI:'true'});assert.equal(result.status,0);assert.equal(result.stdout,'adhoc -');
    result=run({BOT_DEVELOPMENT_IDENTITY:'-'});assert.equal(result.status,0);assert.equal(result.stdout,'adhoc -');
    writeFileSync(join(linked,'.development-signing-identity'),'-\n');
    result=run();assert.equal(result.status,0);assert.equal(result.stdout,'adhoc -');
    rmSync(join(linked,'.development-signing-identity'));
    assert.notEqual(run({FIXTURE_IDENTITY_KIND:'Developer ID Application'}).status,0);
    rmSync(join(main,'.development-signing-identity'));
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

test('ordinary and CI builds select a separate app; only explicit release selects production',()=>{
 const read=extra=>spawnSync('/bin/bash',['-c','source "$1"; printf "%s|%s|%s" "$BOT_APP_NAME" "$BOT_APP_ID" "$BOT_BUNDLE"','fixture',resolve('script/app-identity.sh')],{
  encoding:'utf8',env:{...process.env,BOT_ROOT:'/fixture',BOT_BUILD_CHANNEL:'',BOT_RELEASE_TAG:'',...extra},timeout:10000});
 for(const extra of [{},{CI:'true'}]) {const r=read(extra);assert.equal(r.status,0);assert.equal(r.stdout,'Caelis Bot Dev|dev.caelis.bot.dev|/fixture/dist/Caelis Bot Dev.app');}
 for(const extra of [{BOT_BUILD_CHANNEL:'release'},{BOT_RELEASE_TAG:'v1.0.0'}]) {const r=read(extra);assert.equal(r.status,0);assert.equal(r.stdout,'Caelis Bot|dev.caelis.bot|/fixture/dist/Caelis Bot.app');}
 {const r=read({BOT_RELEASE_TAG:'v1.1.0-dev.1'});assert.equal(r.status,0);assert.equal(r.stdout,'Caelis Bot|dev.caelis.bot|/fixture/dist/Caelis Bot.app');}
 assert.notEqual(read({BOT_BUILD_CHANNEL:'typo'}).status,0);
});

test('Desktop World is mandatory for all app bundles', {skip: process.platform !== 'darwin'},()=>{
 const directory=mkdtempSync(join(tmpdir(),'bot-desktop-world-bundle-'));
 try {
  const app=join(directory,'Fixture.app'), contents=join(app,'Contents');mkdirSync(contents,{recursive:true});
  const plist=join(contents,'Info.plist');
  writeFileSync(plist,'<?xml version="1.0"?><plist version="1.0"><dict/></plist>');
  const verify=(...args)=>spawnSync('/bin/bash',[resolve('script/verify-desktop-world.sh'),app,'adhoc',...args],{encoding:'utf8',timeout:10000});
  assert.notEqual(verify().status,0,'new builds require the payload');
  assert.notEqual(verify('--allow-legacy').status,0,'removed legacy bypass must not skip verification');
  mkdirSync(join(contents,'Resources/DesktopWorld'),{recursive:true});
  assert.notEqual(verify('--allow-legacy').status,0,'unmarked payload is invalid');
  rmSync(join(contents,'Resources'),{recursive:true});
  assert.equal(spawnSync('/usr/libexec/PlistBuddy',['-c','Add CaelisDesktopWorldVersion string v0.1.0-alpha.1',plist]).status,0);
  assert.notEqual(verify('--allow-legacy').status,0,'declared but missing payload is corrupt');
 } finally {rmSync(directory,{recursive:true,force:true});}
});

test('native dependency gate permits build-time self IDs but rejects external loads and search paths', {skip: process.platform !== 'darwin'},()=>{
 const directory=mkdtempSync(join(tmpdir(),'bot native dependencies '));
 try {
  const source=join(directory,'fixture.c'), library=join(directory,'fixture.dylib'), consumer=join(directory,'consumer');
  const run=(command,args)=>spawnSync(command,args,{encoding:'utf8',timeout:30000});
  writeFileSync(source,'int fixture(void) { return 0; }\n');
  assert.equal(run('clang',['-dynamiclib',source,'-install_name',library,'-o',library]).status,0);
  const arch=run('lipo',['-archs',library]).stdout.trim();
  const verify=binary=>run('/bin/bash',[resolve('script/verify-native-dependencies.sh'),arch,binary]);
  let result=verify(library);assert.equal(result.status,0,result.stderr);
  writeFileSync(source,'extern int fixture(void); int main(void) { return fixture(); }\n');
  assert.equal(run('clang',[source,library,'-o',consumer]).status,0);
  result=verify(consumer);assert.notEqual(result.status,0);assert.ok(result.stderr.includes(library));
  writeFileSync(source,'int main(void) { return 0; }\n');
  assert.equal(run('clang',[source,'-Wl,-rpath,'+directory,'-o',consumer]).status,0);
  result=verify(consumer);assert.notEqual(result.status,0);assert.ok(result.stderr.includes(directory));
  assert.equal(run('clang',[source,'-Wl,-rpath,@loader_path','-o',consumer]).status,0);
  result=verify(consumer);assert.equal(result.status,0,result.stderr);
 } finally {rmSync(directory,{recursive:true,force:true});}
});
