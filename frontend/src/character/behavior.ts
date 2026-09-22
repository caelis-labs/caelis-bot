import { Euler, Quaternion, Vector3, type Object3D } from 'three';
import { gestureScore } from './gesture-score';
import { groundedPose } from './grounded-pose';
import { NearGesturePose } from './near-gesture';
import { HandPoseLayer } from './hand-pose';
import type { PerformanceFrame } from './performance';
import type { Activity } from './animation';
import { interactionBusy, type DesktopContext } from './context';

export const idleBehaviors = ['breathe','observe','weight_shift','plane_care','stretch','plane_play'] as const;
export type IdleBehavior = typeof idleBehaviors[number];
type Behavior = IdleBehavior|'focus'|'inspect'|'waiting';
const active = (b:Behavior) => b==='stretch'||b==='plane_play';
const ease=(t:number)=>{t=Math.max(0,Math.min(1,t));return t*t*t*(t*(t*6-15)+10);};

/** Local behavior, independent of Agent runs. Time advances only while visible. */
export class BehaviorDirector {
 behavior:Behavior='breathe'; elapsed=0; duration=10;
 private clock=0; private activeAfter=18; private launched=false;
 private work:Activity='idle'; private hover=false;
 private hoverResponse=0;
 constructor(private random= Math.random) {}
 get active(){return active(this.behavior);}
 get phase(){return this.elapsed/this.duration;}
 get envelope(){return ease(this.elapsed/.9)*ease((this.duration-this.elapsed)/.9);}
 get touch(){return Math.max(0,this.hoverResponse);}
 /** Props are brought out for their own performance, never a permanent accessory. */
 get holdsPlane(){return this.behavior==='plane_care'||this.behavior==='plane_play'&&!this.launched;}
 touched(){this.hoverResponse=.8;}
 private select(name:Behavior,duration:number){this.behavior=name;this.elapsed=0;this.duration=duration;this.launched=false;}
 preview(name:IdleBehavior){this.select(name,name==='plane_play'?10:8);}
 reset(){this.select(this.work==='working'?'focus':this.work==='waiting'?'waiting':'breathe',10);this.hover=false;this.hoverResponse=0;}
 update(dt:number,work:Activity,context?:DesktopContext,oneShot=false):boolean {
  this.clock+=dt;this.elapsed+=dt;this.hoverResponse=Math.max(0,this.hoverResponse-dt);
  const hover=!!context?.pointer.hovering;
  if(hover&&!this.hover)this.hoverResponse=.8;
  this.hover=hover;
  const busy=interactionBusy(context)||hover||oneShot;
  if(work!==this.work){this.work=work;this.select(work==='working'?'focus':work==='waiting'?'waiting':'breathe',12);}
  if((busy||work!=='idle')&&(this.active||this.behavior==='plane_care'))this.select(work==='working'?'focus':work==='waiting'?'waiting':'breathe',10);
  if(this.elapsed>=this.duration){
   if(work==='working')this.select(this.behavior==='focus'?'inspect':'focus',12+this.random()*8);
   else if(work==='waiting')this.select('waiting',12);
   else {
    const candidates=idleBehaviors.filter(b=>b!==this.behavior&&(!active(b)||(!busy&&this.clock>=this.activeAfter)));
    const weights=candidates.map(b=>active(b) ? .30/2 : .70/4);
    let choice=this.random()*weights.reduce((a,b)=>a+b,0),index=0;
    for(;index<weights.length-1&&choice>=weights[index];index++)choice-=weights[index];
    const next=candidates[index];this.select(next,active(next)?(next==='plane_play'?10:7):10+this.random()*8);
    if(active(next))this.activeAfter=this.clock+30+this.random()*30;
   }
  }
  if(this.behavior==='plane_play'&&this.elapsed>=2.2&&!this.launched&&!busy){this.launched=true;return true;}
  return false;
 }
}

