import { Quaternion, type Object3D } from 'three';
import type { PerformanceFrame } from './performance';

export type HandPoseName='relaxed'|'open'|'softGrip'|'hold';
type Joint={node:Object3D;base:Quaternion;before:Quaternion;current:Quaternion;side:'L'|'R';poses:Record<HandPoseName,Quaternion>};
/** Authored local finger poses. Wrist/arm motion belongs to the existing arm solver. */
export class HandPoseLayer {
 private joints:Joint[]=[];
 private airplane=false;
 private relaxedArms=false;
 constructor(root:Object3D){
  this.airplane=root.userData.desktopPetArmMotion?.version===1;
  this.relaxedArms=root.userData.desktopPetRelaxedArms?.version===1;
  const profile=root.userData.desktopPetFingerRig;
  if(profile?.version!==1||!Array.isArray(profile.joints))return;
  for(const row of profile.joints){
   const node=root.getObjectByName(row.name.replaceAll('.',''))??root.getObjectByName(row.name);
   const poses={} as Record<HandPoseName,Quaternion>;
   for(const key of ['relaxed','open','softGrip','hold'] as const){
    const q=profile.poses?.[key]?.[row.name];
    if(Array.isArray(q)&&q.length===4&&q.every(Number.isFinite))poses[key]=new Quaternion().fromArray(q).normalize();
   }
   if(node&&Object.keys(poses).length===4&&(row.side==='L'||row.side==='R')){
    this.joints.push({node,side:row.side,base:node.quaternion.clone(),before:node.quaternion.clone(),current:poses.relaxed.clone(),poses});
   }
  }
 }
 get available(){return this.joints.length>0;}
 capture(){for(const joint of this.joints)joint.before.copy(joint.node.quaternion);}
 restore(){for(const joint of this.joints)joint.node.quaternion.copy(joint.before);}
 apply(dt:number,options:{performance?:PerformanceFrame;dragging?:boolean;holding?:boolean}={}){
  for(const joint of this.joints){
   let pose:HandPoseName='relaxed',amount=1;
   const frame=options.performance;
   if(options.dragging){pose='softGrip';amount=this.airplane?.18:.65;}
   else if(frame?.gesture==='wave'&&joint.side==='L'||frame?.gesture==='ask'&&(!this.relaxedArms||joint.side==='L')){pose='open';amount=frame!.amount*(frame!.gesture==='ask'?.7:1);}
   else if(frame?.gesture==='think'&&joint.side==='R'){pose='softGrip';amount=frame.amount;}
   else if(options.holding&&joint.side==='R')pose='hold';
   this.blend(joint,pose,amount,dt);
  }
 }
 /** Deterministic source/renderer pose review; not a product action endpoint. */
 preview(pose:HandPoseName){for(const joint of this.joints)this.blend(joint,pose,1,0);}
 private blend(joint:Joint,pose:HandPoseName,amount:number,dt:number){
  const target=joint.poses.relaxed.clone().slerp(joint.poses[pose],amount);
  if(dt===0)joint.current.copy(target);else joint.current.slerp(target,1-Math.exp(-dt/.12));
  joint.node.quaternion.copy(joint.base).multiply(joint.current);
 }
 reset(){this.restore();for(const joint of this.joints)joint.current.copy(joint.poses.relaxed);}
}
