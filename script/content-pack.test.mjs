import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdirSync,mkdtempSync,rmSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import validator from 'gltf-validator';

test('community static geometry and independent stick rig degrade without character-specific metadata',async()=>{
 mkdirSync('.cache',{recursive:true});const dir=mkdtempSync(resolve('.cache/content-animation-'));
 try{
  await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/character/animation.ts'),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:'animation.mjs'}}}});
  const {CharacterAnimation}=await import(pathToFileURL(join(dir,'animation.mjs')));
  for(const file of ['internal/contentpack/testdata/basic.glb','frontend/public/models/stick.glb']){
   const bytes=readFileSync(file),report=await validator.validateBytes(new Uint8Array(bytes));assert.equal(report.issues.numErrors,0,JSON.stringify(report.issues));
   const gltf=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
   const player=new CharacterAnimation(gltf.scene,gltf.animations,false);
   player.setActivity('working');player.update(.4);player.gesture('celebrate');player.update(.3);
   assert.ok(Number.isFinite(player.progress));player.rest();player.dispose();
   if(!gltf.animations.length)assert.throws(()=>new CharacterAnimation(gltf.scene,[]),/Missing character clip/);
  }
 }finally{rmSync(dir,{recursive:true,force:true});}
});
