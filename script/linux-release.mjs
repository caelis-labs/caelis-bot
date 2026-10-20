// Same Ed25519 trust as the managed updater; bootstrap adds the launcher and
// installer to the signed inventory. No private key generation or publication.
import {createHash} from 'node:crypto';
import {readFileSync,writeFileSync,copyFileSync,readdirSync,lstatSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {signManifest,verifyManifest,collect} from './linux-update-manifest.mjs';
import {validateTag} from './release-version.mjs';
const digest=b=>createHash('sha256').update(b).digest('hex');
const releaseKey='internal/managedupdate/release-key.txt';
export function linuxNames(version,arch){
 if(!/^(amd64|arm64)$/.test(arch)||!/^\d+\.\d+\.\d+(-qa\.\d+)?$/.test(version))throw Error('Invalid Linux target');
 const platform='linux-'+arch,stem=`caelisbot-${version}`;
 return {platform,archive:`${stem}-${platform}.tar.gz`,launcher:`caelisbot-launcher-${version}-${platform}`,manifest:`${platform}-update.json`,
  checksum:`${stem}-${platform}.sha256`,bootstrap:`${platform}-install.json`,script:`${stem}-install.sh`,installer:`${stem}-install.py`};
}
export function distributionNames(version,arch){const n=linuxNames(version,arch);return [n.archive,n.launcher,n.manifest,n.manifest+'.sig',n.checksum,n.script,n.installer,n.bootstrap,n.bootstrap+'.sig'];}
export function prepareLinux(directory,arch,privateKey,publicKey=releaseKey){
 const manifest=JSON.parse(readFileSync(join(directory,`linux-${arch}-update.json`))),n=linuxNames(manifest.version,arch);
 if(manifest.buildKind==='release'){
  validateTag('v'+manifest.version);
  if(resolve(publicKey)!==resolve(releaseKey))throw Error('Release key must be pinned in source');
 }
 if(manifest.platform!==n.platform)throw Error('Platform mismatch');
 // Re-read the actual package members, including modes and executable machine.
 if(JSON.stringify(collect(join(directory,'package')))!==JSON.stringify(manifest.members))throw Error('Staged package inventory differs');
 const launcher=readFileSync(join(directory,n.launcher));
 if(launcher.subarray(0,6).toString('hex')!=='7f454c460201'||launcher.readUInt16LE(18)!==(arch==='arm64'?183:62))throw Error('Wrong launcher architecture');
 signManifest(join(directory,n.manifest),privateKey,publicKey);
 const key=readFileSync(publicKey,'utf8').trim();if(Buffer.from(key,'base64').length!==32)throw Error('Release public key is not configured');
 writeFileSync(join(directory,n.script),readFileSync('deploy/headless/install.sh.in','utf8').replace('@RELEASE_KEY@',key),{flag:'wx',mode:0o644});
 copyFileSync('deploy/headless/install.py',join(directory,n.installer));
 const files=[n.archive,n.launcher,n.manifest,n.manifest+'.sig',n.script,n.installer];
 writeFileSync(join(directory,n.checksum),files.map(name=>`${digest(readFileSync(join(directory,name)))}  ${name}\n`).join(''),{flag:'wx',mode:0o644});
 files.push(n.checksum);
 const assets=files.map(name=>{const b=readFileSync(join(directory,name));return {name,length:b.length,sha256:digest(b)}});
 const index={schema:1,product:'caelisbot',platform:n.platform,channel:manifest.channel,version:manifest.version,source:manifest.source,buildKind:manifest.buildKind,assets};
 writeFileSync(join(directory,n.bootstrap),JSON.stringify(index,null,2)+'\n',{flag:'wx',mode:0o644});
 signManifest(join(directory,n.bootstrap),privateKey,publicKey);
 return verifyLinux(directory,arch,publicKey,manifest.buildKind==='qa');
}
export function verifyLinux(directory,arch,publicKey=releaseKey,qa=false){
 const m=verifyManifest(join(directory,`linux-${arch}-update.json`),publicKey),n=linuxNames(m.version,arch),index=verifyManifest(join(directory,n.bootstrap),publicKey);
 if(index.schema!==1||index.product!=='caelisbot'||m.schema!==1||m.product!=='caelisbot'||m.flavor!=='bundled'||m.launcher!==1||m.stateSchema!==1||m.platform!==n.platform||index.platform!==m.platform||index.version!==m.version||index.source!==m.source||!/^[a-f0-9]{40}$/.test(m.source)||index.buildKind!==m.buildKind||index.channel!==m.channel)throw Error('Invalid Linux release identity');
 if(!qa&&(m.buildKind!=='release'||m.channel!=='stable'||m.version.includes('-')))throw Error('QA is not a release');
 if(qa&&(m.buildKind!=='qa'||m.channel!=='qa'||!m.version.includes('-qa.')))throw Error('Expected QA identity');
 const names=distributionNames(m.version,arch).filter(name=>name!==n.bootstrap&&name!==n.bootstrap+'.sig');
 if(!Array.isArray(index.assets)||index.assets.length!==names.length||new Set(index.assets.map(a=>a.name)).size!==names.length)throw Error('Invalid signed inventory');
 for(const asset of index.assets){
  if(!names.includes(asset.name))throw Error('Unexpected signed member');
  const path=join(directory,asset.name),stat=lstatSync(path);if(!stat.isFile()||stat.isSymbolicLink())throw Error('Invalid asset');
  const bytes=readFileSync(path);if(bytes.length!==asset.length||digest(bytes)!==asset.sha256)throw Error('Signed asset differs: '+asset.name);
 }
 const bytes=readFileSync(join(directory,n.archive));if(bytes.length!==m.length||digest(bytes)!==m.sha256||m.file!==n.archive)throw Error('Archive differs');
 const checksum=names.filter(name=>name!==n.checksum).map(name=>`${digest(readFileSync(join(directory,name)))}  ${name}\n`).join('');
 if(checksum!==readFileSync(join(directory,n.checksum),'utf8'))throw Error('Checksum inventory differs');
 const launcher=readFileSync(join(directory,n.launcher));if(launcher.subarray(0,6).toString('hex')!=='7f454c460201'||launcher.readUInt16LE(18)!==(arch==='arm64'?183:62))throw Error('Launcher machine differs');
 return {index,manifest:m,names:distributionNames(m.version,arch)};
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 const [cmd,...args]=process.argv.slice(2);
 if(cmd==='prepare')prepareLinux(...args);
 else if(cmd==='verify')verifyLinux(...args);
 else throw Error('Expected prepare or verify');
}
