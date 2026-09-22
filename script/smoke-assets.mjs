import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import {AnimationMixer,Box3} from 'three';
import {verifyPack} from './asset-pack.mjs';
const pack=await verifyPack();
for(const f of pack.files.filter(f=>f.path.endsWith('.glb'))){
 const b=readFileSync(f.path),gltf=await new GLTFLoader().parseAsync(b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength),'');
 assert.ok(!new Box3().setFromObject(gltf.scene).isEmpty());
 if(gltf.animations.length){const mixer=new AnimationMixer(gltf.scene);mixer.clipAction(gltf.animations[0]).play();mixer.update(.25);assert.equal(mixer.time,.25);mixer.stopAllAction();mixer.uncacheRoot(gltf.scene);}
 console.log(`${f.path}: GLB validation, Three.js load and animation update passed; GPU/native appearance not tested here`);
}
