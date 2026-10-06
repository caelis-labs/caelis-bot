import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, readFileSync, writeFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {basename, join} from 'node:path';
import {digest} from './update-manifest.mjs';
import {publishPlatform, receiptFor, validateReceipt} from './publication.mjs';
import {windowsAcceptanceName} from './windows-acceptance.mjs';

function fixture(fn) {
  const directory = mkdtempSync(join(tmpdir(), 'publication-test-'));
  const tag = 'v1.2.3', source = 'a'.repeat(40);
  const mac = {tag, source, os:'macos', arch:'arm64', channel:'stable', validation:'developer-id-notarized-stapled-gatekeeper'};
  const win = {tag, source, os:'windows', arch:'amd64', channel:'preview', validation:'windows-11-native-accepted', artifact:'Caelis-Bot-1.2.3-windows-amd64.msix'};
  const dmg = Buffer.from('Mac DMG fixture');
  const appcast = Buffer.from('signed appcast fixture');
  const macFile = 'Caelis-Bot-1.2.3-macos-arm64.dmg';
  writeFileSync(join(directory,macFile),dmg);
  writeFileSync(join(directory,`${macFile}.sha256`),`${digest(dmg)}  ${macFile}\n`);
  writeFileSync(join(directory,'appcast.xml'),appcast);
  writeFileSync(join(directory,'latest.json'),JSON.stringify({tag,source,sha256:digest(dmg),appcastSHA256:digest(appcast)}));
  writeFileSync(join(directory,'latest.json.sig'),'fixture signature');
  const winBytes = Buffer.from('Windows package fixture');
  writeFileSync(join(directory,win.artifact),winBytes);
  writeFileSync(join(directory,`${win.artifact}.sha256`),`${digest(winBytes)}  ${win.artifact}\n`);
  writeFileSync(join(directory,windowsAcceptanceName(tag,'preview')),JSON.stringify({schema:1,tag,source,platform:'windows-amd64',channel:'preview',package:{name:win.artifact,sha256:digest(winBytes)},signing:{authenticode:true,timestamp:true,thumbprint:'a'.repeat(40)},checks:Object.fromEntries([
    'tray','petVisible','petDrag','bubble','chat','settings','history','approvals','attachments','runtime','memory','tasks','telegramProtocol','desktopWorld','namedPipePeer','credentialManager','ownedProcesses','unicodePaths','webview2','cleanInstall','upgrade','uninstall','dataPreserved',
  ].map(key=>[key,true]))}));
  const assets = new Map();
  const release = {tag_name:tag,draft:true,prerelease:false,assets:[]};
  const calls = [], controls = {};
  const run = (command,args) => {
    calls.push([command,...args]);
    if (command !== 'gh') throw new Error('Unexpected command');
    if (args[0] === 'api' && args[1].endsWith(`/commits/${tag}`)) return JSON.stringify({sha:controls.source??source});
    if (args[0] === 'api' && args[1].endsWith(`/releases/tags/${tag}`)) {
      if (controls.draftTag404 && release.draft) {
        const error = new Error('gh: Not Found (HTTP 404)');
        error.stderr = 'gh: Not Found (HTTP 404)';
        throw error;
      }
      return JSON.stringify(release);
    }
    if (args[0] === 'api' && args[1].endsWith('/releases?per_page=100')) {
      return JSON.stringify(controls.hideDraft ? [] : [{tag_name:'v0.0.0',draft:true},release]);
    }
    if (args[0] === 'release' && args[1] === 'upload') {
      const path=args[3], name=basename(path);
      assets.set(name,readFileSync(path));
      release.assets=[...assets.keys()].map(name=>({name}));
      if (controls.concurrentOnUpload) {
        const other=controls.concurrentOnUpload;
        delete controls.concurrentOnUpload;
        other();
      }
      if (controls.unknownUpload===name) { delete controls.unknownUpload; throw new Error('unknown upload result'); }
      return '';
    }
    if (args[0] === 'release' && args[1] === 'download') {
      const name=args[args.indexOf('--pattern')+1];
      writeFileSync(join(args[args.indexOf('--dir')+1],name),assets.get(name));
      return '';
    }
    if (args[0] === 'release' && args[1] === 'edit') {
      release.draft=false;
      release.prerelease=args.includes('--prerelease=true');
      return '';
    }
    throw new Error(`Unexpected gh call ${args.join(' ')}`);
  };
  try {fn({directory,mac,win,source,assets,release,calls,controls,run});}
  finally {rmSync(directory,{recursive:true,force:true});}
}