/** Critically damped, frame-rate independent; no overshoot or abrupt velocity reset. */
export class Spring {
 value=0; velocity=0;
 step(target:number,dt:number,settle=.3){
  const omega=4/settle,x=this.value-target,e=Math.exp(-omega*dt),v=this.velocity;
  this.value=target+(x+(v+omega*x)*dt)*e;
  this.velocity=(v-omega*(v+omega*x)*dt)*e;return this.value;
 }
 reset(){this.value=this.velocity=0;}
}

/** Restore pre-layer transforms even for constant tracks that PropertyMixer skips. */
export class PoseLayer {
 private bones=new Map<string,{node:Object3D; base:Quaternion; position:Vector3}>();
 private yaw=new Spring(); private pitch=new Spring(); private body=new Spring();
 private toss=new Spring(); private care=new Spring(); private stretch=new Spring(); private lean=new Spring();
 private near:NearGesturePose;
 private gestureBlend=new Spring(); private lastPerformance:PerformanceFrame|undefined;
 private hands:HandPoseLayer;
 private euler=new Euler(); private delta=new Quaternion(); private time=0;
 constructor(private root:Object3D){
  this.near=new NearGesturePose(root);
  this.hands=new HandPoseLayer(root);
  for(const name of ['root','leg.L','leg.R','knee.L','knee.R','foot.L','foot.R','head','chest','upper_arm.L','upper_arm.R','forearm.L','forearm.R','forearm_twist.L','forearm_twist.R','hand.L','hand.R','hair.L','hair.R','hair.back']){
   const node=root.getObjectByName(name.replaceAll('.','')); // GLTFLoader sanitizes bone names.
   const found=node??root.getObjectByName(name);
   if(found)this.bones.set(name,{node:found,base:found.quaternion.clone(),position:found.position.clone()});
  }
 }
 capture(){for(const b of this.bones.values()){b.base.copy(b.node.quaternion);b.position.copy(b.node.position);}this.hands.capture();}
 restore(){for(const {node,base,position}of this.bones.values()){node.quaternion.copy(base);node.position.copy(position);}this.hands.restore();this.root.rotation.y=0;}
 private rotate(name:string,x=0,y=0,z=0){const b=this.bones.get(name);if(b)b.node.quaternion.multiply(this.delta.setFromEuler(this.euler.set(x,y,z)));}
 apply(dt:number,director:BehaviorDirector,context?:DesktopContext,oneShot=false,follow?:{x:number;y:number},performance?:PerformanceFrame){
  this.capture();
  this.time+=dt;const t=this.time,b=director.behavior,envelope=director.envelope;
  const quiet=interactionBusy(context),hover=!!context?.pointer.hovering;
  let targetYaw=0,targetPitch=0;
  if(context&&(follow||hover||b==='observe'||b==='inspect')){
   const window=context.activeWindow.frame;
   const x=follow?follow.x:hover?context.pointer.x:window?window.x+window.width/2:context.desktop.workArea.x+context.desktop.workArea.width/2;
   const y=follow?follow.y:hover?context.pointer.y:window?window.y+window.height*.6:context.actor.y+context.actor.height*.7;
   targetYaw=Math.max(-.5,Math.min(.5,(x-context.actor.x-context.actor.width/2)/380));
   targetPitch=Math.max(-.15,Math.min(.18,(context.actor.y+context.actor.height*.7-y)/320));
  }else if(b==='observe'){targetYaw=Math.sin(t*.38)*.32;}
  const strength=oneShot||quiet?0:1;
  const yaw=this.yaw.step(targetYaw*strength,dt,hover?.3:1.2),pitch=this.pitch.step(targetPitch*strength,dt,hover?.3:1.2);
  const body=this.body.step(targetYaw*.45*strength,dt,.55);
  const care=this.care.step((b==='plane_care'||b==='plane_play')&&!quiet&&!oneShot?envelope:0,dt,.38);
  const toss=this.toss.step(b==='plane_play'&&!quiet&&!oneShot?ease((director.elapsed-1.35)/.6)*(1-ease((director.elapsed-2.4)/.8)):0,dt,.3);
  const stretch=this.stretch.step(b==='stretch'&&!quiet&&!oneShot?Math.sin(Math.PI*director.phase)*envelope:0,dt,.45);
  const lean=this.lean.step(b==='weight_shift'&&!quiet&&!oneShot?.075*envelope*Math.sin(director.phase*Math.PI*2):0,dt,.45);
  this.root.rotation.y=body;
  this.rotate('head',pitch+care*.13, yaw-body*.35, strength*(.012*Math.sin(t*1.1)+director.touch*.035));
  this.rotate('chest',care*.025-stretch*.07-toss*.025,body*.14,lean);
  // Offsets remain small on the reused rig; follow-through trails the head/chest.
  this.rotate('upper_arm.L',stretch*.22,care*.07,-stretch*.24-care*.18);
  this.rotate('upper_arm.R',stretch*.17+toss*.15,toss*.08,stretch*.2+care*.26+toss*.18);
  this.rotate('forearm.R',care*.2-toss*.18,0,care*.16+toss*.22);
  this.rotate('forearm.L',care*.1,0,-care*.08);
  this.rotate('hair.L',0,0,strength*.014*Math.sin(t*1.1-.7));
  this.rotate('hair.R',0,0,strength*.012*Math.sin(t*1.1-1));
  this.rotate('hair.back',strength*.012*Math.sin(t*.85-.8));
  // A performance owns the whole posture. Props and one-shot clips keep their
  // own ownership, and direct interaction fades the standing pose away.
  const fittedStretch=this.root.userData.desktopPetSoftOutfit?.version===1;
  const nearEnabled=!oneShot&&b!=='plane_care'&&b!=='plane_play'&&(b!=='stretch'||fittedStretch);
  const present=!quiet&&nearEnabled&&b!=='stretch'&&!!performance?.gesture;
  if(present)this.lastPerformance=performance;
  const blend=this.gestureBlend.step(present?performance!.amount:0,dt,.36);
  const anatomical=this.root.userData.desktopPetArmMotion?.version===1;
  if(anatomical&&nearEnabled&&this.lastPerformance&&blend>.00001){
   const score=gestureScore({...this.lastPerformance,amount:blend});
   groundedPose(this.root,score.shift,score.drop,score.roll,score.pitch);
   this.rotate('chest',...score.chest);this.rotate('head',...score.head);
   this.rotate('hair.back',-score.pitch*.35,0,-score.roll*.3);
  }else if(anatomical&&nearEnabled&&Math.abs(lean)>.00001){
   groundedPose(this.root,-lean*.32,Math.abs(lean)*.12,-lean*.4,0);
   this.rotate('head',0,0,-lean*.9);
  }
  this.near.apply(dt,quiet||!nearEnabled||b==='stretch'?undefined:performance,nearEnabled,undefined,fittedStretch?stretch:0);
  const gestureWeight=quiet?0:performance?.amount??0;
  if(!anatomical){
   if(performance?.gesture==='think')this.rotate('head',gestureWeight*.065,0,gestureWeight*-.035);
   if(performance?.gesture==='ask')this.rotate('head',0,0,gestureWeight*.07);
   if(performance?.gesture==='wave')this.rotate('head',0,0,gestureWeight*-.025);
  }
  this.hands.apply(dt,{performance:quiet||!nearEnabled?undefined:performance,dragging:context?.interaction.dragging,holding:director.holdsPlane&&!quiet&&!hover&&!oneShot});
 }
 settle(){this.near.reset();this.near.apply(0,undefined,true);this.hands.apply(0);}
 rest(){this.near.reset();this.restore();this.hands.reset();this.yaw.reset();this.pitch.reset();this.body.reset();this.care.reset();this.toss.reset();this.stretch.reset();this.lean.reset();this.gestureBlend.reset();this.lastPerformance=undefined;}
}
