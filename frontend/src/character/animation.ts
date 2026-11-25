import { AnimationMixer, LoopOnce, LoopRepeat, type AnimationAction, AnimationClip, type Object3D } from 'three';

export type Activity = 'idle' | 'working' | 'waiting' | 'dreaming';
export const activities:readonly Activity[] = ['idle','working','waiting','dreaming'];
export type Gesture = 'attention' | 'nod' | 'celebrate';
export const gestures:readonly Gesture[] = ['attention','nod','celebrate'];
export type Feedback = Gesture | 'touch' | 'waiting';
export type ClipMapping = {
 idle:readonly string[];
 activities?:Partial<Record<Exclude<Activity,'idle'>,string>>;
 feedback?:Partial<Record<Feedback,string>>;
};

/** Native activity selects a loop. Only a feedback request selects LoopOnce. */
export class CharacterAnimation {
 readonly mixer:AnimationMixer;
 private actions = new Map<string,AnimationAction>();
 private idle:AnimationAction[];
 private idleAction:AnimationAction;
 private current:AnimationAction;
 private quietRest:AnimationAction;
 private activity:Activity='idle';
 private reduced=false;
 private transition:'idle'|'base'|undefined;
 private fadeElapsed=0;
 private blend:Map<AnimationAction,number>|undefined;
 feedback:Feedback|undefined;

 constructor(private root:Object3D, clips:AnimationClip[], private mapping:ClipMapping, {
  strict=false, quietIdle=false, random=Math.random,
 }:{strict?:boolean;quietIdle?:boolean;random?:()=>number}={}) {
  this.random=random;
  this.quietIdle=quietIdle;
  const names=new Set([...mapping.idle,...Object.values(mapping.activities??{}),...Object.values(mapping.feedback??{})]);
  const available=new Map(clips.filter(clip=>clip.duration>0).map(clip=>[clip.name,clip]));
  for(const name of names)if(strict&&!available.has(name))throw new Error(`Missing character clip: ${name}`);
  this.mixer=new AnimationMixer(root);
  for(const name of names){
   const clip=available.get(name);
   if(clip)this.actions.set(name,this.mixer.clipAction(clip));

  }
  this.idle=mapping.idle.flatMap(name=>this.actions.get(name)??[]);
  if(!this.idle.length)this.idle=[this.mixer.clipAction(new AnimationClip('',1,[]))];
  this.idleAction=this.current=this.idle[0];
  this.allActions=[...new Set([...this.actions.values(),...this.idle])];
  this.quietRest=this.mixer.clipAction(this.current.getClip().clone());
  this.play(this.current,undefined,false);
  this.mixer.addEventListener('finished',this.finished);
  this.mixer.addEventListener('loop',this.looped);
 }
 private readonly random:()=>number;
 private readonly quietIdle:boolean;
 private readonly allActions:AnimationAction[];

 get clip(){return this.current.getClip().name;}
 get progress(){return this.current.time/this.current.getClip().duration;}
 get oneShot(){return this.feedback!==undefined;}
 get animates(){return this.reduced?this.quietIdle&&this.idle[0].getClip().tracks.length>0:this.actions.size>0;}

 private finished=({action}:{action:AnimationAction})=>{
  if(action===this.current&&this.oneShot)this.transition='base';
 };
 private looped=({action}:{action:AnimationAction})=>{
  if(action===this.current&&!this.oneShot&&this.activity==='idle'&&!this.reduced&&this.idle.length>1)this.transition='idle';
 };
 private base(){
  if(this.reduced)return this.idle[0];
  return (this.activity!=='idle'&&this.actions.get(this.mapping.activities?.[this.activity]??''))||this.idleAction;
 }
 private play(next:AnimationAction,feedback?:Feedback,fade=true){
  if(fade&&next===this.current&&feedback===this.feedback&&next.isRunning())return;
  const weights=new Map(this.allActions.map(action=>[action,action.isScheduled()&&action.enabled?action.getEffectiveWeight():0]));
  if(!next.isScheduled()||feedback!==undefined||this.feedback!==undefined&&next===this.current)next.reset();
  next.setEffectiveTimeScale(1).setLoop(feedback===undefined?LoopRepeat:LoopOnce,Infinity);
  next.clampWhenFinished=feedback!==undefined;
  if(fade){this.blend=weights;this.fadeElapsed=0;next.setEffectiveWeight(weights.get(next)??0);}
  else {this.mixer.stopAllAction();this.blend=undefined;next.reset().setEffectiveWeight(1);}
  this.current=next;this.feedback=feedback;next.play();this.mixer.update(0);
 }
 setReducedMotion(value:boolean){
  if(value===this.reduced)return;
  this.rest(value);
 }
 setActivity(activity:Activity,animate=true){
  if(activity===this.activity)return;
  this.activity=activity;
  if(!animate||this.reduced||activity==='dreaming'){this.rest();return;}
  // Completed feedback resolves against the newest activity, even while its
  // previous base is still fading. A waiting cue can start on a quiet entry.
  if(!this.oneShot){
   if(activity!=='waiting'||!this.react('waiting'))this.play(this.base());
  }
 }
 react(feedback:Feedback){
  const next=this.actions.get(this.mapping.feedback?.[feedback]??'');
  if(!next||this.reduced||this.activity==='dreaming')return false;
  this.play(next,feedback);return true;
 }
 rest(reduced=this.reduced){
  this.reduced=reduced;this.transition=undefined;this.play(this.base(),undefined,false);
  if(this.reduced)this.update(0);
 }
 update(seconds:number){
  if(this.reduced){
   if(this.quietIdle){
    // Blend a slow idle into its authored first frame to also reduce amplitude.
    this.quietRest.reset().setEffectiveWeight(.82).play();this.quietRest.paused=true;
    this.current.setEffectiveWeight(.18);this.mixer.update(seconds*.5);
   }
   return;
  }
  if(this.blend){
   this.fadeElapsed+=seconds;
   const t=Math.min(1,this.fadeElapsed/.4),weight=t*t*t*(t*(t*6-15)+10);
   for(const [action,from]of this.blend)action.setEffectiveWeight(from+(Number(action===this.current)-from)*weight);
   if(t===1){for(const action of this.allActions)if(action!==this.current)action.stop();this.blend=undefined;}
  }
  this.mixer.update(seconds);
  const transition=this.transition;this.transition=undefined;
  if(transition==='idle'){
   const candidates=this.idle.filter(action=>action!==this.idleAction);
   this.idleAction=candidates[Math.floor(this.random()*candidates.length)];
  }
  if(transition)this.play(this.base());
 }
 dispose(){
  this.mixer.removeEventListener('finished',this.finished);this.mixer.removeEventListener('loop',this.looped);
  this.mixer.stopAllAction();this.mixer.uncacheRoot(this.root);
 }
}
