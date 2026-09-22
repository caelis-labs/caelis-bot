import { Euler, Quaternion, Vector3, type Object3D } from 'three';
import { Spring } from './behavior';
import { NearGesturePose } from './near-gesture';
import { airplaneRun } from './airplane-run';
import type { DesktopContext } from './context';

const clamp=(x:number,min:number,max:number)=>Math.max(min,Math.min(max,x));

/** Observe existing native snapshots. Never write placement or call the backend. */
export class DragMotion {
 private snapshot:DesktopContext|undefined;
 private previous:{x:number;y:number;time:number}|undefined;
 private clock=0;private age=Infinity;private targetSpeed=0;private targetYaw=0;
 private speed=new Spring();private blend=new Spring();private facing=new Spring();
 phase=0;amount=0;yaw=0;pace=0;
 update(dt:number,context?:DesktopContext){
  this.clock+=dt;this.age+=dt;
  const dragging=!!context?.interaction.dragging;
  if(context!==this.snapshot){
   this.snapshot=context;
   if(dragging&&context){
    const time=context.sampledAtUptime??this.clock;
    const {x,y}=context.pointer;
    if(this.previous){
     const elapsed=time-this.previous.time;
     if(elapsed>.008&&elapsed<.6){
      const scale=clamp(context.actor.width/180,.65,1.6);
      const vx=(x-this.previous.x)/elapsed/scale,vy=(y-this.previous.y)/elapsed/scale;
      this.targetSpeed=clamp(Math.hypot(vx,vy),0,1400);this.age=0;
      if(this.targetSpeed>18)this.targetYaw=Math.abs(vx)>Math.abs(vy)*.22?Math.sign(vx)*1.15:0;
     }
    }
    this.previous={x,y,time};
   }else this.previous=undefined;
  }
  if(!dragging){this.previous=undefined;this.targetSpeed=0;this.targetYaw=0;}
  const speed=this.speed.step(dragging&&this.age<.22?this.targetSpeed:0,dt,.18);
  this.pace=clamp(speed/650,0,1);
  this.amount=this.blend.step(dragging?clamp((speed-12)/90,0,1):0,dt,dragging?.16:.30);
  this.yaw=this.facing.step(this.targetYaw,dt,.26);
  // Keep phase continuous through reversals and easing out; no repeated start frame.
  this.phase=(this.phase+dt*Math.PI*2*(1.35+this.pace*1.15)*this.amount)%(Math.PI*2);
  return this;
 }
 reset(){this.snapshot=undefined;this.previous=undefined;this.age=Infinity;this.targetSpeed=this.targetYaw=0;this.speed.reset();this.blend.reset();this.facing.reset();this.phase=this.amount=this.yaw=this.pace=0;}
}

type Bone={node:Object3D;bind:Quaternion;position:Vector3;before:Quaternion;beforePosition:Vector3};
/** A local gait over the authored rig. Native Window Server remains movement owner. */
export class DragRunPose {
 readonly motion=new DragMotion();
 readonly supported:boolean;
 private bones=new Map<string,Bone>();private nearRun:NearGesturePose;
 private rootPosition=new Vector3();private rootRotation=new Quaternion();private applied=false;
 private q=new Quaternion();private euler=new Euler();private xAxis=new Vector3(1,0,0);
 constructor(private root:Object3D){
  this.nearRun=new NearGesturePose(root);
  for(const name of ['leg.L','leg.R','knee.L','knee.R','foot.L','foot.R','chest','head','hair.back','hair.L','hair.R','upper_arm.L','upper_arm.R','forearm.L','forearm.R','forearm_twist.L','forearm_twist.R','hand.L','hand.R']){
   const node=root.getObjectByName(name.replaceAll('.',''))??root.getObjectByName(name);
   if(node)this.bones.set(name,{node,bind:node.quaternion.clone(),position:node.position.clone(),before:node.quaternion.clone(),beforePosition:node.position.clone()});
  }
  this.supported=root.userData.desktopPetLocomotion==='drag-run-v1'&&['leg.L','leg.R','knee.L','knee.R','foot.L','foot.R'].every(n=>this.bones.has(n));
 }
 restore(){
  if(!this.applied)return;
  for(const b of this.bones.values()){b.node.quaternion.copy(b.before);b.node.position.copy(b.beforePosition);}
  this.root.position.copy(this.rootPosition);this.root.quaternion.copy(this.rootRotation);this.applied=false;
 }
 private angle(name:string,angle:number,weight:number){
  const b=this.bones.get(name);if(!b)return;
  b.node.quaternion.slerp(this.q.copy(b.bind).multiply(new Quaternion().setFromAxisAngle(this.xAxis,angle)),weight);
 }
 apply(dt:number,context?:DesktopContext){
  const {amount,phase,pace,yaw}=this.motion.update(dt,context);
  if(!this.supported||amount<.00001)return;
  for(const b of this.bones.values()){b.before.copy(b.node.quaternion);b.beforePosition.copy(b.node.position);}
  this.rootPosition.copy(this.root.position);this.rootRotation.copy(this.root.quaternion);this.applied=true;
  const airplane=this.root.userData.desktopPetArmMotion?.version===1;
  const flight=airplaneRun(phase,pace);
  this.root.quaternion.slerp(this.q.setFromEuler(this.euler.set(airplane?flight.lean:.12+pace*.065,yaw,airplane?flight.bank:0,'YXZ')),amount);
  this.root.position.y+=amount*(.012+.020*(1-Math.cos(phase*2)));
  for(const [label,offset] of [['L',0],['R',Math.PI]] as const){
   const t=phase+offset,swing=Math.sin(t);
   const hip=-(.46+pace*.15)*swing;
   // Bend most during rear-leg recovery, then extend before the forward stride.
   // A half-cycle offset made the first version read as stiff straight-leg kicks.
   const recovery=Math.max(0,-Math.sin(t-Math.PI*.22));
   // Keep the returning heel below the long hair/skirt silhouette at pet size.
   const knee=.18+(1.10+pace*.16)*Math.pow(recovery,.72);
   this.angle('leg.'+label,hip,amount);this.angle('knee.'+label,knee,amount);
   const k=this.bones.get('knee.'+label)!,foot=this.bones.get('foot.'+label)!;
   const bend=new Quaternion().setFromAxisAngle(this.xAxis,knee);
   const ankle=foot.position.clone().sub(k.position).applyQuaternion(bend).add(k.position);
   foot.node.position.lerp(ankle,amount);
   // Original foot remains parented to the hip for compatibility with old clips.
   // During running its transform follows the new knee and adds a small ankle roll.
   const footRotation=bend.multiply(foot.bind).multiply(new Quaternion().setFromAxisAngle(this.xAxis,-.10*Math.max(0,swing)));
   foot.node.quaternion.slerp(footRotation,amount);
  }
  this.angle('chest',airplane?flight.chestPitch:-.025*Math.sin(phase*2),amount);
  this.angle('head',airplane?flight.headLift:-.04,amount);
  this.angle('hair.back',airplane?flight.hairTrail:.13+.025*Math.sin(phase-.5),amount);
  this.angle('hair.L',airplane?flight.sideHairTrail:.09,amount);this.angle('hair.R',airplane?flight.sideHairTrail:.09,amount);
  this.nearRun.apply(dt,undefined,true,{amount,phase,pace,airplane});
 }
 rest(){this.restore();this.motion.reset();this.nearRun.reset();}
}
