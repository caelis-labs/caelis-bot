import { AnimationMixer, LoopOnce, LoopRepeat, type AnimationAction, type AnimationClip, type Object3D } from 'three';

export type Activity = 'idle' | 'working' | 'waiting';
export type Gesture = 'attention' | 'nod' | 'celebrate';
export const requiredClips = ['idle', 'working', 'attention', 'nod', 'celebrate'] as const;
export const isGesture = (name:string):name is Gesture => ['attention','nod','celebrate'].includes(name);

/** Presentation only. Native lifecycle facts select a loop; gestures never own work. */
export class CharacterAnimation {
 readonly mixer:AnimationMixer;
 private actions = new Map<string,AnimationAction>();
 private current:AnimationAction;
 private activity:Activity='idle';
 private fadeElapsed=0;
 private blend:Map<AnimationAction,number>|undefined;
 constructor(private root:Object3D, clips:AnimationClip[]) {
  for(const name of requiredClips)if(!clips.some(c=>c.name===name))throw new Error(`Missing character clip: ${name}`);
  this.mixer=new AnimationMixer(root);
  for(const clip of clips)this.actions.set(clip.name,this.mixer.clipAction(clip));
  this.current=this.actions.get('idle')!;
  this.play('idle',false);
  this.mixer.addEventListener('finished',this.finished);
 }
 get clip() { return this.current.getClip().name; }
 get progress() { return this.current.time/this.current.getClip().duration; }
 get oneShot() { return isGesture(this.clip); }
 private finished=({action}:{action:AnimationAction})=>{if(action===this.current&&this.oneShot)this.play(this.base());};
 private base() { return this.activity==='working'?'working':'idle'; }
 private play(name:string,fade=true) {
  const next=this.actions.get(name)!;
  if(fade&&next===this.current&&next.isRunning())return;
  const weights=new Map([...this.actions.values()].map(action=>[action,action.isScheduled()&&action.enabled?action.getEffectiveWeight():0]));
  for(const action of this.actions.values())action.stopFading();
  if(!next.isScheduled()||isGesture(name))next.reset();
  next.setEffectiveTimeScale(1).setLoop(isGesture(name)?LoopOnce:LoopRepeat,Infinity);
  next.clampWhenFinished=isGesture(name);
  if(fade){this.blend=weights;this.fadeElapsed=0;next.setEffectiveWeight(weights.get(next)??0);}
  else {this.mixer.stopAllAction();this.blend=undefined;next.reset().setEffectiveWeight(1);}
  this.current=next;next.play();this.mixer.update(0);
 }
 setActivity(activity:Activity,animate=true) {
  if(activity===this.activity)return;
  this.activity=activity;
  if(activity==='waiting'&&animate)this.play('attention');
  // Let feedback finish even when a tool's turn completes immediately after it.
  // Its completion callback returns to the latest authoritative base activity.
  else if(!animate||!this.oneShot)this.play(this.base());
 }
 gesture(name:Gesture) { this.play(name); }
 rest() { this.play(this.base(),false); }
 update(seconds:number) {
  if(this.blend){
   this.fadeElapsed+=seconds;
   const t=Math.min(1,this.fadeElapsed/.4),weight=t*t*t*(t*(t*6-15)+10);
   for(const [action,from]of this.blend)action.setEffectiveWeight(from+(Number(action===this.current)-from)*weight);
   if(t===1){for(const action of this.actions.values())if(action!==this.current)action.stop();this.blend=undefined;}
  }
  this.mixer.update(seconds);
 }
 dispose() { this.mixer.removeEventListener('finished',this.finished);this.mixer.stopAllAction();this.mixer.uncacheRoot(this.root); }
}
