import {defaultModel} from './shipped-character.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {Vector3,Quaternion} from 'three';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';

for(const asset of [defaultModel])test(`${asset}: standing gestures bend forward, retain planted soles and recover after interruption`,async()=>{
 const dir=mkdtempSync(resolve('.cache/gesture-motion-'));
 try{
  await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/character/behavior.ts'),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:'behavior.mjs'}}}});
  const {PoseLayer,BehaviorDirector}=await import(pathToFileURL(join(dir,'behavior.mjs')));
  const bytes=readFileSync(asset);
  const {scene}=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
  const get=n=>scene.getObjectByName(n),position=n=>get(n).getWorldPosition(new Vector3());
  const pose=new PoseLayer(scene),director=new BehaviorDirector(()=>.5);
  const baseline=new Map();scene.traverse(n=>{if(n.isBone)baseline.set(n.name,{q:n.quaternion.clone(),p:n.position.clone()});});
  scene.updateMatrixWorld(true);
  const feet=['footL','footR'].map(name=>({name,p:position(name),q:get(name).getWorldQuaternion(new Quaternion())}));
  for(const gesture of ['wave','think','ask']){
   let previous;
   for(let i=0;i<240;i++){
    const frame=i<120?{gesture,phase:.5,amount:Math.min(1,i/18),expression:'relaxed',expressionAmount:1}:undefined;
    pose.restore();pose.apply(1/60,director,undefined,false,undefined,frame);scene.updateMatrixWorld(true);
    for(const foot of feet){
     assert.ok(position(foot.name).distanceTo(foot.p)<.00002,`${gesture}: sole slid during body weight transfer`);
     assert.ok(get(foot.name).getWorldQuaternion(new Quaternion()).angleTo(foot.q)<.00002,`${gesture}: sole rotated through the floor`);
    }
    const points=['forearmL','forearmR','handL','handR'].map(position);
    if(previous)points.forEach((p,j)=>assert.ok(p.distanceTo(previous[j])<.035,`${gesture}: joint ${j} jumped ${p.distanceTo(previous[j])} at frame ${i}`));
    previous=points;
    if(i===100){
     const sides=gesture==='wave'||gesture==='ask'?['L']:gesture==='think'?['R']:['L','R'];
     {
      const passive=sides[0]==='L'?'R':'L';
      const s=position('upper_arm'+passive),e=position('forearm'+passive),w=position('hand'+passive);
      assert.ok(e.clone().sub(s).angleTo(w.clone().sub(e))<.35,`${gesture}: passive elbow must remain relaxed`);
      assert.ok(w.y<e.y-.1,`${gesture}: passive hand must hang below elbow`);
     }
     for(const side of sides){
      const shoulder=position('upper_arm'+side),elbow=position('forearm'+side),wrist=position('hand'+side);
      if(gesture==='ask'){
       // Low, open-handed inquiry is intentionally below the shoulder.
       assert.ok(wrist.y>elbow.y-.04&&wrist.y<shoulder.y,`${gesture}: low inquiry must reach forward at waist height`);
      }else assert.ok(wrist.y>elbow.y+.05,`${gesture}: raised forearm points down`);
      assert.ok(elbow.z>shoulder.z+.025,`${gesture}: elbow folds behind the body`);
      assert.ok(wrist.z>elbow.z+.025,`${gesture}: forearm points backward`);
     }
     assert.ok(get('root').position.distanceTo(baseline.get('root').p)>.008,`${gesture}: no body weight transfer`);
     assert.ok(get('head').quaternion.angleTo(baseline.get('head').q)>.06,`${gesture}: no coordinated head response`);
    }
   }
   pose.rest();
   for(const [name,base]of baseline){
    assert.ok(get(name).position.distanceTo(base.p)<1e-7,`${gesture}: ${name} position accumulated`);
    assert.ok(get(name).quaternion.clone().normalize().angleTo(base.q.clone().normalize())<1e-6,`${gesture}: ${name} rotation accumulated`);
   }
  }
  {
   // A complete idle stretch must retain the fitted near-body silhouette,
   // including its transition back to breathing (the old path exposed the A-pose).
   pose.rest();pose.settle();director.preview('stretch');
   let previous;
   for(let i=0;i<540;i++){
    director.update(1/60,'idle');pose.restore();pose.apply(1/60,director);scene.updateMatrixWorld(true);
    const wrists=[];
    for(const side of ['L','R']){
     const s=position('upper_arm'+side),e=position('forearm'+side),w=position('hand'+side);wrists.push(w);
     assert.ok(Math.abs(w.x-s.x)<.18,'stretch must not spread the arms into the old A-pose');
     assert.ok(e.clone().sub(s).angleTo(w.clone().sub(e))<.25,'stretch must not bow at the elbow');
     assert.ok(w.y<e.y-.12,'stretch keeps the hand below the elbow');
    }
    if(previous)wrists.forEach((w,j)=>assert.ok(w.distanceTo(previous[j])<.015,'stretch recovery must stay continuous'));
    previous=wrists;
   }
  }
  director.preview('plane_care');
  director.update(.01,'idle',{pointer:{hovering:true},interaction:{pressing:false,dragging:false,input:false,menu:false}});
  assert.equal(director.holdsPlane,false,'hover greeting takes ownership from a held prop');
 }finally{rmSync(dir,{recursive:true,force:true});}
});
