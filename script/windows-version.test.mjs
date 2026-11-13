import {test} from 'node:test';
import assert from 'node:assert/strict';
import {copyFileSync,mkdtempSync,mkdirSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {execFileSync,spawnSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {delimiter,join} from 'node:path';
import {windowsVersion} from './windows-version.mjs';
import {releaseVersion} from './release-version.mjs';

const hasPowerShell=spawnSync('pwsh',['-NoProfile','-Command','exit 0']).status===0;

test('Windows environment derives pins from repository files and rejects incompatible tools',{skip:!hasPowerShell},async t=>{
 const root=mkdtempSync(join(tmpdir(),'bot-windows-env-'));
 t.after(()=>rmSync(root,{recursive:true,force:true}));
 mkdirSync(join(root,'script'));
 copyFileSync(new URL('./windows-env.ps1',import.meta.url),join(root,'script/windows-env.ps1'));
 const extraGo=join(root,'extra-go');mkdirSync(extraGo);
 const goRoot=execFileSync('go',['env','GOROOT'],{encoding:'utf8',env:{...process.env,GOWORK:'off',GOTOOLCHAIN:'local'}}).trim();
 const goName=process.platform==='win32'?'go.exe':'go';
 copyFileSync(join(goRoot,'bin',goName),join(extraGo,goName));
 const pathKey=Object.keys(process.env).find(key=>key.toUpperCase()==='PATH')||'PATH';
 // Keep multiple Go installations on PATH, as on Windows CI. Mock only version
 // reporting; module parsing still uses the first installed Go binary.
 writeFileSync(join(root,'probe.ps1'),`
$ErrorActionPreference = 'Stop'
$taskRealGo = (Get-Command go -CommandType Application | Select-Object -First 1).Source
function node { $env:BOT_TEST_NODE }
function go {
  if ($args.Count -eq 1 -and $args[0] -eq 'version') { $env:BOT_TEST_GO }
  else { & $taskRealGo @args }
}
. "$PSScriptRoot/script/windows-env.ps1"
@{node=$taskNodeVersion;go=$taskGoVersion;wails=$taskWailsVersion} | ConvertTo-Json -Compress
`);
 const pins={node:'98.7.6',go:'1.20.14',wails:'v3.0.0-alpha.8'};
 const run=(versions=pins,reports={})=>{
  writeFileSync(join(root,'.node-version'),versions.node+'\n');
  writeFileSync(join(root,'go.mod'),`module example.com/windows-env-test\n\ngo ${versions.go}\n${versions.wails?`\nrequire github.com/wailsapp/wails/v3 ${versions.wails}\n`:''}`);
  return spawnSync('pwsh',['-NoProfile','-NonInteractive','-File',join(root,'probe.ps1')],{
   cwd:root,encoding:'utf8',timeout:15000,
   env:{...process.env,[pathKey]:process.env[pathKey]+delimiter+extraGo,GOWORK:'off',GOTOOLCHAIN:'local',GOPROXY:'off',
    BOT_TEST_NODE:`v${versions.node}`,BOT_TEST_GO:`go version go${versions.go} windows/amd64`,...reports},
  });
 };
 await t.test('reads alternate pins with multiple Go installations on PATH',()=>{
  for(const versions of [pins,{node:'97.6.5',go:'1.21.9',wails:'v3.0.0-alpha.9'}]){
   const result=run(versions);
   assert.equal(result.status,0,result.stdout+result.stderr);
   assert.deepEqual(JSON.parse(result.stdout),versions);
  }
 });
 for(const [name,reports,message] of [
  ['Node version',{BOT_TEST_NODE:'v98.7.5'},'Use the pinned Node 98.7.6'],
  ['Go version',{BOT_TEST_GO:'go version go1.20.13 windows/amd64'},'Use pinned Go on Windows x64'],
  ['Go architecture',{BOT_TEST_GO:'go version go1.20.14 windows/arm64'},'Use pinned Go on Windows x64'],
 ])await t.test(`rejects mismatched ${name}`,()=>{
  const result=run(pins,reports);
  assert.notEqual(result.status,0);
  assert.ok(result.stderr.includes(message),result.stdout+result.stderr);
 });
 await t.test('rejects a module without a Wails requirement',()=>{
  const result=run({...pins,wails:null});
  assert.notEqual(result.status,0);
  assert.ok(result.stderr.includes('go.mod must pin the Wails application library'),result.stdout+result.stderr);
 });
});

test('Windows build and package commands use the derived Wails CLI version',()=>{
 for(const script of ['build-windows.ps1','package-windows.ps1']){
  const source=readFileSync(new URL(script,import.meta.url),'utf8');
  assert.match(source,/Invoke-BotCommand go run "github\.com\/wailsapp\/wails\/v3\/cmd\/wails3@\$taskWailsVersion"/);
 }
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
