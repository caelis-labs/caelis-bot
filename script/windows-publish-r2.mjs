import {existsSync,mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {windowsRelease} from './windows-version.mjs';
import {publicationPlan} from './windows-publish.mjs';
import {compareStable} from './publish-r2.mjs';
import {releaseCommand} from './release-network.mjs';
import {r2Client} from './r2-client.mjs';
const prefix='caelis-bot/releases/windows-amd64/';
const pointer='caelis-bot/downloads/windows-amd64/latest.json';
export function compareWindows(a,b){
 const aa=windowsRelease(a),bb=windowsRelease(b);
 return compareStable(`v${aa.base}`,`v${bb.base}`)||(aa.channel!==bb.channel?(aa.channel==='stable'?1:-1):Math.sign(aa.revision-bb.revision));
}
export function windowsCleanupKeys(keys,tag){
 windowsRelease(tag);
 const owned=keys.filter(k=>k.startsWith(prefix));
 for(const key of owned){
  const parts=key.slice(prefix.length).split('/');
  if(parts.length!==2)throw Error(`Unexpected Windows object: ${key}`);
  const r=windowsRelease(parts[0]);
  const start=`Caelis-Bot-${r.version}-windows-amd64-${r.channel}`;
  const allowed=['-Setup.exe','.zip'].flatMap(suffix=>['','.sha256','.build.json'].map(sidecar=>start+suffix+sidecar)).concat(['windows-update.json','windows-update.json.sig']);
  if(!allowed.includes(parts[1]))throw Error(`Unexpected Windows object: ${key}`);
 }
 return owned.filter(k=>compareWindows(k.slice(prefix.length).split('/')[0],tag)<0);
}
export function publishWindowsR2(directory,env=process.env,run=(cmd,args)=>releaseCommand(cmd,args,{env})){
 const tag=env.BOT_RELEASE_TAG,r=windowsRelease(tag);
 if(env.GITHUB_REPOSITORY!=='caelis-labs/caelis-bot')throw Error('Unexpected repository');
 const release=JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/releases/tags/${tag}`]));
 if(release.tag_name!==tag||release.draft||release.prerelease!==(r.channel==='preview'))throw Error('Windows release is not published in the expected channel');
 const source=JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/commits/${tag}`])).sha;
 const updates=existsSync(join(directory,'windows-update.json'))||existsSync(join(directory,'windows-update.json.sig'));
 const {manifest,assets}=publicationPlan(directory,tag,source,updates);
 if(assets.some(name=>!release.assets?.some(a=>a.name===name)))throw Error('Windows GitHub publication is incomplete');
 const temporary=mkdtempSync(join(tmpdir(),'caelis-windows-r2-'));
 try{
  const r2=r2Client(directory,temporary,env,run),existing=r2.list();
  windowsCleanupKeys(existing,tag);
  if(existing.includes(pointer)){
   const previous=JSON.parse(r2.read(pointer));
   if(previous.schema!==1||previous.platform!=='windows-amd64')throw Error('Invalid Windows download pointer');
   if(compareWindows(previous.tag,tag)>0)return 'Newer Windows R2 release exists: R2 unchanged';
   if(previous.tag===tag&&(previous.source!==source||JSON.stringify(previous.artifacts)!==JSON.stringify(manifest.artifacts)))throw Error('Refusing to replace published Windows version');
  }
  const versioned=prefix+tag+'/';
  for(const file of assets)r2.put(file,versioned+file,file.endsWith('.zip')?'application/zip':file.endsWith('.exe')?'application/octet-stream':'text/plain',true,existing);
  const download={schema:1,platform:'windows-amd64',tag,source,channel:r.channel,artifacts:manifest.artifacts,baseURL:`https://releases.caelis.dev/${versioned}`};
  // This is a download index, never an unsigned automatic-update authority.
  writeFileSync(join(directory,'windows-download.json'),JSON.stringify(download,null,2)+'\n');
  r2.put('windows-download.json',pointer,'application/json',false,existing);
  const obsolete=windowsCleanupKeys(r2.list(),tag);
  r2.remove(obsolete);
  return `Published Windows ${tag}; retained latest baseline, removed ${obsolete.length} older objects`;
 }finally{rmSync(temporary,{recursive:true,force:true});}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 console.log(publishWindowsR2(resolve(process.argv[2]||'release')));
}
