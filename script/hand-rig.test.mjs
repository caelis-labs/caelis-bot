import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {Vector3,Group} from 'three';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';

const dir=mkdtempSync(resolve('.cache/hand-test-'));
try{
 await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/character/hand-pose.ts'),outDir:dir,rolldownOptions:{output:{entryFileNames:'hands.mjs'}}}});
 const {HandPoseLayer}=await import(pathToFileURL(join(dir,'hands.mjs')));
 await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/character/held-plane.ts'),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:'anchor.mjs'}}}});
 const {HeldPlaneAnchor}=await import(pathToFileURL(join(dir,'anchor.mjs')));
 const parse=async name=>{const bytes=readFileSync(`frontend/public/models/${name}.glb`);return new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');};
 test('body, outfit and accessories stay independently removable on one skeleton',async()=>{
  const {scene:root}=await parse('caelis-soft-outfit-v1');
  const layerNames=['body','face','hair','clothing','accessories'],layers=[];
  root.traverse(node=>{if(node.userData.caelisLayer&&node.parent?.userData.caelisLayer!==node.userData.caelisLayer)layers.push(node);});
  assert.deepEqual(layers.map(n=>n.userData.caelisLayer).sort(),layerNames.sort());
  const bones=[];root.traverse(n=>{if(n.isBone)bones.push(n)});assert.equal(bones.length,61);
  for(const layer of layers){let vertices=0;layer.traverse(n=>{if(n.isSkinnedMesh){vertices+=n.geometry.attributes.position.count;for(const bone of n.skeleton.bones)assert.ok(bones.includes(bone));}});assert.ok(vertices>0);}
  for(const layer of layers)if(['clothing','accessories'].includes(layer.userData.caelisLayer))layer.visible=false;
  for(const layer of layers)if(['body','face','hair'].includes(layer.userData.caelisLayer))assert.ok(layer.visible);
  // Torso geometry still exists after hiding the costume, independent of the arms.
  root.updateMatrixWorld(true);const torso=[];layers.find(n=>n.userData.caelisLayer==='body').traverse(n=>{if(n.isMesh){const p=n.geometry.attributes.position;for(let i=0;i<p.count;i++){const v=new Vector3().fromBufferAttribute(p,i).applyMatrix4(n.matrixWorld);if(Math.abs(v.x)<.14&&v.y>.55&&v.y<.93)torso.push(i);}}});
  assert.ok(torso.length>250);
 });
 test('hand chains remain attached to the wrist, export normalized weights and deform independently',async()=>{
  const {scene:root}=await parse('caelis-soft-outfit-v1'),profile=root.userData.desktopPetFingerRig;
  assert.equal(profile.version,1);assert.equal(profile.joints.length,38);
  const get=name=>root.getObjectByName(name.replaceAll('.',''));
  for(const row of profile.joints)assert.equal(get(row.name).parent,get(row.parent));
  const hands=new HandPoseLayer(root);assert.equal(hands.available,true);
  const meshes=[];root.traverse(node=>{if(node.isSkinnedMesh)meshes.push(node)});
  const update=()=>{root.updateMatrixWorld(true);for(const mesh of meshes)mesh.skeleton.update();};
  hands.preview('open');update();
  const samples=[];let fingerVertices=0,wristVertices=0;
  for(const mesh of meshes){
   const indices=mesh.geometry.attributes.skinIndex,weights=mesh.geometry.attributes.skinWeight;
   for(let i=0;i<indices.count;i++){
    const names=[],values=[];
    for(let j=0;j<4;j++){names.push(mesh.skeleton.bones[indices.getComponent(i,j)].name);values.push(weights.getComponent(i,j));}
    assert.ok(Math.abs(values.reduce((a,b)=>a+b,0)-1)<2e-5);
    const finger=names.some((n,j)=>values[j]>.5&&/^(index|middle|ring|pinky|thumb)/.test(n));
    const wrist=names.some((n,j)=>values[j]>.99&&/^forearm/.test(n));
    if(finger||wrist){samples.push({mesh,i,p:mesh.getVertexPosition(i,new Vector3()).clone(),finger,wrist});if(finger)fingerVertices++;if(wrist)wristVertices++;}
   }
  }
  assert.ok(fingerVertices>500);assert.ok(wristVertices>30);
  hands.preview('softGrip');update();let moved=0;
  for(const sample of samples){const distance=sample.p.distanceTo(sample.mesh.getVertexPosition(sample.i,new Vector3()));if(sample.wrist)assert.ok(distance<1e-6);if(sample.finger&&distance>.004)moved++;assert.ok(distance<.10,'bounded short-finger deformation');}
  assert.ok(moved>150,'grip must move the skinned finger surface, not just unused bones');
  // Wrist and every original body transform remain owned by the arm/body layers.
  const original=get('hand.L').quaternion.clone();hands.preview('hold');assert.ok(original.angleTo(get('hand.L').quaternion)<1e-7);
 });
 test('pose blending restores cleanly and archived rigs remain a no-op',async()=>{
  const {scene:root}=await parse('caelis-soft-outfit-v1'),hands=new HandPoseLayer(root);
  const bones=root.userData.desktopPetFingerRig.joints.map(j=>root.getObjectByName(j.name.replaceAll('.','')));
  const bind=bones.map(b=>b.quaternion.clone());
  for(let frame=0;frame<180;frame++){hands.restore();hands.capture();hands.apply(1/60,{dragging:frame<60,holding:frame>=60&&frame<120});}
  hands.restore();bones.forEach((b,i)=>assert.deepEqual(b.quaternion.toArray(),bind[i].toArray()));
  hands.apply(0);const relaxed=bones.map(b=>b.quaternion.clone());
  for(let i=0;i<60;i++){hands.restore();hands.capture();hands.apply(1/60);}
  bones.forEach((b,i)=>assert.ok(relaxed[i].clone().normalize().angleTo(b.quaternion.clone().normalize())<1e-6));
  const old=new Group(),legacy=new HandPoseLayer(old);assert.equal(legacy.available,false);legacy.apply(1/30,{holding:true});legacy.restore();
 });
 test('the paper fold stays between posed contact joints as wrist and body turn',async()=>{
  const {scene:root}=await parse('caelis-soft-outfit-v1'),hand=root.getObjectByName('handR');
  const hands=new HandPoseLayer(root),anchor=new HeldPlaneAnchor(root,hand),plane=new Group();plane.scale.setScalar(.28);
  const contact=()=>root.getObjectByName('thumb02R').getWorldPosition(new Vector3()).lerp(root.getObjectByName('index03R').getWorldPosition(new Vector3()),.5);
  hands.preview('hold');anchor.place(plane);const before=contact();
  assert.ok(plane.localToWorld(new Vector3(-.20,-.099,0)).distanceTo(before)<1e-6);
  root.rotation.y=.35;hand.rotateX(.25);hands.preview('open');anchor.place(plane);
  const after=contact();assert.ok(after.distanceTo(before)>.01);
  assert.ok(plane.localToWorld(new Vector3(-.20,-.099,0)).distanceTo(after)<1e-6);
 });
}finally{rmSync(dir,{recursive:true,force:true});}
