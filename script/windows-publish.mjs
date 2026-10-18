import {mkdtempSync,readFileSync,writeFileSync,rmSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';
import {qualify,verifyPublication} from './windows-update-manifest.mjs';
import {digest} from './update-manifest.mjs';
import {releaseCommand} from './release-network.mjs';
const repository='caelis-labs/caelis-bot';
export function publicationPlan(directory,tag,source,publishUpdates=false){
 const manifest=publishUpdates?verifyPublication(directory,tag,source):qualify(directory,tag,source);
 const assets=manifest.artifacts.flatMap(a=>[a.file,a.file+'.sha256',a.file+'.build.json']).concat(publishUpdates?['windows-update.json','windows-update.json.sig']:[]);
 return {manifest,assets};
}
export function publishWindows(directory,tag,source,publishUpdates=false,run=releaseCommand){
 const {manifest,assets}=publicationPlan(directory,tag,source,publishUpdates);
 const gh=args=>run('gh',args);
 if(JSON.parse(gh(['api',`repos/${repository}/commits/${tag}`])).sha!==source)throw Error('Immutable Windows tag source mismatch');
 const prerelease=manifest.channel==='preview';
 const release=()=>{
  try{return JSON.parse(gh(['api',`repos/${repository}/releases/tags/${tag}`]));}
  catch(error){
   if(!String(error.stderr||error).includes('HTTP 404'))throw error;
   const matches=JSON.parse(gh(['api',`repos/${repository}/releases?per_page=100`])).filter(r=>r.tag_name===tag);
   if(matches.length>1)throw Error('Ambiguous release');
   return matches[0];
  }
 };
 let current=release();
 const temporary=mkdtempSync(join(tmpdir(),'caelis-windows-publish-'));
 try{
  if(!current){
   const notes=join(temporary,'notes.md');
   writeFileSync(notes,`Windows ${manifest.channel} ${manifest.version}\n\nUnsigned Setup.exe and portable ZIP for Windows 10 22H2 x64 or later with WebView2. Windows 11 is the primary development target. CI checks and exact package integrity passed; native GUI evidence is recorded separately.\n\nThe installer is not Authenticode signed; Windows or organization policy may warn or block it. ${publishUpdates?'Automatic updates independently require the embedded Ed25519 signature.':'Installable packages only; no unsigned automatic update manifest is published.'}\n\nSource: ${source}\n`);
   let failure;
   try{gh(['release','create',tag,'--repo',repository,'--verify-tag','--draft',`--prerelease=${prerelease}`,'--latest=false','--title',prerelease?`Windows preview ${manifest.version}`:`Caelis Bot ${manifest.version}`,'--notes-file',notes]);}catch(error){failure=error;}
   current=release();
   if(!current)throw failure||Error('Release creation not confirmed');
  }
  if(current.tag_name!==tag||current.prerelease!==prerelease)throw Error('Release channel mismatch');
  for(const name of assets){
   let uploadError;
   if(!current.assets?.some(a=>a.name===name)){
    try{gh(['release','upload',tag,join(directory,name),'--repo',repository]);}catch(error){uploadError=error;}
    current=release();
   }
   if(!current?.assets?.some(a=>a.name===name))throw uploadError||Error(`Upload not confirmed: ${name}`);
   gh(['release','download',tag,'--repo',repository,'--pattern',name,'--dir',temporary,'--clobber']);
   if(digest(readFileSync(join(directory,name)))!==digest(readFileSync(join(temporary,name))))throw Error(`Immutable release asset differs: ${name}`);
  }
  if(current.draft){
   let failure;
   try{gh(['release','edit',tag,'--repo',repository,'--draft=false',`--prerelease=${prerelease}`,'--latest=false']);}catch(error){failure=error;}
   current=release();
   if(current?.draft&&failure)throw failure;
  }
  if(!current||current.draft||current.tag_name!==tag||current.prerelease!==prerelease)throw Error('Windows publication not confirmed');
  return `Published Windows ${manifest.channel} ${tag}`;
 }finally{rmSync(temporary,{recursive:true,force:true});}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 console.log(publishWindows(process.argv[2],process.env.WINDOWS_TAG,process.env.WINDOWS_SOURCE,process.env.WINDOWS_PUBLISH_UPDATES==='true'));
}
