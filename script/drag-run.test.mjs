import {defaultModel} from './shipped-character.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import {Box3,Vector3} from 'three';

test('native-rate drag samples drive a bounded reversible gait without transform drift',async()=>{
 const dir=mkdtempSync(resolve('.cache/drag-test-'));
 try{
  for(const name of ['drag-run','behavior','animation'])await build({configFile:false,logLevel:'silent',build:{ssr:resolve(`frontend/src/character/${name}.ts`),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:`${name}.mjs`}}}});
  const {DragRunPose,DragMotion}=await import(pathToFileURL(join(dir,'drag-run.mjs')));
  const {PoseLayer,BehaviorDirector}=await import(pathToFileURL(join(dir,'behavior.mjs')));
  const {CharacterAnimation}=await import(pathToFileURL(join(dir,'animation.mjs')));
  const bytes=readFileSync(defaultModel);
  const gltf=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
  const player=new CharacterAnimation(gltf.scene,gltf.animations),pose=new PoseLayer(gltf.scene),run=new DragRunPose(gltf.scene),director=new BehaviorDirector(()=>.5);
  assert.ok(run.supported);
  const sample=(time,x,dragging=true)=>({revision:Math.round(time*8),sampledAtUptime:time,
   actor:{x:0,y:0,width:180,height:240},pointer:{x,y:0,hovering:false},
   interaction:{dragging,pressing:false,input:false,menu:false}});
  const saved=new Map();gltf.scene.traverse(o=>{if(o.isBone)saved.set(o.name,{q:o.quaternion.clone(),p:o.position.clone()});});
  let context,lastSample=-1,maxKnee=0,underlyingArms;
  const wrists={L:[],R:[]};
  for(let frame=0;frame<180;frame++){
   const time=frame/60,s=Math.floor(time*8);
   if(s!==lastSample){lastSample=s;context=sample(s/8,s<12?s*65:780-(s-12)*65);}
   run.restore();
   if(underlyingArms)for(const [node,q] of underlyingArms)assert.ok(node.quaternion.equals(q),`${node.name}: gait must restore the underlying animated pose`);
   pose.restore();player.update(1/60);director.update(1/60,'idle',context);pose.apply(1/60,director,context);
   underlyingArms=['upper_armL','upper_armR','forearmL','forearmR','forearm_twistL','forearm_twistR','handL','handR'].map(name=>{const node=gltf.scene.getObjectByName(name);return[node,node.quaternion.clone()];});
   run.apply(1/60,context);
   maxKnee=Math.max(maxKnee,gltf.scene.getObjectByName('kneeL').quaternion.angleTo(saved.get('kneeL').q));
   if(frame>45){
    gltf.scene.updateMatrixWorld(true);
    const local=name=>gltf.scene.worldToLocal(gltf.scene.getObjectByName(name).getWorldPosition(new Vector3()));
    for(const side of ['L','R']){
     const shoulder=local('upper_arm'+side),elbow=local('forearm'+side),wrist=local('hand'+side);
     const upper=elbow.clone().sub(shoulder),lower=wrist.clone().sub(elbow);
     assert.ok(gltf.scene.getObjectByName('forearm'+side).position.distanceTo(saved.get('forearm'+side).p)<1e-7,'upper arm never stretches');
     assert.ok(gltf.scene.getObjectByName('hand'+side).position.distanceTo(saved.get('hand'+side).p)<1e-7,'forearm never stretches');
     const bend=upper.angleTo(lower);assert.ok(bend>.16&&bend<.40,`soft airplane elbow: ${bend}`);
     assert.ok(wrist.z<shoulder.z-.015,'the wing hand stays gently behind the shoulder');
     assert.ok(Math.abs(wrist.x-shoulder.x)>.30,'wing silhouette extends outward');
    }
    const left=local('handL'),right=local('handR');wrists.L.push(left);wrists.R.push(right);
    assert.ok(left.x-right.x>.95,'both wings remain legible across the body');
   }
   if(frame===75)assert.ok(run.motion.yaw>1,'rightward drag faces right');
   if(frame===165)assert.ok(run.motion.yaw< -1,'reversing direction turns the same gait');
   if(frame%12===0){gltf.scene.updateMatrixWorld(true);const box=new Box3().setFromObject(gltf.scene,true);assert.ok(box.min.x> -1.05&&box.max.x<1.05&&box.min.y>-.5134&&box.max.y<2.2867,'running must fit the existing actor/mask surface');}
  }
  assert.ok(maxKnee>.9,'the rear leg must bend at the new knee');
  // Wings breathe with the step, without reverting to the former pumping arm arc.
  for(const side of ['L','R']){const excursion=Math.max(...wrists[side].flatMap(a=>wrists[side].map(b=>a.distanceTo(b))));assert.ok(excursion>.008&&excursion<.09,`${side}: restrained wing response ${excursion}`);}
  const speed=run.motion.pace;assert.ok(speed>.5);
  // Holding the mouse still gets no new samples: animation must settle anyway.
  for(let i=0;i<90;i++)run.motion.update(1/60,context);
  assert.ok(run.motion.amount<.002,'stale pointer samples must not run forever');
  // Explicit release is immediate input, with visual easing only.
  context=sample(5,780,false);
  for(let i=0;i<90;i++){run.restore();pose.restore();player.update(1/60);pose.apply(1/60,director,context);run.apply(1/60,context);}
  run.rest();pose.rest();player.rest();
  for(const name of saved.keys()){
   const node=gltf.scene.getObjectByName(name),original=saved.get(name);
   assert.ok(node.quaternion.clone().normalize().angleTo(original.q.clone().normalize())<1e-6,`${name}: rotation drift`);
   assert.ok(node.position.distanceTo(original.p)<1e-7,`${name}: position drift`);
  }
  assert.ok(gltf.scene.position.length()<1e-8,'root bob must not persist');
  const slow=new DragMotion(),fast=new DragMotion();
  for(let f=0;f<120;f++){
   const t=f/60,s=Math.floor(t*8)/8;
   if(f%8===0){slow.last=sample(s,s*100);fast.last=sample(s,s*800);}
   slow.update(1/60,slow.last);fast.update(1/60,fast.last);
  }
  assert.ok(fast.pace>slow.pace+.4,'drag speed must influence cadence');
  const before=fast.phase;fast.reset();assert.ok(before>0);assert.equal(fast.amount,0);assert.equal(fast.phase,0);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
