import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdirSync,mkdtempSync,rmSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import {Box3} from 'three';
import validator from 'gltf-validator';

test('shipped character, five clips and host transitions preserve frame bounds',async()=>{
 const bytes=readFileSync('frontend/public/models/caelis-soft-outfit-v1.glb');
 const report=await validator.validateBytes(new Uint8Array(bytes),{uri:'caelis-soft-outfit-v1.glb'});
 assert.equal(report.issues.numErrors,0);assert.equal(report.issues.numWarnings,0);
 const gltf=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
 assert.deepEqual(gltf.animations.map(c=>c.name).sort(),['attention','celebrate','idle','nod','working']);
 mkdirSync('.cache',{recursive:true});const dir=mkdtempSync(resolve('.cache/character-test-'));
 try{
  await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/character/animation.ts'),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:'animation.mjs'}}}});
  const {CharacterAnimation}=await import(pathToFileURL(join(dir,'animation.mjs')));
  const player=new CharacterAnimation(gltf.scene,gltf.animations);
  player.setActivity('working');player.update(.25);assert.equal(player.clip,'working');
  player.gesture('celebrate');player.update(.3);player.gesture('nod');player.update(1.1);assert.equal(player.clip,'working');
  player.setActivity('waiting');assert.equal(player.clip,'attention');player.update(1.8);assert.equal(player.clip,'idle');
  player.setActivity('waiting');assert.equal(player.clip,'idle','same pending decision must not repeat attention');
  player.setActivity('working');player.gesture('attention');player.rest();assert.equal(player.clip,'working','hidden/reduced motion cancels a transient gesture');
  player.gesture('celebrate');player.setActivity('idle');assert.equal(player.clip,'celebrate','turn completion must not cut feedback short');
  player.update(2);assert.equal(player.clip,'idle','feedback must return to the latest base activity');
  const clips=['idle','working','attention','nod','celebrate'];
  let samples=0;
  for(const from of clips)for(const to of clips){
   const select=name=>name==='idle'||name==='working'?player.setActivity(name):player.gesture(name);
   select(from);player.update(.35);select(to);
   for(let frame=0;frame<6;frame++){
    player.update(1/30);gltf.scene.updateMatrixWorld(true);
    const bounds=new Box3().setFromObject(gltf.scene,true);
    assert.ok(bounds.min.x> -1.05&&bounds.max.x<1.05&&bounds.min.y>-.5134&&bounds.max.y<2.2867,`${from} → ${to} clipped`);samples++;
   }
  }
  player.dispose();assert.equal(samples,150);
 }finally{rmSync(dir,{recursive:true,force:true});}
});

