import { Euler, Quaternion, Vector3, type Object3D } from 'three';

/** Locate the paper's lower fold between the posed thumb and index finger. */
export class HeldPlaneAnchor {
 private thumb:Object3D|undefined;private index:Object3D|undefined;
 private point=new Vector3();private offset=new Vector3();
 private tilt=new Quaternion().setFromEuler(new Euler(1.05,0,-.12));
 constructor(private root:Object3D,private hand:Object3D){
  if(root.userData.desktopPetFingerRig?.version===1){
   const grip=root.userData.desktopPetFingerRig.gripBones?.R;
   if(typeof grip?.thumb==='string')this.thumb=root.getObjectByName(grip.thumb.replaceAll('.',''));
   if(typeof grip?.index==='string')this.index=root.getObjectByName(grip.index.replaceAll('.',''));
  }
 }
 place(plane:Object3D){
  this.root.updateMatrixWorld(true);
  this.root.getWorldQuaternion(plane.quaternion);plane.quaternion.multiply(this.tilt);
  if(this.thumb&&this.index){
   this.thumb.getWorldPosition(plane.position);
   plane.position.lerp(this.index.getWorldPosition(this.point),.5);
   this.offset.set(-.20,-.099,0).multiply(plane.scale).applyQuaternion(plane.quaternion);
   plane.position.sub(this.offset);
  }else{
   this.hand.getWorldPosition(plane.position);plane.position.z+=.09;plane.position.y-=.015;
  }
  plane.updateMatrixWorld(true);
 }
}