test('first verified platform publishes; late Windows preview appends without changing release metadata',()=>fixture(f=>{
  const mac=publishPlatform(f.directory,f.mac,f.run);
  assert.equal(validateReceipt(mac).state,'published');
  assert.equal(f.release.draft,false);
  const before=f.calls.filter(call=>call[1]==='edit').length;
  publishPlatform(f.directory,f.win,f.run);
  assert.equal(f.calls.filter(call=>call[1]==='edit').length,before);
  assert.equal(f.release.prerelease,false);
  assert.ok(f.assets.has(mac.receipt));
  assert.ok(f.assets.has('Caelis-Bot-1.2.3-windows-amd64-preview.publication.json'));
}));

test('draft lookup 404 recovers only the exact draft before immutable publication',()=>fixture(f=>{
  f.controls.draftTag404=true;
  const receipt=publishPlatform(f.directory,f.mac,f.run);
  assert.equal(receipt.source,f.source);
  assert.equal(f.release.draft,false);
  assert.ok(f.assets.has(receipt.receipt));
  assert.ok(f.calls.some(call=>call[1]==='api' && call[2].endsWith('/releases?per_page=100')));
}));

test('missing draft after tag lookup 404 fails before any asset upload',()=>fixture(f=>{
  f.controls.draftTag404=true;
  f.controls.hideDraft=true;
  assert.throws(()=>publishPlatform(f.directory,f.mac,f.run),/HTTP 404/);
  assert.equal(f.assets.size,0);
}));

test('interleaved platform publishers reconcile one shared draft and retain both receipts',()=>fixture(f=>{
  f.controls.concurrentOnUpload=()=>publishPlatform(f.directory,f.win,f.run);
  publishPlatform(f.directory,f.mac,f.run);
  assert.equal(f.release.draft,false);
  assert.equal(f.release.prerelease,false);
  assert.equal(f.calls.filter(call=>call[1]==='release' && call[2]==='edit').length,1);
  assert.ok(f.assets.has('Caelis-Bot-1.2.3-macos-arm64-stable.publication.json'));
  assert.ok(f.assets.has('Caelis-Bot-1.2.3-windows-amd64-preview.publication.json'));
}));

test('retry reconciles existing bytes and an unknown upload through the original release',()=>fixture(f=>{
  f.controls.unknownUpload='Caelis-Bot-1.2.3-macos-arm64.dmg';
  publishPlatform(f.directory,f.mac,f.run);
  assert.equal(f.calls.filter(call=>call[1]==='release' && call[2]==='upload' && call[4]?.endsWith('.dmg')).length,1);
  publishPlatform(f.directory,f.mac,f.run);
  const uploads=f.calls.filter(call=>call[1]==='upload').length;
  publishPlatform(f.directory,f.mac,f.run);
  assert.equal(f.calls.filter(call=>call[1]==='upload').length,uploads);
}));

test('source, immutable bytes, validation and feed source mismatch fail closed',()=>fixture(f=>{
  f.controls.source='b'.repeat(40);
  assert.throws(()=>publishPlatform(f.directory,f.mac,f.run),/source mismatch/);
  f.controls.source=f.source;
  assert.throws(()=>receiptFor(f.directory,{...f.win,validation:'packaged'}),/native acceptance/);
  writeFileSync(join(f.directory,'latest.json'),JSON.stringify({tag:f.mac.tag,source:'b'.repeat(40),sha256:'c'.repeat(64),appcastSHA256:'d'.repeat(64)}));
  assert.throws(()=>receiptFor(f.directory,f.mac),/feed source/);
}));

test('published platform assets cannot be overwritten even when another platform succeeds',()=>fixture(f=>{
  publishPlatform(f.directory,f.mac,f.run);
  const name='Caelis-Bot-1.2.3-macos-arm64.dmg';
  f.assets.set(name,Buffer.from('different bytes'));
  assert.throws(()=>publishPlatform(f.directory,f.mac,f.run),/Immutable release asset differs/);
  assert.equal(f.release.draft,false);
}));
