import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,readFileSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,basename} from 'node:path';
import {digest} from './update-manifest.mjs';
import {windowsRelease} from './windows-version.mjs';
import {publishWindows} from './windows-publish.mjs';
import {publishWindowsR2,compareWindows,windowsCleanupKeys} from './windows-publish-r2.mjs';
function fixture(tag,fn){
 const dir=mkdtempSync(join(tmpdir(),'windows-publication-')),r=windowsRelease(tag),source='a'.repeat(40),assets=new Map(),objects=new Map(),calls=[],controls={};
 for(const [format,suffix] of [['nsis','-Setup.exe'],['zip','.zip']]){
  const name=`Caelis-Bot-${r.version}-windows-amd64-${r.channel}${suffix}`,bytes=Buffer.from(format),sha256=digest(bytes);
  for(const [file,value] of [[name,bytes],[name+'.sha256',`${sha256}  ${name}\n`],[name+'.build.json',JSON.stringify({schema:1,version:r.version,channel:r.channel,platform:'windows-amd64',format,source,dirty:false,signed:false,signingPolicy:`unsigned-${r.channel}`,sha256})]])writeFileSync(join(dir,file),value);
 }
 let release={tag_name:tag,draft:true,prerelease:r.channel==='preview'};
 const run=(cmd,args)=>{
  calls.push([cmd,...args]);const value=key=>args[args.indexOf(key)+1];
  if(cmd==='gh'){
   if(args[0]==='api'){
    if(args[1].includes('/commits/'))return JSON.stringify({sha:source});
    return JSON.stringify({...release,assets:[...assets.keys()].map(name=>({name}))});
   }
   if(args[1]==='upload'){
    const name=basename(args[3]);assets.set(name,readFileSync(args[3]));
    if(controls.lostUpload)throw Error('connection reset after upload');
   }else if(args[1]==='download')writeFileSync(join(value('--dir'),value('--pattern')),assets.get(value('--pattern')));
   else if(args[1]==='edit')release.draft=false;
   else throw Error('unexpected GitHub command');
   return '';
  }
  if(args[1]==='list-objects-v2')return JSON.stringify({Contents:[...objects.keys()].map(Key=>({Key}))});
  if(args[1]==='cp'){
   const key=args[3].replace('s3://caelis-releases/','');
   if(controls.failWrite)throw Error('upload failed');
   objects.set(key,readFileSync(args[2]));if(controls.lostWrite)throw Error('connection reset after put');return '';
  }
  if(args[1]==='get-object'){
   const key=value('--key');if(!objects.has(key))throw Error('missing object');
   writeFileSync(args[args.indexOf('--key')+2],controls.corrupt?Buffer.from('wrong'):objects.get(key));return '{}';
  }
  if(args[1]==='delete-object'){objects.delete(value('--key'));return '{}';}
  throw Error('unexpected command');
 };
 const env={BOT_RELEASE_TAG:tag,GITHUB_REPOSITORY:'caelis-labs/caelis-bot',R2_ENDPOINT:'https://example.test'};
 try{fn({dir,tag,source,assets,objects,calls,controls,run,env,release});}finally{rmSync(dir,{recursive:true,force:true});}
}
for(const tag of ['v1.2.3','v1.2.3-windows.preview.2'])test(`${tag} reconciles lost uploads, preserves other platform assets and retries without overwriting`,()=>fixture(tag,f=>{
 f.assets.set('macos-existing.dmg',Buffer.from('mac'));f.controls.lostUpload=true;
 assert.match(publishWindows(f.dir,f.tag,f.source,false,f.run),/Published/);
 const uploads=f.calls.filter(c=>c[2]==='upload').length;assert.equal(uploads,6);
 assert.match(publishWindows(f.dir,f.tag,f.source,false,f.run),/Published/);
 assert.equal(f.calls.filter(c=>c[2]==='upload').length,uploads);
 assert.equal(f.assets.get('macos-existing.dmg').toString(),'mac');
 assert.equal(f.calls.filter(c=>c[2]==='edit').length,1);
 const name=[...f.assets.keys()].find(k=>k.endsWith('.zip'));f.assets.set(name,Buffer.from('tamper'));
 assert.throws(()=>publishWindows(f.dir,f.tag,f.source,false,f.run),/Immutable/);
}));
test('Windows R2 advances only after readback, keeps Mac and removes prior preview on stable promotion',()=>fixture('v1.2.3',f=>{
 publishWindows(f.dir,f.tag,f.source,false,f.run);
 const old='caelis-bot/releases/windows-amd64/v1.2.3-windows.preview.1/Caelis-Bot-1.2.3-windows.preview.1-windows-amd64-preview.zip';
 const mac='caelis-bot/releases/v1.0.0/Caelis-Bot-1.0.0-macos-arm64.dmg';
 f.objects.set(old,Buffer.from('old'));f.objects.set(mac,Buffer.from('mac'));f.controls.failWrite=true;
 assert.throws(()=>publishWindowsR2(f.dir,f.env,f.run),/upload failed/);assert.ok(f.objects.has(old));
 f.controls.failWrite=false;f.controls.lostWrite=true;
 assert.match(publishWindowsR2(f.dir,f.env,f.run),/Published/);assert.ok(!f.objects.has(old));assert.ok(f.objects.has(mac));
 assert.match(publishWindowsR2(f.dir,f.env,f.run),/Published/);
 const pointer=JSON.parse(f.objects.get('caelis-bot/downloads/windows-amd64/latest.json'));assert.equal(pointer.tag,f.tag);
 pointer.tag='v2.0.0';f.objects.set('caelis-bot/downloads/windows-amd64/latest.json',Buffer.from(JSON.stringify(pointer)));
 assert.match(publishWindowsR2(f.dir,f.env,f.run),/Newer/);
}));
test('R2 corrupt readback cannot publish a download pointer or prune history',()=>fixture('v1.2.3-windows.preview.2',f=>{
 publishWindows(f.dir,f.tag,f.source,false,f.run);f.controls.corrupt=true;
 assert.throws(()=>publishWindowsR2(f.dir,f.env,f.run),/readback mismatch/);
 assert.ok(!f.objects.has('caelis-bot/downloads/windows-amd64/latest.json'));
 assert.ok(!f.calls.some(c=>c[2]==='delete-object'));
}));
test('retention orders product baselines then stable over preview, and rejects unknown owned objects',()=>{
 assert.equal(compareWindows('v1.10.0-windows.preview.1','v1.9.0'),1);
 assert.equal(compareWindows('v1.2.3','v1.2.3-windows.preview.999'),1);
 assert.equal(compareWindows('v1.2.3-windows.preview.10','v1.2.3-windows.preview.2'),1);
 assert.throws(()=>windowsCleanupKeys(['caelis-bot/releases/windows-amd64/v1.2.3/private.txt'],'v1.2.3'),/Unexpected/);
});
