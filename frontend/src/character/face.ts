import { Mesh, type Object3D } from 'three';
import { Spring } from './behavior';
import { expressionProfiles, facialTargets, neutralFace, envelope, ease, type ExpressionName, type FacialTarget } from './expression-profile';
import type { PerformanceFrame, PerformanceState } from './performance';
export { facialTargets } from './expression-profile';
export type FacePreview='blink'|'happy'|'curious'|'relaxed'|'soft_smile'|'joy'|'focused'|'expectant';
const closure=(t:number)=>t<0?0:t<.09?ease(t/.09):t<.125?1:1-ease((t-.125)/.17);
/** Morphs share the hand/face timeline. Quiet input still permits a brief soft response. */
export class FacialAnimation {
 readonly weights=neutralFace();
 private meshes:Mesh[]=[];private restClosed=.88;private refined=false;
 private springs=Object.fromEntries(facialTargets.map(k=>[k,new Spring()])) as Record<FacialTarget,Spring>;
 private blink=-1;private nextBlink:number;private hover=false;private warmth=0;
 private previewing:{kind:FacePreview;elapsed:number}|undefined;
 constructor(root:Object3D,private random=Math.random){
  this.restClosed=root.userData.desktopPetMouthRest==='closed'?1:.88;this.weights.mouthClosed=this.restClosed;
  root.traverse(o=>{if(o instanceof Mesh&&facialTargets.some(k=>o.morphTargetDictionary?.[k]!==undefined))this.meshes.push(o);});
  this.refined=this.meshes.some(m=>m.morphTargetDictionary?.eyeSmile!==undefined);
  this.nextBlink=2+random()*3;this.springs.mouthClosed.value=this.restClosed;this.write();
 }
 get available(){return this.meshes.length>0;}
 acknowledge(){this.warmth=1.2;}
 preview(kind:FacePreview){this.previewing={kind,elapsed:0};}
 private write(){
  for(const mesh of this.meshes){
   const refined=this.refined;
   for(const name of facialTargets){const index=mesh.morphTargetDictionary![name];if(index!==undefined)mesh.morphTargetInfluences![index]=name==='happy'&&!refined?this.weights.eyeSmile:this.weights[name];}
  }
 }
 update(dt:number,state:PerformanceState,performance?:PerformanceFrame){
  if(!this.available)return;
  const {activity,clip,progress,context}=state;
  const hover=!!context?.pointer.hovering,hardQuiet=!!context&&(context.interaction.dragging||context.interaction.menu||context.interaction.pressing);
  const input=!!context?.interaction.input;
  if(hover&&!this.hover){this.warmth=.9;this.nextBlink=Math.min(this.nextBlink,.22);}
  this.hover=hover;this.warmth=Math.max(0,this.warmth-dt);
  this.nextBlink-=dt;
  if(this.blink<0&&this.nextBlink<=0){this.blink=0;this.nextBlink=3.2+this.random()*3.8;}
  if(this.blink>=0){this.blink+=dt;if(this.blink>.31)this.blink=-1;}
  let expression:ExpressionName=performance?.expression??(clip==='celebrate'?'joy':clip==='attention'?'curious':clip==='nod'?'soft_smile':activity==='working'?'focused':activity==='waiting'?'expectant':'relaxed');
  let amount=performance?.expressionAmount??(!['idle','working'].includes(clip)?envelope(progress,.01,.99,.17):1);
  if(this.warmth>0&&activity==='idle'&&!hardQuiet){expression='soft_smile';amount=Math.min(1,this.warmth/.25);}
  if(input){expression=activity==='waiting'?'expectant':this.warmth>0?'soft_smile':'relaxed';amount=activity==='waiting'?.4:1;}
  if(hardQuiet){expression='relaxed';amount=1;this.previewing=undefined;}
  if(this.previewing&&!hardQuiet&&!input){
   const p=this.previewing;p.elapsed+=dt;
   if(p.kind!=='blink'){expression=p.kind==='happy'?'joy':p.kind;amount=envelope(p.elapsed/4,0,1,.09);}
   if(p.elapsed>=4)this.previewing=undefined;
  }else if(input)this.previewing=undefined;
  const target=neutralFace(),preset=expressionProfiles[expression];target.mouthClosed=this.restClosed;
  for(const name of facialTargets){const base=target[name];target[name]=base+((preset[name]??0)*(name==='mouthClosed'?this.restClosed/.88:1)-base)*amount;}
  for(const name of facialTargets)this.weights[name]=Math.max(0,Math.min(1,this.springs[name].step(target[name],dt,name.startsWith('mouth')?.32:name.startsWith('brow')?.25:.18)));
  const previewBlink=this.previewing?.kind==='blink'?envelope(this.previewing.elapsed/4,0,1,.09):0;
  const left=Math.max(closure(this.blink),previewBlink),right=Math.max(closure(this.blink-.012),previewBlink);
  this.weights.blinkLeft=left*(1-this.weights.eyeSmile);this.weights.blinkRight=right*(1-this.weights.eyeSmile);
  this.weights.eyeFocused*=1-Math.max(left,right,this.weights.eyeSmile);
  // Mouth and lid targets are alternative endpoints, not cumulative deformations.
  const mouthKeys=['mouthClosed','mouthRound','mouthSmile','mouthFlat'] as const;
  const mouthSum=mouthKeys.reduce((n,k)=>n+this.weights[k],0);
  if(mouthSum>1)for(const k of mouthKeys)this.weights[k]/=mouthSum;
  this.write();
 }
 rest(){
  this.blink=-1;this.nextBlink=2+this.random()*3;this.hover=false;this.warmth=0;this.previewing=undefined;
  for(const spring of Object.values(this.springs))spring.reset();this.springs.mouthClosed.value=this.restClosed;
  Object.assign(this.weights,neutralFace(),{mouthClosed:this.restClosed});this.write();
 }
}
