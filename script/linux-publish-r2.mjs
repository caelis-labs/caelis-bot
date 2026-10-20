import {mkdtempSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {verifyLinux,distributionNames,linuxNames} from './linux-release.mjs';
import {validateTag} from './release-version.mjs';
import {compareStable} from './publish-r2.mjs';
import {r2Client} from './r2-client.mjs';
import {releaseCommand} from './release-network.mjs';
export function linuxCleanupKeys(keys,tag,arch){
 validateTag(tag);const prefix=`caelis-bot/releases/linux-${arch}/`;linuxNames('1.0.0',arch);
 const owned=keys.filter(k=>k.startsWith(prefix));
 for(const key of owned){const [version,name,...extra]=key.slice(prefix.length).split('/');if(extra.length||!distributionNames(validateTag(version),arch).includes(name))throw Error('Unexpected Linux object: '+key)}
 return owned.filter(k=>compareStable(k.slice(prefix.length).split('/')[0],tag)<0);
}
export function publishLinuxR2(directory,env=process.env,run=(cmd,args)=>releaseCommand(cmd,args,{env}),verify=verifyLinux,readPointer=verifyPointer){
 const tag=env.BOT_RELEASE_TAG,version=validateTag(tag);
 if(env.GITHUB_REPOSITORY!=='caelis-labs/caelis-bot')throw Error('Unexpected repository');
 const release=JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/releases/tags/${tag}`]));
 if(release.tag_name!==tag||release.draft||release.prerelease)throw Error('Expected published stable release');
 const source=JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/commits/${tag}`])).sha;
 const targets=['amd64','arm64'].map(arch=>({arch,...verify(directory,arch)}));
 if(targets.some(t=>t.index.version!==version||t.index.source!==source||t.names.some(name=>!release.assets?.some(a=>a.name===name))))throw Error('Incomplete Linux GitHub publication');
 const temp=mkdtempSync(join(tmpdir(),'caelis-linux-r2-'));
 try{
  const r2=r2Client(directory,temp,env,run),existing=r2.list();
  for(const {arch} of targets){
   linuxCleanupKeys(existing,tag,arch);
   const pointer=`caelis-bot/downloads/linux-${arch}/latest.json`;
   if(existing.includes(pointer)){
    const bytes=r2.read(pointer),sig=r2.read(pointer+'.sig');
    writeFileSync(join(temp,`linux-${arch}-install.json`),bytes);writeFileSync(join(temp,`linux-${arch}-install.json.sig`),sig);
    // A pointer is update authority only after Ed25519 verification.
    const previous=readPointer(join(temp,`linux-${arch}-install.json`));
    if(previous.platform!==`linux-${arch}`)throw Error('Invalid Linux pointer');
    if(compareStable('v'+previous.version,tag)>0)return 'Newer Linux R2 release exists: R2 unchanged';
    if(previous.version===version&&JSON.stringify(previous)!==JSON.stringify(targets.find(t=>t.arch===arch).index))throw Error('Refusing to replace published Linux identity');
   }
  }
  // All immutable assets for both architectures first. Helpers use exact-byte
  // readback even after an unknown upload response; no blind upload retry.
  for(const target of targets)for(const name of target.names)r2.put(name,`caelis-bot/releases/linux-${target.arch}/${tag}/${name}`,'application/octet-stream',true,existing);
  for(const {arch} of targets){const n=linuxNames(version,arch),pointer=`caelis-bot/downloads/linux-${arch}/latest.json`;
   r2.put(n.bootstrap+'.sig',pointer+'.sig','text/plain',false,existing);
   r2.put(n.bootstrap,pointer,'application/json',false,existing);
  }
  // A temporary pointer/signature mismatch fails closed. Bootstrap moves last.
  r2.put(linuxNames(version,'amd64').script,'caelis-bot/install.sh','text/x-shellscript',false,existing);
  for(const {arch} of targets)r2.remove(linuxCleanupKeys(r2.list(),tag,arch));
  return `Published Linux ${tag}; both signed architecture baselines retained`;
 }finally{rmSync(temp,{recursive:true,force:true})}
}
import {verifyManifest} from './linux-update-manifest.mjs';
function verifyPointer(file){return verifyManifest(file,'internal/managedupdate/release-key.txt')}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href)console.log(publishLinuxR2(resolve(process.argv[2]||'release')));
