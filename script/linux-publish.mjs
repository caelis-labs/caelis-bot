import {mkdtempSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash} from 'node:crypto';
import {verifyLinux} from './linux-release.mjs';
import {validateTag} from './release-version.mjs';
import {releaseCommand} from './release-network.mjs';
const repo='caelis-labs/caelis-bot',digest=b=>createHash('sha256').update(b).digest('hex');
// Same immutable upload/readback behavior as existing desktop publication.
export function publishLinux(directory,tag,run=(cmd,args)=>releaseCommand(cmd,args),verify=verifyLinux){
 const version=validateTag(tag),source=JSON.parse(run('gh',['api',`repos/${repo}/commits/${tag}`])).sha;
 const targets=['amd64','arm64'].map(arch=>verify(directory,arch));
 if(targets.some(t=>t.index.version!==version||t.index.source!==source))throw Error('Tag source differs from signed Linux package');
 const readRelease=()=>{
  try{return JSON.parse(run('gh',['api',`repos/${repo}/releases/tags/${tag}`]))}catch(error){
   if(!String(error.stderr||error).includes('HTTP 404'))throw error;
   const matches=JSON.parse(run('gh',['api',`repos/${repo}/releases?per_page=100`])).filter(r=>r.tag_name===tag&&r.draft);
   if(matches.length!==1)throw error;return matches[0];
  }
 };
 let release=readRelease();if(release.tag_name!==tag||release.prerelease)throw Error('Expected stable release');
 const temp=mkdtempSync(join(tmpdir(),'caelis-linux-publication-'));
 try{
  for(const name of new Set(targets.flatMap(t=>t.names))){
   const expected=digest(readFileSync(join(directory,name)));let failure;
   if(!release.assets?.some(a=>a.name===name)){
    try{run('gh',['release','upload',tag,join(directory,name),'--repo',repo])}catch(e){failure=e}
    release=readRelease();if(!release.assets?.some(a=>a.name===name))throw failure||Error('Upload not confirmed: '+name);
   }
   run('gh',['release','download',tag,'--repo',repo,'--pattern',name,'--dir',temp,'--clobber']);
   if(digest(readFileSync(join(temp,name)))!==expected)throw Error('Immutable Linux asset differs: '+name);
  }
  if(release.draft){let failure;try{run('gh',['release','edit',tag,'--repo',repo,'--draft=false','--prerelease=false','--latest=false'])}catch(e){failure=e}release=readRelease();if(release.draft)throw failure||Error('Release remains draft')}
  if(release.tag_name!==tag||release.prerelease)throw Error('Release identity changed');
  return `Published Linux ${tag}; both signed architecture inventories read back`;
 }finally{rmSync(temp,{recursive:true,force:true})}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href)console.log(publishLinux(resolve(process.argv[2]),process.env.BOT_RELEASE_TAG));
