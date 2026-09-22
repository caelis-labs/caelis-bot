// Public handoff contract. This module never reads a private repository.
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync,lstatSync,readdirSync,mkdirSync,copyFileSync,existsSync,unlinkSync} from 'node:fs';
import {resolve,dirname,join,relative} from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash} from 'node:crypto';
import validator from 'gltf-validator';
import {validateAvatarSVG} from './avatar-svg.mjs';

export const manifestPath='resources/character-pack.json';
export const runtimeExtras=new Set(['desktopPetMouthRest','desktopPetWristRest','desktopPetHands','desktopPetProfile','desktopPetLocomotion','desktopPetViewMorphs','desktopPetHandLocal','desktopPetHandGestures','desktopPetFingerRig','desktopPetArmPole','desktopPetArmMotion','desktopPetGestures','desktopPetForearmTwist','desktopPetRelaxedArms','desktopPetSoftOutfit','caelisLayer','targetNames']);
const fixedPaths=new Set(['frontend/public/models/caelis-SOURCES.md','frontend/public/icons/caelis-avatar.png','internal/desktop/assets/app-icon.png','internal/desktop/assets/status-icon.png','resources/macos/CaelisBot.icns']);
const animatedAvatar='frontend/assets/caelis-avatar-v1.svg';
const modelPath=p=>/^frontend\/public\/models\/[a-z0-9]+(?:-[a-z0-9]+)*\.glb$/.test(p);
const id=s=>typeof s==='string'&&/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(s);
export const sha256=bytes=>createHash('sha256').update(bytes).digest('hex');
function file(root,path){
 assert.ok(!path.startsWith('/')&&!path.split('/').some(p=>p==='..'||p==='.'||!p),'unsafe asset path');
 let current=resolve(root);
 assert.ok(!lstatSync(current).isSymbolicLink(),'symlink root');
 for(const part of path.split('/')){current=join(current,part);assert.ok(!lstatSync(current).isSymbolicLink(),`symlink: ${path}`);}
 assert.ok(lstatSync(current).isFile(),`not a file: ${path}`);
 return current;
}
function exactKeys(object,keys){assert.deepEqual(Object.keys(object).sort(),keys.sort(),'unsupported manifest fields');}
export function validateManifest(m){
 exactKeys(m,['schemaVersion','packId','version','contractVersion','defaultCharacter','defaultVariant','characters','fallbackModel','props','branding','files']);
 assert.equal(m.schemaVersion,1);assert.ok([1,2].includes(m.contractVersion),'unsupported runtime contract');
 assert.ok(id(m.packId)&&/^\d+\.\d+\.\d+$/.test(m.version),'invalid pack version');
 assert.ok(Array.isArray(m.files)&&m.files.length>0&&m.files.length<=64);
 const paths=new Set();
 for(const f of m.files){
  exactKeys(f,['path','sha256','license']);
  assert.ok(fixedPaths.has(f.path)||modelPath(f.path)||(m.contractVersion===2&&f.path===animatedAvatar),`not an approved output path: ${f.path}`);
  assert.ok(!paths.has(f.path),'duplicate file');paths.add(f.path);
  assert.match(f.sha256,/^[a-f0-9]{64}$/);
  assert.ok(['Apache-2.0','LicenseRef-Caelis-Character-1.0'].includes(f.license));
 }
 const use=p=>assert.ok(paths.has(p),`missing referenced file: ${p}`);
 const model=p=>{assert.ok(modelPath(p),'expected model');use(p);};
 for(const p of fixedPaths)use(p);
 assert.ok(Array.isArray(m.characters)&&m.characters.length>0);
 const characters=new Set();
 for(const c of m.characters){
  exactKeys(c,['id','variants']);assert.ok(id(c.id)&&!characters.has(c.id));characters.add(c.id);
  assert.ok(c.variants.length>0);const variants=new Set();
  for(const v of c.variants){
   exactKeys(v,['id','model','capabilities']);assert.ok(id(v.id)&&!variants.has(v.id));variants.add(v.id);model(v.model);
   assert.deepEqual(v.capabilities,['body-v1','face-v1','hands-v1','view-v1','drag-run-v1'],'new capabilities require a reviewed app contract change');
  }
 }
 assert.ok(m.characters.find(c=>c.id===m.defaultCharacter)?.variants.some(v=>v.id===m.defaultVariant),'default variant missing');
 model(m.fallbackModel);exactKeys(m.props,['paperPlane']);model(m.props.paperPlane);
 const branding={avatar:'frontend/public/icons/caelis-avatar.png',appIcon:'internal/desktop/assets/app-icon.png',statusIcon:'internal/desktop/assets/status-icon.png',macIcon:'resources/macos/CaelisBot.icns'};
 if(m.contractVersion===2){branding.animatedAvatar=animatedAvatar;use(animatedAvatar);}
 assert.deepEqual(m.branding,branding);
 return m;
}
export function readManifest(root='.'){return validateManifest(JSON.parse(readFileSync(file(root,manifestPath),'utf8')));}
export async function verifyPack(root='.',{strict=false}={}){
 const m=readManifest(root);let total=0;
 for(const f of m.files){
  const path=file(root,f.path),size=lstatSync(path).size;
  assert.ok(size>0&&size<=16*1024*1024,`asset size: ${f.path}`);total+=size;
  const bytes=readFileSync(path);assert.equal(sha256(bytes),f.sha256,`hash mismatch: ${f.path}`);
  if(f.path.endsWith('.glb')){
   assert.equal(bytes.readUInt32LE(0),0x46546c67);assert.equal(bytes.readUInt32LE(4),2);assert.equal(bytes.readUInt32LE(8),bytes.length);
   const doc=JSON.parse(bytes.subarray(20,20+bytes.readUInt32LE(12)));
   function check(o){
    if(!o||typeof o!=='object')return;
    if(o.extras)for(const key of Object.keys(o.extras))assert.ok(runtimeExtras.has(key),`authoring metadata: ${key}`);
    if('uri' in o)assert.fail('self-contained GLB required; external/data URIs are not supported');
    for(const [key,value]of Object.entries(o))if(key!=='extras')check(value);
   }
   check(doc);
   assert.ok(!/\/Users\/|\.blend\b|characters\/|sourceSHA|source_art|prompt/i.test(JSON.stringify(doc)),'private metadata in GLB');
   const result=await validator.validateBytes(new Uint8Array(bytes),{uri:f.path});
   assert.equal(result.issues.numErrors,0,JSON.stringify(result.issues.messages));
   assert.equal(result.issues.numWarnings,0,JSON.stringify(result.issues.messages));
  }else if(f.path.endsWith('.svg'))validateAvatarSVG(bytes.toString('utf8'));
  else if(f.path.endsWith('.png'))assert.equal(bytes.subarray(0,8).toString('hex'),'89504e470d0a1a0a');
  else if(f.path.endsWith('.icns'))assert.equal(bytes.subarray(0,4).toString(),'icns');
 }
 assert.ok(total<=64*1024*1024,'pack budget exceeded');
 if(strict){
  const expected=new Set([manifestPath,...m.files.map(f=>f.path)]);
  function walk(dir){for(const entry of readdirSync(dir,{withFileTypes:true})){
   const p=join(dir,entry.name);assert.ok(!entry.isSymbolicLink(),'release bundle may not contain symlinks');
   if(entry.isDirectory())walk(p);else assert.ok(expected.has(relative(resolve(root),p)),`unlisted release file: ${p}`);
  }}
  walk(resolve(root));
 }
 return m;
}
export async function importPack(source,destination='.'){
 const m=await verifyPack(source,{strict:true});
 const old=existsSync(resolve(destination,manifestPath))?readManifest(destination):undefined;
 const removed=(old?.files??[]).filter(f=>!m.files.some(next=>next.path===f.path));
 // Validate every input before writing anything, and reject symlink destinations.
 for(const path of [...m.files.map(f=>f.path),...removed.map(f=>f.path),manifestPath]){
  let current=resolve(destination);
  for(const part of path.split('/')){current=join(current,part);try{assert.ok(!lstatSync(current).isSymbolicLink(),'symlink destination');}catch(e){if(e.code!=='ENOENT')throw e;}}
 }
 for(const f of removed)if(existsSync(resolve(destination,f.path)))unlinkSync(resolve(destination,f.path));
 for(const f of m.files){const out=resolve(destination,f.path);mkdirSync(dirname(out),{recursive:true});copyFileSync(resolve(source,f.path),out);}
 mkdirSync(resolve(destination,'resources'),{recursive:true});
 writeFileSync(resolve(destination,manifestPath),JSON.stringify(m,null,2)+'\n');
 return m;
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
 const [command='verify',source='.',destination='.']=process.argv.slice(2);
 assert.ok(['verify','import'].includes(command),'usage: node script/asset-pack.mjs verify [root] | import <release-directory> [destination]');
 const pack=command==='import'?await importPack(source,destination):await verifyPack(source,{strict:process.argv.includes('--strict')});
 console.log(`${pack.packId}@${pack.version}: ${pack.files.length} finished assets verified (contract v${pack.contractVersion})`);
}
