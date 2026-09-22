import type { Activity } from './animation';
import type { BehaviorDirector } from './behavior';
import { interactionBusy, type DesktopContext } from './context';
import { envelope, type ExpressionName } from './expression-profile';
export type NearGesture='wave'|'think'|'ask';
export type PerformanceFrame={gesture:NearGesture|null;phase:number;amount:number;expression:ExpressionName;expressionAmount:number};
export type PerformanceState={activity:Activity;clip:string;progress:number;director:BehaviorDirector;context?:DesktopContext};
const rest:PerformanceFrame={gesture:null,phase:0,amount:0,expression:'relaxed',expressionAmount:1};
const duration:Record<NearGesture,number>={wave:2.4,think:4.8,ask:2.8};
/** Shared local timeline for face and hands; no native or Agent work is scheduled. */
export class LocalPerformance {
 private cue:{name:NearGesture;time:number}|undefined;
 private hovering=false;private clock=0;private nextGreeting=0;private activity:Activity='idle';
 private pendingAsk=false;
 preview(name:NearGesture){this.cue={name,time:0};}
 reset(){this.cue=undefined;this.hovering=false;this.pendingAsk=false;}
 update(dt:number,state:PerformanceState):PerformanceFrame{
  const {activity,clip,progress,director,context}=state;
  this.clock+=dt;
  const hover=!!context?.pointer.hovering,busy=interactionBusy(context),oneShot=!['idle','working'].includes(clip);
  if(activity!==this.activity){this.activity=activity;this.cue=undefined;this.pendingAsk=activity==='waiting';}
  if(hover&&!this.hovering&&!busy&&activity==='idle'&&this.clock>=this.nextGreeting){this.cue={name:'wave',time:-.25};this.nextGreeting=this.clock+24;}
  this.hovering=hover;
  if(busy){this.cue=undefined;}
  if(!oneShot&&!busy&&this.pendingAsk){this.cue={name:'ask',time:0};this.pendingAsk=false;}
  if(this.cue&&!oneShot){this.cue.time+=dt;if(this.cue.time>=duration[this.cue.name]){this.cue=undefined;}}
  if(oneShot){
   const expression=clip==='celebrate'?'joy':clip==='attention'?'curious':'soft_smile';
   return {...rest,expression,expressionAmount:envelope(progress,.01,.99,.17)};
  }
  if(busy)return {...rest,expression:activity==='waiting'?'expectant':'relaxed',expressionAmount:activity==='waiting'?.45:1};
  let gesture=this.cue?.name??null,phase=this.cue?Math.max(0,this.cue.time/duration[this.cue.name]):0;
  if(!gesture&&activity==='working'){
   gesture='think';phase=Math.max(0,Math.min(1,(director.elapsed-1.0)/6.0));
  }
  if(gesture){
   const amount=envelope(phase,0,1,.22);
   const expression=gesture==='wave'?'soft_smile':gesture==='ask'?'expectant':'focused';
   return {gesture,phase,amount,expression,expressionAmount:envelope(phase,-.025,.96,.18)};
  }
  if(activity==='waiting')return {...rest,expression:'expectant',expressionAmount:.42};
  if(activity==='working')return {...rest,expression:'focused',expressionAmount:.4};
  const joy=director.behavior==='stretch'?envelope(director.phase,.24,.72,.14):director.behavior==='plane_play'?envelope(director.phase,.8,1,.055):0;
  return {...rest,expression:joy?'joy':'relaxed',expressionAmount:joy||1};
 }
}
