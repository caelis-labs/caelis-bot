import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {windowsVersion} from './windows-version.mjs';
import {releaseVersion} from './release-version.mjs';

test('Windows Wails generation and packaging use the application library version',()=>{
 const pins=JSON.parse(readFileSync(new URL('../toolchain.json',import.meta.url),'utf8'));
 const goMod=readFileSync(new URL('../go.mod',import.meta.url),'utf8');
 const libraryVersion=goMod.match(/^\s*github\.com\/wailsapp\/wails\/v3\s+(\S+)\s*$/m)?.[1];
 assert.ok(libraryVersion,'go.mod must pin the Wails application library');
 assert.equal(pins.wails,libraryVersion,'Windows build/package CLI must match the Wails library');
});

test('Windows preview qualification is independent from the accepted macOS stable tag',()=>{
 assert.equal(windowsVersion('0.14.0','preview','v0.14.0-windows.preview.3',3).version,'0.14.0-windows.preview.3');
 assert.equal(releaseVersion('0.14.0','v0.14.0').version,'0.14.0');
 assert.throws(()=>releaseVersion('0.14.0','v0.14.0-windows.preview.3'));
 for(const tag of ['v0.14.0','v0.13.0-windows.preview.3','v0.14.0-windows.preview.4'])assert.throws(()=>windowsVersion('0.14.0','preview',tag,3));
 assert.throws(()=>windowsVersion('0.14.0','development','v0.14.0'));
});

test('untagged previews use local version, Dev identity and no release updates',()=>{
 const local=windowsVersion('0.14.0','preview','',1),release=windowsVersion('0.14.0','preview','v0.14.0-windows.preview.1',1);
 assert.equal(local.version,'0.14.0-windows.preview.1-local');
 assert.equal(local.identity,'development');assert.equal(local.releaseUpdates,false);
 assert.equal(release.identity,'windows-preview');assert.equal(release.releaseUpdates,true);
 assert.notEqual(local.version,release.version);
});


test('stable builds use the product tag and installed identity; local stable candidates stay Dev',()=>{
 assert.deepEqual(windowsVersion('1.2.3','stable','v1.2.3'),{version:'1.2.3',channel:'stable',identity:'release',releaseUpdates:true});
 assert.deepEqual(windowsVersion('1.2.3','stable'),{version:'1.2.3-local',channel:'stable',identity:'development',releaseUpdates:false});
 for(const tag of ['v1.2.4','v1.2.3-windows.preview.1'])assert.throws(()=>windowsVersion('1.2.3','stable',tag));
});

test('current Windows candidate layout keeps Core and exact Setup/ZIP receipts without a second DTW payload',async()=>{
 const {mkdtempSync,mkdirSync,writeFileSync,rmSync}=await import('node:fs');
 const {join}=await import('node:path');const {tmpdir}=await import('node:os');
 const {createHash}=await import('node:crypto');const {spawnSync,execFileSync}=await import('node:child_process');
 const root=mkdtempSync(join(tmpdir(),'bot-windows-candidate-')),app=join(root,'app'),releases=join(root,'releases');
 const digest=bytes=>createHash('sha256').update(bytes).digest('hex');
 const put=(path,bytes)=>{mkdirSync(join(path,'..'),{recursive:true});writeFileSync(path,bytes);};
 const json=(path,value)=>put(path,JSON.stringify(value));
 try{
  for(const name of ['caelis-bot.exe','caelis-terminal.exe','Helpers/caelis.exe','Resources/LICENSE','Resources/ASSET-LICENSE.md'])put(join(app,name),'fixture bytes; never executed');
  const binary='fixture bytes; never executed';
  const build={...windowsVersion('0.22.0','stable'),platform:'windows-amd64',source:execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim(),sha256:digest(binary)};
  json(join(app,'build.json'),build);
  const receipts=[];
  for(const [format,suffix] of [['nsis','-Setup.exe'],['zip','.zip']]){
   const path=join(releases,'candidate'+suffix),bytes='private package fixture';put(path,bytes);
   const meta={version:build.version,source:build.source,channel:build.channel,format,qualified:false,signed:false,sha256:digest(bytes)};
   json(path+'.build.json',meta);receipts.push({path,meta});
  }
  const verify=()=>spawnSync(process.execPath,['script/verify-windows-candidate.mjs',app,releases],{encoding:'utf8',timeout:10000});
  const result=verify();assert.equal(result.status,0,result.stdout+result.stderr);
  rmSync(join(app,'Helpers/caelis.exe'));assert.notEqual(verify().status,0,'Core helper is required');put(join(app,'Helpers/caelis.exe'),binary);
  for(const patch of [{source:'wrong-source'},{sha256:'0'.repeat(64)},{identity:'release'},{releaseUpdates:true}]){
   json(join(app,'build.json'),{...build,...patch});assert.notEqual(verify().status,0,'candidate identity remains guarded');
  }
  json(join(app,'build.json'),build);
  for(const {path,meta} of receipts){
   for(const patch of [{version:'wrong'},{source:'wrong'},{channel:'preview'},{format:'wrong'},{qualified:true},{signed:true},{sha256:'0'.repeat(64)}]){
    json(path+'.build.json',{...meta,...patch});assert.notEqual(verify().status,0,'package receipt remains exact');
   }
   json(path+'.build.json',meta);
  }
  assert.equal(verify().status,0);
 }finally{rmSync(root,{recursive:true,force:true});}
});
