import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import {Group,OrthographicCamera,Vector3,Quaternion} from 'three';
import validator from 'gltf-validator';
const dir=mkdtempSync(resolve('.cache/view-test-'));
try {
 for(const name of ['view-correctives','face'])await build({configFile:false,logLevel:'silent',build:{ssr:resolve(`frontend/src/character/${name}.ts`),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:`${name}.mjs`}}}});
 const {ViewCorrectives,pruneEmptyViewTargets,viewWeights}=await import(pathToFileURL(join(dir,'view-correctives.mjs')));
 const {FacialAnimation}=await import(pathToFileURL(join(dir,'face.mjs')));
 const parse=async path=>{const b=readFileSync(path);return new GLTFLoader().parseAsync(b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength),'')};
 const clips=g=>g.animations.map(c=>{const {uuid,...data}=c.toJSON();return data});
 const path='frontend/public/models/caelis-soft-outfit-v1.glb';
 const sample=await parse(path),profile=sample.scene.userData.desktopPetViewMorphs;
 test('view blending is continuous, bounded, symmetric and fades away at the back',()=>{
  for(let angle=-180;angle<=180;angle+=.5){const w=viewWeights(angle*Math.PI/180,profile),next=viewWeights((angle+.001)*Math.PI/180,profile);assert.ok(Object.values(w).every(n=>n>=0&&n<=1));assert.ok(Object.values(w).reduce((a,b)=>a+b,0)<=1+1e-8);assert.ok(Object.keys(w).every(k=>Math.abs(next[k]-w[k])<.001));}
  assert.equal(viewWeights(35*Math.PI/180,profile).view3QLeft,1);assert.equal(viewWeights(-85*Math.PI/180,profile).viewSideRight,1);
  for(const a of [0,140,180,-140,-180])assert.equal(Object.values(viewWeights(a*Math.PI/180,profile)).reduce((a,b)=>a+b,0),0);
 });
 test('zero-target pruning preserves evaluated vertices and respects partial facial dictionaries',async()=>{
  const gltf=await parse(path),meshes=[];gltf.scene.traverse(o=>{if(o.isMesh)meshes.push(o)});
  const samples=[];let before=0,after=0;
  for(const mesh of meshes){before+=mesh.morphTargetInfluences.length;mesh.morphTargetInfluences=mesh.morphTargetInfluences.map((_,i)=>i%4*.13);mesh.updateMatrixWorld(true);for(let i=0;i<mesh.geometry.attributes.position.count;i+=131)samples.push([mesh,i,mesh.getVertexPosition(i,new Vector3()).clone()]);}
  pruneEmptyViewTargets(gltf.scene);
  for(const [m,i,p] of samples)assert.ok(m.getVertexPosition(i,new Vector3()).distanceTo(p)<1e-7);
  for(const m of meshes)after+=m.morphTargetInfluences.length;
  assert.ok(after<before/2,`${before} -> ${after}`);
  const face=new FacialAnimation(gltf.scene,()=>.5);assert.ok(face.available);face.rest();
  for(const m of meshes){const i=m.morphTargetDictionary.mouthClosed;if(i!==undefined)assert.equal(m.morphTargetInfluences[i],1);}
  assert.ok(meshes.some(m=>m.morphTargetDictionary.mouthClosed!==undefined&&m.morphTargetDictionary.blinkLeft===undefined),'partial mouth-only dictionary participates');
 });
 test('head and camera turns drive corrections without expression or transform drift',async()=>{
  const gltf=await parse(path);pruneEmptyViewTargets(gltf.scene);const root=gltf.scene, parent=new Group();parent.add(root);
  const head=root.getObjectByName('head'),bind=head.quaternion.clone();
  const controller=new ViewCorrectives(root),camera=new OrthographicCamera(-1,1,2,0,.1,10);camera.position.z=5;
  const meshes=[];root.traverse(o=>{if(o.isMesh)meshes.push(o)});
  const facial=new FacialAnimation(root,()=>.5);facial.rest();controller.apply(camera);assert.ok(Math.abs(controller.yaw)<1e-6);
  parent.rotation.y=-85*Math.PI/180;controller.apply(camera);assert.ok(Math.abs(controller.yaw-85*Math.PI/180)<1e-5);
  for(const mesh of meshes){const d=mesh.morphTargetDictionary,v=mesh.morphTargetInfluences;for(const n of profile.expressionTargets){if(d[n]!==undefined)v[d[n]]=.37;}controller.apply(camera);for(const n of profile.expressionTargets){if(d[n]!==undefined)assert.equal(v[d[n]],.37);const ci=d['viewSideLeft__'+n];if(ci!==undefined)assert.ok(Math.abs(v[ci]-.37)<1e-7);}}
  // Camera rotates with the model: front again, independent of its world heading.
  camera.rotation.y=parent.rotation.y;controller.apply(camera);assert.ok(Math.abs(controller.yaw)<1e-5);
  // Turn head alone about world up; bind bone orientation must not skew the yaw.
  parent.rotation.y=0;camera.rotation.y=0;root.updateMatrixWorld(true);
  const axis=new Vector3(0,1,0).applyQuaternion(head.parent.getWorldQuaternion(new Quaternion()).invert());
  head.quaternion.copy(bind).premultiply(new Quaternion().setFromAxisAngle(axis,-35*Math.PI/180));controller.apply(camera);assert.ok(Math.abs(controller.yaw-35*Math.PI/180)<1e-5);
  const q=head.quaternion.clone(),weights=meshes.map(m=>m.morphTargetInfluences.slice());for(let i=0;i<120;i++)controller.apply(camera);assert.ok(q.angleTo(head.quaternion)<1e-6);meshes.forEach((m,i)=>assert.deepEqual(m.morphTargetInfluences,weights[i]));
  const noop=new ViewCorrectives(new Group());assert.equal(noop.available,false);noop.apply(camera);
 });
} finally {rmSync(dir,{recursive:true,force:true});}
