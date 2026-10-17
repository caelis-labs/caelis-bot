import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync,createHash} from 'node:crypto';
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {create,qualify,verifyPublication} from './windows-update-manifest.mjs';

test('preview publication binds native evidence, source, both artifacts and the embedded update key',()=>{
 const dir=mkdtempSync(join(tmpdir(),'bot-windows-feed-'));
 try {
  const tag='v0.14.0-windows.preview.2',version=tag.slice(1),source='1'.repeat(40);
  const pair=generateKeyPairSync('ed25519'),key=join(dir,'test-only.pem'),pub=join(dir,'public.txt');
  writeFileSync(key,pair.privateKey.export({format:'pem',type:'pkcs8'}));
  writeFileSync(pub,pair.publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64'));
  const artifacts=[['nsis','-Setup.exe'],['zip','.zip']].map(([format,suffix])=>{
   const file=`Caelis-Bot-${version}-windows-amd64-preview${suffix}`,bytes=Buffer.from(`synthetic ${format}`),sha256=createHash('sha256').update(bytes).digest('hex');
   writeFileSync(join(dir,file),bytes);writeFileSync(join(dir,file+'.build.json'),JSON.stringify({schema:1,version,source,channel:'preview',platform:'windows-amd64',format,dirty:false,signed:false,signingPolicy:'unsigned-preview',sha256}));
   return {format,file,sha256,length:bytes.length};
  });
  const acceptance=join(dir,'acceptance.json'),record={schema:1,tag,source,channel:'preview',platform:'windows-amd64',os:'synthetic only',testedAt:'test',artifacts,checks:Object.fromEntries(['nativeShell','runtimeAndComputerUse','activeTaskHandoff','oldClientReconnect','setupLifecycle','zipLifecycle','autostart','dataPreserved'].map(key=>[key,true]))};
  writeFileSync(acceptance,JSON.stringify({...record,checks:{...record.checks,oldClientReconnect:false}}));
  assert.throws(()=>qualify(dir,tag,source,acceptance),/acceptance/);
  writeFileSync(acceptance,JSON.stringify(record));
  assert.throws(()=>qualify(dir,'v0.14.0',source,acceptance),/preview tag/);
  assert.throws(()=>qualify(dir,tag,'2'.repeat(40),acceptance),/Exact clean/);
  const second=generateKeyPairSync('ed25519'),wrong=join(dir,'wrong.pem');writeFileSync(wrong,second.privateKey.export({format:'pem',type:'pkcs8'}));
  assert.throws(()=>create(dir,tag,source,acceptance,wrong,pub),/differs/);
  create(dir,tag,source,acceptance,key,pub);
  assert.equal(verifyPublication(dir,tag,source,pub).qualified,true);
  assert.throws(()=>create(dir,tag,source,acceptance,key,pub),/Immutable/);
  const archive=join(dir,artifacts[1].file),original=readFileSync(archive);writeFileSync(archive,'modified');
  assert.throws(()=>verifyPublication(dir,tag,source,pub),/Exact clean/);writeFileSync(archive,original);
  writeFileSync(join(dir,'windows-update.json'),readFileSync(join(dir,'windows-update.json'),'utf8').replace(source,'3'.repeat(40)));
  assert.throws(()=>verifyPublication(dir,tag,source,pub),/signature/);
 } finally {rmSync(dir,{recursive:true,force:true});}
});
