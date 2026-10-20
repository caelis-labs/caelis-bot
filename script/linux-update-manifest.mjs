// Typed Linux package/signature contract. No publication or key generation.
import {createHash,createPrivateKey,createPublicKey,sign,verify} from 'node:crypto';
import {readFileSync,writeFileSync,lstatSync,readdirSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

const digest=b=>createHash('sha256').update(b).digest('hex');
const publicKey=path=>{
 const raw=Buffer.from(readFileSync(path,'utf8').trim(),'base64');
 if(raw.length!==32)throw Error('Persistent Ed25519 public key is not configured');
 return createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),raw]),format:'der',type:'spki'});
};
const allowed=new Set(['caelisbot','Helpers/caelis','Resources/Caelis-Runtime.json','Resources/Caelis-Runtime-LICENSE','LICENSE','README.md']);
export function collect(directory) {
 const files=[];
 function visit(relative=''){
  for(const name of readdirSync(join(directory,relative)).sort()){
   const path=relative?relative+'/'+name:name,stat=lstatSync(join(directory,path));
   if(stat.isSymbolicLink())throw Error('Links are not package members');
   if(stat.isDirectory()){visit(path);continue;}
   if(!stat.isFile()||!allowed.has(path)||stat.size<=0)throw Error('Unexpected package member: '+path);
   const mode=path==='caelisbot'||path==='Helpers/caelis'?0o755:0o644;
   if((stat.mode&0o777)!==mode)throw Error('Wrong member mode');
   files.push({path,sha256:digest(readFileSync(join(directory,path))),length:stat.size,mode});
  }
 }
 visit();for(const name of [...allowed].filter(n=>n!=='README.md'))if(!files.some(f=>f.path===name))throw Error('Missing '+name);
 return files.sort((a,b)=>a.path.localeCompare(b.path));
}
export function create(directory,archive,version,source,platform,kind){
 if(!/^[a-f0-9]{40}$/.test(source)||!/^linux-(amd64|arm64)$/.test(platform))throw Error('Exact source and platform required');
 if(!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-qa\.[1-9]\d*)?$/.test(version))throw Error('Invalid version');
 if(kind!=='release'&&kind!=='qa'||(kind==='qa')!==version.includes('-qa.'))throw Error('QA identity must be explicit');
 const members=collect(directory),bytes=readFileSync(archive),pin=JSON.parse(readFileSync(join(directory,'Resources/Caelis-Runtime.json'),'utf8'));
 const target=pin.targets[platform],runtime=members.find(f=>f.path==='Helpers/caelis');
 if(!target||target.binary_sha256!==runtime.sha256)throw Error('Bundled Runtime digest mismatch');
 for(const member of members.filter(f=>f.mode===0o755)){
  const binary=readFileSync(join(directory,member.path)),machine=platform==='linux-arm64'?183:62;
  if(binary.subarray(0,6).toString('hex')!=='7f454c460201'||binary.readUInt16LE(18)!==machine)throw Error('Wrong executable architecture');
 }
 if(bytes.length===0||bytes.length>512*1024*1024)throw Error('Archive budget exceeded');
 return {schema:1,product:'caelisbot',flavor:'bundled',platform,channel:kind==='qa'?'qa':'stable',version,source,buildKind:kind,launcher:1,stateSchema:1,
  file:`caelisbot-${version}-${platform}.tar.gz`,sha256:digest(bytes),length:bytes.length,expandedBytes:members.reduce((n,f)=>n+f.length,0),
  runtime:{version:pin.version,source:pin.commit,sha256:runtime.sha256},members};
}
export function signManifest(file,privatePath,publicPath){
 const bytes=readFileSync(file),manifest=JSON.parse(bytes),key=createPrivateKey(readFileSync(privatePath));
 if(key.asymmetricKeyType!=='ed25519')throw Error('Ed25519 required');
 const signature=sign(null,bytes,key);
 if(!verify(null,bytes,publicKey(publicPath),signature))throw Error('Private/public release identity mismatch');
 if(manifest.buildKind==='release'&&resolve(publicPath)!==resolve('internal/managedupdate/release-key.txt'))throw Error('Production must use the reviewed embedded release key');
 writeFileSync(file+'.sig',signature.toString('base64')+'\n',{flag:'wx',mode:0o600});
}
export function verifyManifest(file,publicPath){
 const bytes=readFileSync(file),signature=Buffer.from(readFileSync(file+'.sig','utf8').trim(),'base64');
 if(!verify(null,bytes,publicKey(publicPath),signature))throw Error('Invalid manifest signature');
 return JSON.parse(bytes);
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 const [command,...args]=process.argv.slice(2);
 if(command==='create'){
  const [directory,archive,version,source,platform,kind,output]=args;
  writeFileSync(output,JSON.stringify(create(directory,archive,version,source,platform,kind),null,2)+'\n',{flag:'wx',mode:0o600});
 }else if(command==='sign')signManifest(...args);
 else if(command==='verify')verifyManifest(...args);
 else throw Error('Expected create, sign or verify');
}
