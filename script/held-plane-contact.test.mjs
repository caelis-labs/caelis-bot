import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,rmSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {build} from 'vite';
import {Group,Quaternion,Vector3,Euler} from 'three';
const dir=mkdtempSync(resolve('.cache/plane-contact-test-'));
try{
 await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/character/held-plane.ts'),outDir:dir,rolldownOptions:{output:{entryFileNames:'anchor.mjs'}}}});
 const {HeldPlaneAnchor}=await import(pathToFileURL(join(dir,'anchor.mjs')));
 const setup=contact=>{
  const root=new Group(),hand=new Group(),thumb=new Group(),index=new Group(),plane=new Group();
  hand.name='handR';thumb.name='thumb02R';index.name='index03R';root.add(hand);hand.add(thumb,index);
  hand.position.set(-.3,.7,.1);thumb.position.set(.02,-.04,.01);index.position.set(.04,-.04,.01);plane.scale.setScalar(.28);
  root.userData.desktopPetFingerRig={version:1,gripBones:{R:{thumb:'thumb.02.R',index:'index.03.R',contact}}};
  return {root,hand,thumb,index,plane,anchor:new HeldPlaneAnchor(root,hand)};
 };
 test('calibrated paper grip follows finger pads and wrist rotation through a full turn',()=>{
  const contact={version:1,thumbOffset:[.002,.01,-.003],indexOffset:[-.002,.009,.004],rotation:new Quaternion().setFromEuler(new Euler(.2,-.5,.6)).toArray()};
  const {root,hand,thumb,index,plane,anchor}=setup(contact);const before=plane.quaternion.clone();
  for(const angle of [0,.3,1.2,Math.PI]){
   root.rotation.y=angle*.35;hand.rotation.set(angle,-angle*.2,angle*.4);thumb.rotation.x=angle*.1;index.rotation.z=-angle*.1;
   anchor.place(plane);
   const pinch=thumb.localToWorld(new Vector3().fromArray(contact.thumbOffset)).lerp(index.localToWorld(new Vector3().fromArray(contact.indexOffset)),.5);
   assert.ok(plane.localToWorld(new Vector3(-.20,-.099,0)).distanceTo(pinch)<1e-8);
   const rotation=hand.getWorldQuaternion(new Quaternion()).multiply(new Quaternion().fromArray(contact.rotation));
   assert.ok(rotation.angleTo(plane.quaternion)<1e-7);
  }
  assert.ok(before.angleTo(plane.quaternion)>.5);
 });
 test('hover follows the hand but stays upright through wrist and body turns',()=>{
  const {root,hand,plane}=setup(undefined);
  const presentation={version:1,mode:'hover',handOffset:[.02,.06,0],rootOffset:[-.04,.12,.05],rotation:new Quaternion().setFromEuler(new Euler(0,.4,.08)).toArray()};
  root.userData.desktopPetFingerRig.gripBones.R.presentation=presentation;
  const anchor=new HeldPlaneAnchor(root,hand);
  for(const angle of [0,.5,1.2,Math.PI]){
   root.rotation.y=angle;root.position.set(.1,.2,.3);hand.rotation.set(angle,angle*.2,-angle*.3);
   anchor.place(plane);
   const rotation=root.getWorldQuaternion(new Quaternion());
   const palm=hand.localToWorld(new Vector3().fromArray(presentation.handOffset));
   const expected=palm.clone().add(new Vector3().fromArray(presentation.rootOffset).applyQuaternion(rotation));
   assert.ok(plane.position.distanceTo(expected)<1e-8);
   assert.ok(plane.position.y-palm.y>.119);
   assert.ok(plane.quaternion.angleTo(rotation.multiply(new Quaternion().fromArray(presentation.rotation)))<1e-7);
  }
 });
 test('hover appears only with the palm ready and never unhides a released prop',()=>{
  const {root,hand,plane}=setup(undefined);
  root.userData.desktopPetFingerRig.gripBones.R.presentation={version:1,mode:'hover',handOffset:[0,0,0],rootOffset:[0,.1,0],rotation:[0,0,0,1],handRotation:[0,0,0,1]};
  const anchor=new HeldPlaneAnchor(root,hand);
  hand.rotation.x=1;plane.visible=true;anchor.place(plane);assert.equal(plane.visible,false);
  hand.rotation.x=0;anchor.place(plane);assert.equal(plane.visible,false);
  plane.visible=true;anchor.place(plane);assert.equal(plane.visible,true);
 });
 test('invalid hover metadata falls back instead of applying unbounded offsets',()=>{
  for(const presentation of [{version:2,mode:'hover'},
   {version:1,mode:'hover',handOffset:[0,0,0],rootOffset:[0,10,0],rotation:[0,0,0,1]},
   {version:1,mode:'hover',handOffset:[0,0,0],rootOffset:[0,.1,0],rotation:[0,0,0,0]}]){
   const {root,hand,plane,anchor:legacy}=setup(undefined);legacy.place(plane);const expected=plane.position.clone();
   root.userData.desktopPetFingerRig.gripBones.R.presentation=presentation;
   new HeldPlaneAnchor(root,hand).place(plane);assert.ok(plane.position.distanceTo(expected)<1e-8);
  }
 });
 test('older or malformed contact metadata retains the legacy anchor',()=>{
  for(const contact of [undefined,{version:2},{version:1,thumbOffset:[0,0,0],indexOffset:[0,0,0],rotation:[0,0,0,0]},{version:1,thumbOffset:[NaN,0,0],indexOffset:[0,0,0],rotation:[0,0,0,1]}]){
   const {root,hand,thumb,index,plane,anchor}=setup(contact);hand.rotation.x=.9;root.rotation.y=.3;anchor.place(plane);
   const mid=thumb.getWorldPosition(new Vector3()).lerp(index.getWorldPosition(new Vector3()),.5);
   assert.ok(plane.localToWorld(new Vector3(-.20,-.099,0)).distanceTo(mid)<1e-8);
   const legacy=root.getWorldQuaternion(new Quaternion()).multiply(new Quaternion().setFromEuler(new Euler(1.05,0,-.12)));
   assert.ok(legacy.angleTo(plane.quaternion)<1e-7);
  }
 });
}finally{rmSync(dir,{recursive:true,force:true});}
