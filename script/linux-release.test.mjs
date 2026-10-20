import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync,createHash} from 'node:crypto';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,chmodSync,statSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join,basename} from 'node:path';
import {create} from './linux-update-manifest.mjs';
import {prepareLinux,verifyLinux,distributionNames,linuxNames} from './linux-release.mjs';
import {publishLinux} from './linux-publish.mjs';
import {publishLinuxR2,linuxCleanupKeys} from './linux-publish-r2.mjs';
import {cleanupKeys} from './publish-r2.mjs';
const sha=b=>createHash('sha256').update(b).digest('hex');
test('QA bootstrap signs actual launcher, installer, package and notices; tampering fails closed',t=>{
 const dir=mkdtempSync(join(tmpdir(),'caelis-linux-sign-fixture-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));
 const root=join(dir,'package');mkdirSync(join(root,'Helpers'),{recursive:true});mkdirSync(join(root,'Resources'));
 const binary=Buffer.alloc(64);binary.set(Buffer.from('7f454c460201','hex'));binary.writeUInt16LE(183,18);
 for(const name of ['caelisbot','Helpers/caelis']){writeFileSync(join(root,name),binary);chmodSync(join(root,name),0o755)}
 const pin={version:'v0.72.2',commit:'b'.repeat(40),targets:{'linux-arm64':{binary_sha256:sha(binary)}}};
 const notices=['LICENSE','ASSET-LICENSE.md','Resources/Caelis-Runtime-LICENSE','Resources/Caelis-Protocol-LICENSE','Resources/OpenClaw-Weixin-LICENSE','Resources/Go-QRCode-LICENSE'];
 for(const name of notices){writeFileSync(join(root,name),'fixture notice');chmodSync(join(root,name),0o644)}
 writeFileSync(join(root,'Resources/Caelis-Runtime.json'),JSON.stringify(pin));chmodSync(join(root,'Resources/Caelis-Runtime.json'),0o644);
 const n=linuxNames('0.17.1-qa.9','arm64');writeFileSync(join(dir,n.archive),'fixture archive');writeFileSync(join(dir,n.launcher),binary);
 writeFileSync(join(dir,n.manifest),JSON.stringify(create(root,join(dir,n.archive),'0.17.1-qa.9','a'.repeat(40),'linux-arm64','qa')));
 const {publicKey,privateKey}=generateKeyPairSync('ed25519'),pub=join(dir,'qa.pub'),priv=join(dir,'qa.key');
 writeFileSync(pub,publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64'));writeFileSync(priv,privateKey.export({format:'pem',type:'pkcs8'}),{mode:0o600});
 // Exercise the actual CI artifact extraction -> prepare path as a non-root
 // signing process with umask 077. The ELF/package bytes remain a QA fixture.
 const unsigned=join(dir,'unsigned.tar.gz'),extracted=join(dir,'extracted');
 execFileSync('tar',['-czf',unsigned,'-C',dir,'package',n.archive,n.launcher,n.manifest]);
 mkdirSync(extracted);
 execFileSync('bash',['-c','umask 077; bash script/extract-linux-build.sh "$1" "$2"; node --input-type=module - "$2" "$3" "$4" <<\'JS\'\nimport {prepareLinux} from "./script/linux-release.mjs"; const [dir,key,pub]=process.argv.slice(2); prepareLinux(dir,"arm64",key,pub);\nJS','sign-fixture',unsigned,extracted,priv,pub]);
 assert.equal(statSync(join(extracted,'package/caelisbot')).mode&0o777,0o755);
 assert.equal(statSync(join(extracted,'package/LICENSE')).mode&0o777,0o644);
 assert.equal(statSync(priv).mode&0o777,0o600);
 assert.equal(verifyLinux(extracted,'arm64',pub,true).index.assets.length,7);
 const release=prepareLinux(dir,'arm64',priv,pub);assert.equal(release.names.length,9);
 assert.throws(()=>verifyLinux(dir,'arm64',pub),/QA is not a release/);
 for(const name of [n.launcher,n.installer,n.script,n.archive,n.checksum,n.manifest+'.sig']){
  const original=readFileSync(join(dir,name));writeFileSync(join(dir,name),Buffer.concat([original,Buffer.from('tamper')]));
  assert.throws(()=>verifyLinux(dir,'arm64',pub,true));writeFileSync(join(dir,name),original);
 }
 assert.equal(verifyLinux(dir,'arm64',pub,true).index.assets.length,7);
});
function publicationFixture(t){
 const dir=mkdtempSync(join(tmpdir(),'caelis-linux-publish-fixture-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));
 const version='1.2.3',tag='v'+version,source='a'.repeat(40),assets=new Map(),objects=new Map(),calls=[],controls={};
 const targets=new Map();
 for(const arch of ['amd64','arm64']){
  const names=distributionNames(version,arch),index={version,source,platform:'linux-'+arch};
  for(const name of names)writeFileSync(join(dir,name),name.endsWith('-install.json')?JSON.stringify(index):'fixture '+name);
  targets.set(arch,{names,index});
 }
 const release={tag_name:tag,draft:true,prerelease:false};
 const run=(cmd,args)=>{calls.push([cmd,...args]);const value=k=>args[args.indexOf(k)+1];
  if(cmd==='gh'){
   if(args[0]==='api')return JSON.stringify(args[1].includes('/commits/')?{sha:source}:{...release,assets:[...assets.keys()].map(name=>({name}))});
   if(args[1]==='upload'){assets.set(basename(args[3]),readFileSync(args[3]));if(controls.lostUpload)throw Error('lost response');return ''}
   if(args[1]==='download'){writeFileSync(join(value('--dir'),value('--pattern')),assets.get(value('--pattern')));return ''}
   if(args[1]==='edit'){release.draft=false;return ''}
  }
  if(cmd==='aws'){
   if(args[1]==='list-objects-v2')return JSON.stringify({Contents:[...objects.keys()].map(Key=>({Key}))});
   if(args[1]==='cp'){const key=args[3].replace('s3://caelis-releases/','');objects.set(key,readFileSync(args[2]));if(controls.lostPut)throw Error('lost put');return ''}
   if(args[1]==='get-object'){const key=value('--key');if(!objects.has(key))throw Error('missing');writeFileSync(args[args.indexOf('--key')+2],controls.corrupt?Buffer.from('bad'):objects.get(key));return '{}'}
   if(args[1]==='delete-object'){objects.delete(value('--key'));return '{}'}
  }
  throw Error('unexpected command '+cmd+args.join(' '));
 };
 return {dir,tag,source,assets,objects,calls,controls,run,verify:(_,arch)=>targets.get(arch),pointer:p=>JSON.parse(readFileSync(p)),env:{BOT_RELEASE_TAG:tag,GITHUB_REPOSITORY:'caelis-labs/caelis-bot',R2_ENDPOINT:'https://fixture.invalid'}};
}
test('GitHub immutable readback reconciles unknown upload without resending',t=>{
 const f=publicationFixture(t);f.controls.lostUpload=true;
 assert.match(publishLinux(f.dir,f.tag,f.run,f.verify),/Published/);const uploads=f.calls.filter(c=>c[2]==='upload').length;assert.equal(uploads,16);
 publishLinux(f.dir,f.tag,f.run,f.verify);assert.equal(f.calls.filter(c=>c[2]==='upload').length,uploads);
 f.assets.set([...f.assets.keys()][0],Buffer.from('tamper'));assert.throws(()=>publishLinux(f.dir,f.tag,f.run,f.verify),/Immutable/);
});
test('R2 mirrors full two-arch inventories, readback before pointers; platform retention isolated',t=>{
 const f=publicationFixture(t);publishLinux(f.dir,f.tag,f.run,f.verify);f.controls.lostPut=true;
 const mac='caelis-bot/releases/v1.0.0/Caelis-Bot-1.0.0-macos-arm64.dmg',win='caelis-bot/releases/windows-amd64/v1.0.0/Caelis-Bot-1.0.0-windows-amd64-stable.zip';
 f.objects.set(mac,Buffer.from('mac'));f.objects.set(win,Buffer.from('win'));
 const old='caelis-bot/releases/linux-arm64/v1.0.0/linux-arm64-update.json';f.objects.set(old,Buffer.from('old'));
 assert.match(publishLinuxR2(f.dir,f.env,f.run,f.verify,f.pointer),/Published/);
 assert(f.objects.has(mac)&&f.objects.has(win));assert(!f.objects.has(old));
 for(const arch of ['amd64','arm64'])for(const name of distributionNames('1.2.3',arch))assert(f.objects.has(`caelis-bot/releases/linux-${arch}/v1.2.3/${name}`));
 const puts=f.calls.filter(c=>c[2]==='cp').map(c=>c[4]);assert.equal(puts.at(-1),'s3://caelis-releases/caelis-bot/install.sh');
 const firstPointer=puts.findIndex(k=>k.includes('/downloads/'));assert.equal(firstPointer,18);
 publishLinuxR2(f.dir,f.env,f.run,f.verify,f.pointer);
 f.controls.corrupt=true;assert.throws(()=>publishLinuxR2(f.dir,f.env,f.run,f.verify,f.pointer));
 assert.deepEqual(linuxCleanupKeys([mac,win,old],'v1.2.3','arm64'),[old]);
 assert.doesNotThrow(()=>cleanupKeys([mac,win,old],'v1.2.3'));
});
