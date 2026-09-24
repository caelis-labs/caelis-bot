import { Euler, Quaternion, Vector3, type Object3D } from 'three';

const finiteTuple=(value:unknown,size:number):value is number[]=>Array.isArray(value)&&value.length===size&&value.every(Number.isFinite);
/** Locate the paper's lower fold between the posed thumb and index finger. */
export class HeldPlaneAnchor {
 private thumb:Object3D|undefined;private index:Object3D|undefined;
 private point=new Vector3();private offset=new Vector3();
 private tilt=new Quaternion().setFromEuler(new Euler(1.05,0,-.12));
 private hover:{handOffset:Vector3;rootOffset:Vector3;rotation:Quaternion;handRotation?:Quaternion}|undefined;
 private contact:{thumb:Vector3;index:Vector3;rotation:Quaternion}|undefined;
 constructor(private root:Object3D,private hand:Object3D){
  if(root.userData.desktopPetFingerRig?.version===1){
   const grip=root.userData.desktopPetFingerRig.gripBones?.R;
   if(typeof grip?.thumb==='string')this.thumb=root.getObjectByName(grip.thumb.replaceAll('.',''));
   if(typeof grip?.index==='string')this.index=root.getObjectByName(grip.index.replaceAll('.',''));
   const hover=grip?.presentation;
   if(hover?.version===1&&hover.mode==='hover'&&finiteTuple(hover.handOffset,3)&&finiteTuple(hover.rootOffset,3)&&finiteTuple(hover.rotation,4)){
    const rotation=new Quaternion().fromArray(hover.rotation);
    if(rotation.lengthSq()>1e-8&&[...hover.handOffset,...hover.rootOffset].every(v=>Math.abs(v)<=.3)){
     const palm=finiteTuple(hover.handRotation,4)?new Quaternion().fromArray(hover.handRotation):undefined;
     this.hover={handRotation:palm&&palm.lengthSq()>1e-8?palm.normalize():undefined,handOffset:new Vector3().fromArray(hover.handOffset),rootOffset:new Vector3().fromArray(hover.rootOffset),rotation:rotation.normalize()};
    }
   }
   const contact=grip?.contact;
   if(contact?.version===1&&finiteTuple(contact.thumbOffset,3)&&finiteTuple(contact.indexOffset,3)&&finiteTuple(contact.rotation,4)){
    const rotation=new Quaternion().fromArray(contact.rotation);
    if(rotation.lengthSq()>1e-8&&[...contact.thumbOffset,...contact.indexOffset].every(v=>Math.abs(v)<.15)){
     this.contact={thumb:new Vector3().fromArray(contact.thumbOffset),index:new Vector3().fromArray(contact.indexOffset),rotation:rotation.normalize()};
    }
   }
  }
 }
 place(plane:Object3D){
  this.root.updateMatrixWorld(true);
  if(this.hover){
   // A stylized prop may hover above the hand without requiring a contact grip.
   // Position follows the hand; orientation and lift follow the character.
   const rootRotation=this.root.getWorldQuaternion(plane.quaternion);
   this.hand.localToWorld(plane.position.copy(this.hover.handOffset));
   plane.position.add(this.offset.copy(this.hover.rootOffset).applyQuaternion(rootRotation));
   if(this.hover.handRotation){
    const target=rootRotation.clone().multiply(this.hover.handRotation);
    // Materialize only after the palm is up. Never override the caller's
    // hidden state (release, interruption, reduced motion or native flight).
    plane.visible=plane.visible&&this.hand.getWorldQuaternion(new Quaternion()).angleTo(target)<.25;
   }
   plane.quaternion.multiply(this.hover.rotation);
   plane.updateMatrixWorld(true);
   return;
  }
  if(this.contact&&this.thumb&&this.index){
   this.hand.getWorldQuaternion(plane.quaternion);plane.quaternion.multiply(this.contact.rotation);
   this.thumb.localToWorld(plane.position.copy(this.contact.thumb));
   this.index.localToWorld(this.point.copy(this.contact.index));
   plane.position.lerp(this.point,.5);
  }else{
   this.root.getWorldQuaternion(plane.quaternion);plane.quaternion.multiply(this.tilt);
   if(this.thumb&&this.index){
    this.thumb.getWorldPosition(plane.position);
    plane.position.lerp(this.index.getWorldPosition(this.point),.5);
   }else{
    this.hand.getWorldPosition(plane.position);plane.position.z+=.09;plane.position.y-=.015;
   }
  }
  if(this.thumb&&this.index){
   this.offset.set(-.20,-.099,0).multiply(plane.scale).applyQuaternion(plane.quaternion);
   plane.position.sub(this.offset);
  }
  plane.updateMatrixWorld(true);
 }
}
