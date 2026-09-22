import { Quaternion, Vector3, type Object3D } from 'three';
import type { PerformanceFrame } from './performance';
import { gestureScore } from './gesture-score';
import { airplaneRun } from './airplane-run';
// Model-space wrist targets derived from the unchanged Xiaoai v0.4.1 rig.
// Finger articulation is an optional separate layer; this solver owns the wrists.
export const wristPoses={
 rest:{left:[.450,.722,-.020],right:[-.450,.722,-.020]},
 wave:{left:[.36,1.055,.26],right:[-.450,.722,-.020]},
 think:{left:[.22,.79,.22],right:[-.105,1.025,.25]},
 ask:{left:[.36,.86,.21],right:[-.36,.86,.21]},
} satisfies Record<string,{left:number[];right:number[]}>;
type Arm={upper:Object3D;lower:Object3D;hand:Object3D;side:1|-1;twistBone?:Object3D;twistBind?:Quaternion;neutralHand:Quaternion;restHand:Quaternion;localHand?:Quaternion;restWrist:Vector3;anchor?:Object3D;anchorWrist?:Vector3;anchorPole?:Vector3;wrist:Vector3;velocity:Vector3;restPole:Vector3;pole:Vector3;poleVelocity:Vector3;palm:Quaternion;previousUpper?:Quaternion;previousLower?:Quaternion};
function springPoint(current:Vector3,velocity:Vector3,target:Vector3,dt:number,response:number){
 if(dt===0){current.copy(target);velocity.set(0,0,0);return;}
 const omega=4/response,decay=Math.exp(-omega*dt),offset=current.clone().sub(target),rate=velocity.clone().addScaledVector(offset,omega);
 current.copy(target).add(offset.addScaledVector(rate,dt).multiplyScalar(decay));velocity.addScaledVector(rate,-omega*dt).multiplyScalar(decay);
}
/** Small two-link arm solver. All target ownership stays in the local presentation. */
export class NearGesturePose {
 private arms:Arm[]=[];private influence=1;
 constructor(private root:Object3D){
  for(const [suffix,side] of [['L',1],['R',-1]] as const){
   const get=(name:string)=>root.getObjectByName(`${name}${suffix}`)??root.getObjectByName(`${name}.${suffix}`);
   const upper=get('upper_arm'),lower=get('forearm'),hand=get('hand');
   if(upper&&lower&&hand){
    root.updateMatrixWorld(true);
    const neutralHand=root.getWorldQuaternion(new Quaternion()).invert().multiply(hand.getWorldQuaternion(new Quaternion()));
    const key=side===1?'left':'right';
    const wristData=root.userData.desktopPetWristRest?.[key],handData=root.userData.desktopPetHands?.[suffix];
    const restWrist=new Vector3().fromArray(Array.isArray(wristData)&&wristData.length===3&&wristData.every(Number.isFinite)?wristData:wristPoses.rest[key]);
    const restHand=neutralHand.clone();
    if(Array.isArray(handData)&&handData.length===4&&handData.every(Number.isFinite))restHand.premultiply(new Quaternion().fromArray(handData).normalize());
    const localData=root.userData.desktopPetHandLocal?.[suffix];
    const localHand=Array.isArray(localData)&&localData.length===4&&localData.every(Number.isFinite)
     ?hand.quaternion.clone().multiply(new Quaternion().fromArray(localData).normalize()):undefined;
    const twistBone=get('forearm_twist');
    const poleData=root.userData.desktopPetArmPole?.[key];
    const restPole=Array.isArray(poleData)&&poleData.length===3&&poleData.every(Number.isFinite)?new Vector3().fromArray(poleData):new Vector3(side*.46,.73,.20);
    const anchor=root.userData.desktopPetRelaxedArms?.version===1?upper.parent??undefined:undefined;
    const anchorWrist=anchor?.worldToLocal(root.localToWorld(restWrist.clone()));
    const anchorPole=anchor?.worldToLocal(root.localToWorld(restPole.clone()));
    this.arms.push({upper,lower,hand,side,anchor,anchorWrist,anchorPole,twistBone,twistBind:twistBone?.quaternion.clone(),restWrist,restHand,localHand,wrist:restWrist.clone(),velocity:new Vector3(),restPole,pole:restPole.clone(),poleVelocity:new Vector3(),palm:restHand.clone(),neutralHand});
   }
  }
 }
 apply(dt:number,frame:PerformanceFrame|undefined,enabled:boolean,run?:{amount:number;phase:number;pace:number;airplane?:boolean},stretch=0){
  this.influence+=(Number(enabled)-this.influence)*(1-Math.exp(-dt/.14));
  const influence=run?run.amount:this.influence;
  const anatomical=this.root.userData.desktopPetArmMotion?.version===1;
  const score=anatomical&&frame?.gesture?gestureScore(frame,this.root.userData.desktopPetRelaxedArms?.version===1):undefined;
  if(influence<.001&&!anatomical)return;
  for(const arm of this.arms){
   const key=arm.side===1?'left':'right';
   // The passive arm follows the chest rather than reaching for a fixed world point.
   this.root.updateMatrixWorld(true);
   const rest=arm.anchor&&arm.anchorWrist?this.root.worldToLocal(arm.anchor.localToWorld(arm.anchorWrist.clone())):arm.restWrist.clone();
   const target=rest.clone();
   const forward=run?(1-Math.sin(run.phase+(arm.side===1?0:Math.PI)))*.5:0;
   if(run){
    // Opposite arm to forward leg. The return hand stays outside/in front of
    // the hair envelope instead of swinging through the long rear locks.
    target.set(arm.side*(.43-.13*forward),.77+.15*forward,.07+.18*forward);
   }else if(stretch>0){
    // A small shoulder-led extension retains the fitted arm length and elbow
    // plane. Loose sleeves supply volume without making the skeleton bow out.
    const shoulder=this.root.worldToLocal(arm.upper.getWorldPosition(new Vector3()));
    target.sub(shoulder).applyAxisAngle(new Vector3(0,0,1),arm.side*.035*stretch)
     .applyAxisAngle(new Vector3(1,0,0),-.06*stretch).add(shoulder);
   }else if(frame?.gesture){
    const authored=score?.wrists[key]??this.root.userData.desktopPetGestures?.[frame.gesture]?.[key];
    const endpoint=Array.isArray(authored)&&authored.length===3&&authored.every(Number.isFinite)?authored:wristPoses[frame.gesture][key];
    const passive=score&&this.root.userData.desktopPetRelaxedArms?.version===1&&!score.wrists[key];
    const end=passive||frame.gesture==='wave'&&arm.side===-1?rest.clone():new Vector3().fromArray(endpoint);
    if(!score&&frame.gesture==='wave'&&arm.side===1)end.x+=.028*Math.sin((frame.phase-.2)*Math.PI*5);
    // Arms follow the expression by a short interval; recovery uses the same envelope.
    target.lerp(end,frame.amount);
   }
   const response=run?.10:anatomical?Math.max(.36,this.root.userData.desktopPetArmMotion.response):.28;
   springPoint(arm.wrist,arm.velocity,target,dt,response);
   target.copy(arm.wrist);
   this.root.updateMatrixWorld(true);this.root.localToWorld(target);
   const pole=run?new Vector3(arm.side*.46,.73,.20):arm.anchor&&arm.anchorPole?this.root.worldToLocal(arm.anchor.localToWorld(arm.anchorPole.clone())):arm.restPole.clone();
   if(score?.poles[key])pole.lerp(new Vector3().fromArray(score.poles[key]!),frame!.amount);
   // Retraction and interruption move the elbow plane with the wrist, avoiding
   // an instantaneous flip back to the low resting pole while the hand is high.
   if(anatomical){springPoint(arm.pole,arm.poleVelocity,pole,dt,response);pole.copy(arm.pole);}
   this.root.localToWorld(pole);
   if(run&&this.root.userData.desktopPetArmMotion?.version===1)this.solveRun(arm,forward,influence,run.airplane?airplaneRun(run.phase,run.pace,arm.side):undefined);
   else this.solve(arm,target,pole,influence);
   // Near a straight-arm configuration, a small pole change can otherwise
   // produce a fast elbow swivel. Limit joint angular travel, not bone length.
   // Running and authored clips retain their own timing and ownership.
   if(anatomical&&enabled&&!run&&dt>0&&arm.previousUpper&&arm.previousLower){
    arm.upper.quaternion.copy(arm.previousUpper.rotateTowards(arm.upper.quaternion,4*dt));
    arm.lower.quaternion.copy(arm.previousLower.rotateTowards(arm.lower.quaternion,5*dt));
    this.root.updateMatrixWorld(true);
   }
   arm.previousUpper=arm.upper.quaternion.clone();arm.previousLower=arm.lower.quaternion.clone();
   const rootWorld=this.root.getWorldQuaternion(new Quaternion());
   const parentWorld=arm.hand.parent!.getWorldQuaternion(new Quaternion());
   // New assets carry an authored local wrist pose: the relaxed hand follows the
   // forearm instead of holding a fixed world-space palm as the elbow moves.
   // Preserve the earlier projection for archived assets without this metadata.
   const desired=arm.localHand
    ?rootWorld.clone().invert().multiply(parentWorld).multiply(arm.localHand)
    :arm.restHand.clone();
   if(arm.localHand&&frame?.gesture&&frame.amount>0&&!run){
    const endpoint=this.root.userData.desktopPetHandGestures?.[frame.gesture]?.[arm.side===1?'L':'R'];
    if(Array.isArray(endpoint)&&endpoint.length===4&&endpoint.every(Number.isFinite)){
     const endpointRotation=new Quaternion().fromArray(endpoint).normalize().multiply(arm.neutralHand);
     const turn=anatomical?Math.min(1,this.root.userData.desktopPetArmMotion.wristTurnLimit/Math.max(1e-6,desired.angleTo(endpointRotation))):1;
     desired.slerp(endpointRotation,frame.amount*turn);
    }
   }
   if(!arm.localHand&&run){desired.slerp(arm.neutralHand,forward*.4);}
   else if(!arm.localHand&&frame?.gesture&&frame.amount>0){
    const rotation=frame.gesture==='ask'?new Quaternion().setFromAxisAngle(new Vector3(1,0,0),-.8):new Quaternion().setFromAxisAngle(new Vector3(0,0,1),arm.side*1.1);
    const shouldOrient=frame.gesture==='ask'||frame.gesture==='wave'&&arm.side===1||frame.gesture==='think'&&arm.side===-1;
    if(shouldOrient)desired.slerp(rotation.multiply(arm.neutralHand),frame.amount);
   }
   if(score&&frame?.gesture==='wave'&&arm.side===1)desired.premultiply(new Quaternion().setFromAxisAngle(new Vector3(0,0,1),score.rock));
   const palmTarget=rootWorld.clone().invert().multiply(arm.hand.getWorldQuaternion(new Quaternion())).slerp(desired,anatomical?1:influence);
   if(dt===0||!enabled||run&&this.root.userData.desktopPetArmMotion?.version===1)arm.palm.copy(palmTarget);else arm.palm.slerp(palmTarget,1-Math.exp(-dt/.075));
   if(enabled||anatomical)arm.hand.quaternion.copy(parentWorld.invert().multiply(rootWorld.multiply(arm.palm)));
   // Pronation belongs to forearm skin, not the loose sleeve or one wrist ring.
   // Follow the extra palm turn during gestures while retaining the authored rest.
   const twist=this.root.userData.desktopPetForearmTwist?.[arm.side===1?'L':'R'];
   if(arm.twistBone&&arm.twistBind&&arm.localHand&&typeof twist==='number'&&Number.isFinite(twist)){
    const change=arm.hand.quaternion.clone().multiply(arm.localHand.clone().invert());
    const raw=2*Math.atan2(change.y,change.w);
    const extra=anatomical?Math.atan2(Math.sin(raw),Math.cos(raw)):raw;
    arm.twistBone.quaternion.slerp(arm.twistBind.clone().multiply(new Quaternion().setFromAxisAngle(new Vector3(0,1,0),twist+extra)),anatomical?1:influence);
   }
  }
 }
 private solve({upper,lower,hand}:Arm,target:Vector3,pole:Vector3,weight:number){
  const uq=upper.quaternion.clone(),lq=lower.quaternion.clone();
  const shoulder=upper.getWorldPosition(new Vector3()),elbow=lower.getWorldPosition(new Vector3()),wrist=hand.getWorldPosition(new Vector3());
  const a=shoulder.distanceTo(elbow),b=elbow.distanceTo(wrist);
  if(a<1e-5||b<1e-5)return;
  const axis=target.clone().sub(shoulder),profile=this.root.userData.desktopPetArmMotion;
  const bounded=profile?.version===1;
  const minDistance=bounded?Math.sqrt(a*a+b*b+2*a*b*Math.cos(profile.maxFlexion)):Math.abs(a-b)+.001;
  const maxDistance=bounded?Math.sqrt(a*a+b*b+2*a*b*Math.cos(profile.minFlexion)):a+b-.002;
  const distance=Math.max(minDistance,Math.min(maxDistance,axis.length()));axis.normalize();
  const along=(a*a-b*b+distance*distance)/(2*distance),height=Math.sqrt(Math.max(0,a*a-along*along));
  const outward=pole.clone().sub(shoulder);outward.addScaledVector(axis,-outward.dot(axis));
  if(outward.lengthSq()<1e-8)return;
  const desiredElbow=shoulder.clone().addScaledVector(axis,along).addScaledVector(outward.normalize(),height);
  const desiredWrist=shoulder.clone().addScaledVector(axis,distance);
  this.aim(upper,elbow.clone().sub(shoulder),desiredElbow.clone().sub(shoulder));
  this.root.updateMatrixWorld(true);
  const currentElbow=lower.getWorldPosition(new Vector3()),currentWrist=hand.getWorldPosition(new Vector3());
  this.aim(lower,currentWrist.sub(currentElbow),desiredWrist.sub(currentElbow));
  upper.quaternion.slerp(uq,1-weight);lower.quaternion.slerp(lq,1-weight);
  this.root.updateMatrixWorld(true);
 }
 private solveRun({upper,lower,hand,side}:Arm,forward:number,weight:number,airplane?:ReturnType<typeof airplaneRun>){
  // Shoulder swing drives a stable bent elbow. The wrist follows this chain;
  // it is not an independently delayed point pulling the arm around a loop.
  const profile=this.root.userData.desktopPetArmMotion;
  const uq=upper.quaternion.clone(),lq=lower.quaternion.clone();
  const shoulder=upper.getWorldPosition(new Vector3()),elbow=lower.getWorldPosition(new Vector3()),wrist=hand.getWorldPosition(new Vector3());
  const a=shoulder.distanceTo(elbow),b=elbow.distanceTo(wrist);
  const upperDirection=airplane?new Vector3(...airplane.upper).normalize():new Vector3(side*Math.sin(profile.runAbduction),-Math.cos(profile.runAbduction),0);
  const hinge=upperDirection.clone().cross(new Vector3(0,0,1)).normalize();
  const lowerDirection=upperDirection.clone().applyAxisAngle(hinge,airplane?.flexion??profile.runFlexion+.12*(1-forward));
  const pitch=airplane?0:-.12-profile.runSwing*forward,axis=new Vector3(1,0,0),world=this.root.getWorldQuaternion(new Quaternion());
  upperDirection.applyAxisAngle(axis,pitch).applyQuaternion(world);
  lowerDirection.applyAxisAngle(axis,pitch).applyQuaternion(world);
  this.aim(upper,elbow.clone().sub(shoulder),upperDirection.multiplyScalar(a));
  this.root.updateMatrixWorld(true);
  this.aim(lower,hand.getWorldPosition(new Vector3()).sub(lower.getWorldPosition(new Vector3())),lowerDirection.multiplyScalar(b));
  upper.quaternion.slerp(uq,1-weight);lower.quaternion.slerp(lq,1-weight);
  this.root.updateMatrixWorld(true);
 }
 private aim(bone:Object3D,from:Vector3,to:Vector3){
  const delta=new Quaternion().setFromUnitVectors(from.normalize(),to.normalize());
  const world=bone.getWorldQuaternion(new Quaternion()).premultiply(delta);
  const parent=bone.parent!.getWorldQuaternion(new Quaternion()).invert();
  bone.quaternion.copy(parent.multiply(world));
 }
 reset(){this.influence=1;for(const arm of this.arms){arm.wrist.copy(arm.restWrist);arm.velocity.set(0,0,0);arm.pole.copy(arm.restPole);arm.poleVelocity.set(0,0,0);arm.previousUpper=arm.previousLower=undefined;}}
}
