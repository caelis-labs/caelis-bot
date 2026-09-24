import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync, sign} from 'node:crypto';
import {mkdtempSync, readFileSync, writeFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {publish, cleanupKeys, compareStable} from './publish-r2.mjs';
import {digest, verifyDirectory} from './update-manifest.mjs';
import {validateUpdateKey} from './configure-updates.mjs';

function fixture(fn) {
  const directory=mkdtempSync(join(tmpdir(),'bot-updates-test-'));
  const {publicKey,privateKey}=generateKeyPairSync('ed25519');
  const key=publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
  const tag='v1.2.3', file='Caelis-Bot-1.2.3-macos-arm64.dmg', source='a'.repeat(40);
  const bytes=Buffer.from('notarized DMG fixture');
  const body=Buffer.from(`<rss><channel><item><sparkle:version>1.2.3</sparkle:version><enclosure url="https://releases.caelis.dev/caelis-bot/releases/${tag}/${file}" length="${bytes.length}" sparkle:edSignature="${sign(null,bytes,privateKey).toString('base64')}"></enclosure></item></channel></rss>`);
  const xml=Buffer.concat([body,Buffer.from(`<!-- sparkle-signatures:\nedSignature: ${sign(null,body,privateKey).toString('base64')}\nlength: ${body.length}\n-->\n`)]);
  writeFileSync(join(directory,file),bytes);
  writeFileSync(join(directory,`${file}.sha256`),`${digest(bytes)}  ${file}\n`);
  writeFileSync(join(directory,'appcast.xml'),xml);
  const manifest={schema:1,tag,version:'1.2.3',file,source,length:bytes.length,sha256:digest(bytes),appcastSHA256:digest(xml)};
  const metadata=Buffer.from(JSON.stringify(manifest));
  writeFileSync(join(directory,'latest.json'),metadata);
  writeFileSync(join(directory,'latest.json.sig'),sign(null,metadata,privateKey).toString('base64'));
  const env={BOT_RELEASE_TAG:tag,BOT_SPARKLE_PUBLIC_KEY:key,GITHUB_REPOSITORY:'caelis-labs/caelis-bot',R2_ENDPOINT:'https://example.r2.cloudflarestorage.com'};
  const objects=new Map([['caelis-bot/releases/v1.0.0/Caelis-Bot-1.0.0-macos-arm64.dmg',Buffer.from('old')],['releases/v1.0.0/caelis.tar.gz',Buffer.from('core')]]);
  const calls=[];
  const controls={};
  let checks=0;
  const run=(cmd,args)=>{
    calls.push([cmd,...args]);
    if(cmd==='gh') {
      if(args[1].endsWith('/latest')) return JSON.stringify({tag_name:++checks===2&&controls.moved?'v1.3.0':controls.latest??tag,draft:false,prerelease:false});
      return JSON.stringify({sha:controls.source??source});
    }
    const value=name=>args[args.indexOf(name)+1];
    if(args[1]==='list-objects-v2') {
      if(controls.listFail) throw new Error('listing failed');
      return JSON.stringify({Contents:[...objects.keys()].filter(k=>k.startsWith('caelis-bot/')).map(Key=>({Key}))});
    }
    if(args[1]==='cp') {
      const key=args[3].replace('s3://caelis-releases/','');
      if(controls.failKey===key) throw new Error('upload failed');
      objects.set(key,readFileSync(args[2]));return '';
    }
    if(args[1]==='get-object') {
      const key=value('--key'), target=args[args.indexOf('--key')+2];
      assert.ok(objects.has(key),key);
      writeFileSync(target,controls.corrupt===key?Buffer.from('corrupt'):objects.get(key));return '{}';
    }
    if(args[1]==='delete-object') { objects.delete(value('--key'));return '{}'; }
    throw new Error('Unexpected command');
  };
  try { fn({directory,key,tag,file,env,objects,calls,controls,run,manifest,metadata}); }
  finally {rmSync(directory,{recursive:true,force:true});}
}

test('R2 commits only verified bytes, then cleans only Bot versions; retry is idempotent',()=>fixture(f=>{
  assert.match(publish(f.directory,f.env,f.run),/Published/);
  assert.ok(f.objects.has('releases/v1.0.0/caelis.tar.gz'));
  assert.ok(![...f.objects.keys()].some(k=>k.startsWith('caelis-bot/releases/v1.0.0/')));
  assert.ok(f.objects.has('caelis-bot/appcast.xml'));
  const deletion=f.calls.findIndex(c=>c[2]==='delete-object');
  const feed=f.calls.findIndex(c=>c[2]==='get-object'&&c.includes('caelis-bot/appcast.xml'));
  assert.ok(deletion>feed);
  assert.match(publish(f.directory,f.env,f.run),/Published/);
}));

for(const stage of ['caelis-bot/releases/v1.2.3/Caelis-Bot-1.2.3-macos-arm64.dmg','caelis-bot/appcast.xml','caelis-bot/latest.json']) {
  test(`failed upload ${stage} preserves old release`,()=>fixture(f=>{
    f.controls.failKey=stage;
    assert.throws(()=>publish(f.directory,f.env,f.run),/upload failed/);
    assert.ok(!f.calls.some(c=>c[2]==='delete-object'));
    assert.ok([...f.objects.keys()].some(k=>k.startsWith('caelis-bot/releases/v1.0.0/')));
  }));
}
test('corrupt readback cannot switch feed or delete previous release',()=>fixture(f=>{
  f.controls.corrupt=`caelis-bot/releases/v1.2.3/${f.file}`;
  assert.throws(()=>publish(f.directory,f.env,f.run),/readback mismatch/);
  assert.ok(!f.objects.has('caelis-bot/appcast.xml'));
  assert.ok(!f.calls.some(c=>c[2]==='delete-object'));
}));
test('changed GitHub latest during upload preserves the active feed',()=>fixture(f=>{
  f.controls.moved=true;
  assert.match(publish(f.directory,f.env,f.run),/latest changed/);
  assert.ok(!f.objects.has('caelis-bot/appcast.xml'));
  assert.ok(!f.calls.some(c=>c[2]==='delete-object'));
}));
test('old queued job and preview cannot mutate R2',()=>fixture(f=>{
  f.controls.latest='v1.3.0';
  assert.match(publish(f.directory,f.env,f.run),/unchanged/);
  assert.ok(!f.calls.some(c=>c[0]==='aws'));
  assert.match(publish(f.directory,{...f.env,BOT_RELEASE_TAG:'v1.3.0-preview.1'},f.run),/unchanged/);
}));
test('R2 refuses rollback even if GitHub latest was moved backwards',()=>fixture(f=>{
  f.objects.set('caelis-bot/latest.json',Buffer.from(JSON.stringify({...f.manifest,tag:'v2.0.0',version:'2.0.0',file:'Caelis-Bot-2.0.0-macos-arm64.dmg'})));
  assert.match(publish(f.directory,f.env,f.run),/Newer R2/);
  assert.ok(!f.calls.some(c=>c[2]==='cp'));
}));
test('a retry cannot overwrite different bytes at an immutable version URL',()=>fixture(f=>{
  const key=`caelis-bot/releases/${f.tag}/${f.file}`;
  f.objects.set(key,Buffer.from('different published bytes'));
  assert.throws(()=>publish(f.directory,f.env,f.run),/immutable bytes/);
  assert.equal(f.objects.get(key).toString(),'different published bytes');
  assert.ok(!f.calls.some(c=>c[2]==='cp'||c[2]==='delete-object'));
}));
test('listing failures and unexpected keys fail before mutation',()=>fixture(f=>{
  f.controls.listFail=true;assert.throws(()=>publish(f.directory,f.env,f.run),/listing failed/);
  f.controls.listFail=false;f.objects.set('caelis-bot/releases/v1.0.0/user-file',Buffer.from('unowned'));
  assert.throws(()=>publish(f.directory,f.env,f.run),/Unexpected object/);
  assert.ok(!f.calls.some(c=>c[2]==='cp'||c[2]==='delete-object'));
}));
test('wrong public key, tampered manifest and changed DMG all fail verification',()=>fixture(f=>{
  const wrong=generateKeyPairSync('ed25519').publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
  assert.throws(()=>verifyDirectory(f.directory,f.tag,wrong),/signature/);
  writeFileSync(join(f.directory,'latest.json'),Buffer.from(JSON.stringify({...f.manifest,tag:'v9.0.0'})));
  assert.throws(()=>publish(f.directory,f.env,f.run),/signature/);
  writeFileSync(join(f.directory,'latest.json'),f.metadata);
  writeFileSync(join(f.directory,f.file),'modified');
  assert.throws(()=>publish(f.directory,f.env,f.run),/digest mismatch/);
  assert.equal(f.calls.length,0);
}));
test('cleanup ownership and stable numeric ordering',()=>{
  assert.deepEqual(cleanupKeys(['releases/v1.0.0/caelis.tar.gz','caelis-bot/appcast.xml'],'v1.2.3'),[]);
  assert.throws(()=>cleanupKeys(['caelis-bot/releases/v1.0.0/../../elsewhere'],'v1.2.3'));
  assert.equal(compareStable('v1.10.0','v1.9.9'),1);
  assert.equal(compareStable('v1.2.3','v1.2.3'),0);
  assert.equal(validateUpdateKey('',false),'');
  assert.throws(()=>validateUpdateKey('',true));
});