test('local behavior yields to interaction, restores constant tracks and keeps prop paths bounded',async()=>{
 mkdirSync('.cache',{recursive:true});const dir=mkdtempSync(resolve('.cache/behavior-test-'));
 try{
  for(const name of ['behavior','plane'])await build({configFile:false,logLevel:'silent',build:{ssr:resolve(`frontend/src/character/${name}.ts`),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:`${name}.mjs`}}}});
  const {BehaviorDirector,PoseLayer,Spring,idleBehaviors}=await import(pathToFileURL(join(dir,'behavior.mjs')));
  const {flightPoint}=await import(pathToFileURL(join(dir,'plane.mjs')));
  const context={desktop:{workArea:{x:0,y:40,width:1440,height:860}},actor:{x:1200,y:300,width:180,height:240},activeWindow:{frame:{x:100,y:100,width:700,height:600}},pointer:{x:1280,y:490,hovering:false},interaction:{pressing:false,dragging:false,input:false,menu:false}};
  const director=new BehaviorDirector(()=>.99);
  assert.equal(director.holdsPlane,false,'ordinary idle starts empty-handed');
  director.preview('plane_care');assert.equal(director.holdsPlane,true);
  director.preview('breathe');assert.equal(director.holdsPlane,false,'put the prop away when its performance ends');
  director.preview('plane_play');assert.equal(director.holdsPlane,true);
  assert.equal(director.update(2.3,'idle',context),true);
  assert.equal(director.holdsPlane,false,'release clears the held prop even after a flight receipt or rejection');
  assert.equal(director.update(.1,'idle',context),false,'one release per behavior');
  director.update(.1,'idle',{...context,interaction:{...context.interaction,dragging:true}});
  assert.equal(director.active,false,'direct manipulation cancels active idle');
  director.preview('plane_play');director.update(.1,'waiting',context);assert.equal(director.behavior,'waiting');
  for(let i=0;i<60;i++){director.update(1,'idle',{...context,interaction:{...context.interaction,input:true}});assert.equal(director.active,false);}
  const s30=new Spring(),s60=new Spring();for(let i=0;i<30;i++)s30.step(.5,1/30);for(let i=0;i<60;i++)s60.step(.5,1/60);
  assert.ok(Math.abs(s30.value-s60.value)<1e-9,'motion must not depend on refresh rate');
  const bytes=readFileSync('frontend/public/models/caelis-soft-outfit-v1.glb');
  const gltf=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
  const pose=new PoseLayer(gltf.scene),head=gltf.scene.getObjectByName('head'),original=head.quaternion.clone();
  assert.ok(gltf.scene.getObjectByName('handR'),'reused rig supplies the prop attachment');
  // Constant channels skip mixer writes: repeated layer application must restore exactly.
  for(let i=0;i<600;i++){pose.restore();pose.apply(1/30,director,context);}
  pose.restore();assert.ok(head.quaternion.angleTo(original)<1e-7,'constant tracks must not accumulate offsets');
  for(const behavior of idleBehaviors){
   director.preview(behavior);
   for(let i=0;i<90;i++){
    director.update(1/30,'idle',context);pose.restore();pose.apply(1/30,director,context);
   }
   gltf.scene.updateMatrixWorld(true);const bounds=new Box3().setFromObject(gltf.scene,true);
   assert.ok(bounds.min.x> -1.05&&bounds.max.x<1.05&&bounds.min.y>-.5134&&bounds.max.y<2.2867,`${behavior} must fit actor surface`);
  }
  for(const direction of [-1,1]){
   const f={width:520,height:360,x:direction>0?80:440,y:80,direction};
   assert.ok(flightPoint(f,0).distanceTo(flightPoint(f,1))<1e-6,'prop returns to its release point');
   for(let i=0;i<=100;i++){const p=flightPoint(f,i/100);assert.ok(p.x>=20&&p.x<=500&&p.y>=20&&p.y<=340);}
  }
  const prop=readFileSync('frontend/public/models/xiaoai-paper-plane.glb');
  const report=await validator.validateBytes(new Uint8Array(prop),{uri:'xiaoai-paper-plane.glb'});
  assert.equal(report.issues.numErrors,0);assert.equal(report.issues.numWarnings,0);
 }finally{rmSync(dir,{recursive:true,force:true});}
});

