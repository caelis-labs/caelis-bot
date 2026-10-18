// Independent Windows preview feed. Never writes Sparkle, latest.json or R2.
import {createHash,createPrivateKey,createPublicKey,sign,verify} from 'node:crypto';
import {readFileSync,writeFileSync,existsSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {windowsVersion} from './windows-version.mjs';

const sha256=bytes=>createHash('sha256').update(bytes).digest('hex');
const formats=[['nsis','-Setup.exe'],['zip','.zip']];
const publicKeyPath='internal/windowsupdate/public-key.txt';
export function previewVersion(tag){
 const match=/^v(\d+\.\d+\.\d+)-windows\.preview\.([1-9]\d*)$/.exec(tag);
 if(!match)throw Error('Expected an independent Windows preview tag');
 return windowsVersion(match[1],'preview',tag,Number(match[2])).version;
}
function publicKey(path){
 const raw=Buffer.from(readFileSync(path,'utf8').trim(),'base64');
 if(raw.length!==32)throw Error('Invalid Windows update public key');
 return createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),raw]),format:'der',type:'spki'});
}
export function collectArtifacts(directory,tag,source){
 const version=previewVersion(tag);
 if(!/^[a-f0-9]{40}$/.test(source))throw Error('Exact source SHA required');
 return formats.map(([format,suffix])=>{
  const file=`Caelis-Bot-${version}-windows-amd64-preview${suffix}`,path=join(directory,file);
  const bytes=readFileSync(path),build=JSON.parse(readFileSync(path+'.build.json','utf8'));
  if(build.schema!==1||build.version!==version||build.channel!=='preview'||build.platform!=='windows-amd64'||build.format!==format||build.dirty!==false||build.source!==source||build.sha256!==sha256(bytes)||build.signed!==false||build.signingPolicy!=='unsigned-preview'||bytes.length===0||bytes.length>512*1024*1024)throw Error('Exact clean unsigned preview artifacts required');
  if(readFileSync(path+'.sha256','utf8').trim()!==`${sha256(bytes)}  ${file}`)throw Error('Artifact checksum sidecar mismatch');
  return {format,file,sha256:sha256(bytes),length:bytes.length};
 });
}
// The protected workflow runs Windows checks and packages the exact main tag.
// Qualification here binds those immutable bytes; it does not claim native GUI acceptance.
export function qualify(directory,tag,source){
 return {schema:1,version:previewVersion(tag),tag,channel:'preview',platform:'windows-amd64',source,qualified:true,qualification:'ci-build-checks',artifacts:collectArtifacts(directory,tag,source)};
}
export function create(directory,tag,source,privatePath,keyPath=publicKeyPath){
 const manifest=qualify(directory,tag,source);
 const bytes=Buffer.from(JSON.stringify(manifest,null,2)+'\n');
 const key=createPrivateKey(readFileSync(privatePath));
 if(key.asymmetricKeyType!=='ed25519')throw Error('Ed25519 private key required');
 const signature=sign(null,bytes,key);
 if(!verify(null,bytes,publicKey(keyPath),signature))throw Error('Private key differs from the public key embedded in Bot');
 for(const file of ['windows-update.json','windows-update.json.sig'])if(existsSync(join(directory,file)))throw Error('Immutable update metadata already exists');
 writeFileSync(join(directory,'windows-update.json'),bytes,{flag:'wx'});
 writeFileSync(join(directory,'windows-update.json.sig'),signature.toString('base64')+'\n',{flag:'wx'});
 return manifest;
}
export function verifyPublication(directory,tag,source,keyPath=publicKeyPath){
 const bytes=readFileSync(join(directory,'windows-update.json'));
 const signature=Buffer.from(readFileSync(join(directory,'windows-update.json.sig'),'utf8').trim(),'base64');
 if(!verify(null,bytes,publicKey(keyPath),signature))throw Error('Windows update signature mismatch');
 const manifest=JSON.parse(bytes),artifacts=collectArtifacts(directory,tag,source);
 if(manifest.schema!==1||manifest.version!==previewVersion(tag)||manifest.tag!==tag||manifest.source!==source||manifest.channel!=='preview'||manifest.platform!=='windows-amd64'||manifest.qualified!==true||JSON.stringify(manifest.artifacts)!==JSON.stringify(artifacts))throw Error('Windows update artifact/source mismatch');
 return manifest;
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 const [command,...args]=process.argv.slice(2);
 if(command==='create')create(...args);
 else if(command==='verify')verifyPublication(...args);
 else if(command==='qualify')qualify(...args);
 else throw Error('Expected create, verify or qualify');
 console.log('Windows preview metadata verified');
}