for (const asset of ['caelis-soft-outfit-v1']) test(`${asset}: near gestures share expression timing, recover their arm pose and yield to input`,async()=>{
 mkdirSync('.cache',{recursive:true});const dir=mkdtempSync(resolve('.cache/near-test-'));
 try{
  for(const name of ['behavior','performance'])await build({configFile:false,logLevel:'silent',build:{ssr:resolve(`frontend/src/character/${name}.ts`),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:`${name}.mjs`}}}});
  const {LocalPerformance}=await import(pathToFileURL(join(dir,'performance.mjs')));
  const {BehaviorDirector,PoseLayer}=await import(pathToFileURL(join(dir,'behavior.mjs')));
  const director=new BehaviorDirector(()=>.5),timeline=new LocalPerformance();
  const context={pointer:{hovering:false},interaction:{input:false,dragging:false,pressing:false,menu:false}};
  const state={activity:'idle',clip:'idle',progress:0,director,context};
  timeline.preview('wave');let result;
  for(let i=0;i<35;i++)result=timeline.update(1/30,state);
  assert.equal(result.expression,'soft_smile');assert.ok(result.amount>.95);
  result=timeline.update(1/30,{...state,context:{...context,interaction:{...context.interaction,input:true}}});
  assert.equal(result.gesture,null,'opening input cancels the arm gesture');
  result=timeline.update(1/30,state);assert.equal(result.gesture,null,'closing input does not replay a stale gesture');
  const waiting={...state,activity:'waiting'};
  assert.equal(timeline.update(1/30,{...waiting,clip:'attention',progress:.5}).expression,'curious');
  assert.equal(timeline.update(1/30,waiting).gesture,'ask');
  for(let i=0;i<100;i++)result=timeline.update(1/30,waiting);
  assert.equal(result.gesture,null,'pending approval asks once, then waits quietly');
  const bytes=readFileSync(`frontend/public/models/${asset}.glb`);
  const gltf=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
  const pose=new PoseLayer(gltf.scene);
  pose.settle();
gltf.scene.updateMatrixWorld(true);
  const {Vector3}=await import('three');const hand=gltf.scene.getObjectByName('handL');
  const relaxed=hand.getWorldPosition(new Vector3()),relaxedOrientation=hand.quaternion.clone();pose.restore();
  for(const gesture of ['wave','think','ask']){
   timeline.update(0,state);timeline.preview(gesture);
   for(let frame=0;frame<190;frame++){
    result=timeline.update(1/30,state);pose.restore();pose.apply(1/30,director,undefined,false,undefined,result);
    if(['caelis-arms-v1','caelis-costume-v1','caelis-relaxed-v1','caelis-soft-outfit-v1'].includes(asset))for(const side of ['L','R']){
     const helper=gltf.scene.getObjectByName('forearm_twist'+side);
     assert.equal(gltf.scene.userData.desktopPetForearmTwist[side],0,'new arm has no static half-turn compensation');
     assert.ok(Math.abs(helper.quaternion.y)<.53,'gesture must not twist the forearm through a half turn');
    }
    if(frame===35||frame===80||frame===180){
     gltf.scene.updateMatrixWorld(true);const bounds=new Box3().setFromObject(gltf.scene,true);
     assert.ok(bounds.min.x> -1.05&&bounds.max.x<1.05&&bounds.min.y>-.5134&&bounds.max.y<2.2867,`${gesture} clipped`);
    }
   }
   assert.ok(hand.getWorldPosition(new Vector3()).distanceTo(relaxed)<.002,`${gesture} must return to relaxed wrists without drift`);
   assert.ok(hand.quaternion.angleTo(relaxedOrientation)<.002,`${gesture} must restore the relaxed wrist orientation`);
  }
  timeline.preview('wave');
  for(let i=0;i<35;i++){result=timeline.update(1/30,state);pose.restore();pose.apply(1/30,director,undefined,false,undefined,result);}
  const before=hand.getWorldPosition(new Vector3());
  result=timeline.update(1/30,{...state,context:{...context,interaction:{...context.interaction,input:true}}});
  pose.restore();pose.apply(1/30,director,undefined,false,undefined,result);
  assert.ok(hand.getWorldPosition(new Vector3()).distanceTo(before)<.06,'input interruption must blend the wrists back, not teleport');
  pose.rest();pose.settle();assert.ok(hand.getWorldPosition(new Vector3()).distanceTo(relaxed)<.002,'reduced motion keeps the relaxed stance');
 }finally{rmSync(dir,{recursive:true,force:true});}
});

for(const asset of ['caelis-soft-outfit-v1'])test(`${asset} resting mouth closes fully and opens only with expression changes`,async()=>{
 const bytes=readFileSync(`frontend/public/models/${asset}.glb`);
 const gltf=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
 assert.equal(gltf.scene.userData.desktopPetMouthRest,'closed');
 const dir=mkdtempSync(resolve('.cache/natural-face-test-'));
 try{
  for(const name of ['face','behavior'])await build({configFile:false,logLevel:'silent',build:{ssr:resolve(`frontend/src/character/${name}.ts`),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:`${name}.mjs`}}}});
  const {FacialAnimation}=await import(pathToFileURL(join(dir,'face.mjs'))),{BehaviorDirector}=await import(pathToFileURL(join(dir,'behavior.mjs')));
  const face=new FacialAnimation(gltf.scene,()=>.5),director=new BehaviorDirector(()=>.5),state={activity:'idle',clip:'idle',progress:0,director};
  assert.equal(face.weights.mouthClosed,1);
  for(let i=0;i<60;i++)face.update(1/30,state);
  assert.equal(face.weights.mouthClosed,1,'idle must retain the authored closed smile');
  for(let i=0;i<40;i++)face.update(1/30,{...state,clip:'celebrate',progress:.5});
  assert.ok(face.weights.mouthClosed<.15,'happy feedback may open the mouth');
  for(let i=0;i<60;i++)face.update(1/30,state);
  assert.ok(face.weights.mouthClosed>.999,'completion must return to closed mouth');
  face.rest();assert.equal(face.weights.mouthClosed,1);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
