import { observeCharacterCanvas, petWidth as width, petHeight as height } from './canvas-resolution';
import { characterSource, defaultCharacterKey, fallbackCharacter } from './assets';
import { observeAppearance, builtinAppearance, type Appearance } from '../appearance';
import { MeshBasicMaterial, OrthographicCamera, Scene, WebGLRenderer, WebGLRenderTarget } from 'three';
import { loadCharacter, type LoadedCharacter } from './load';
import { desktop, type Placement } from '../desktop';
import { activities, gestures, type Activity, type Gesture } from './animation';
import { characterProfile } from './profile';
import { CharacterMotion } from './character-motion';
import type { DesktopContext } from './context';
import { createHitMaskEncoder } from './hit-mask';

// Preserve the native 44-point foot anchor and the entire five-clip envelope.
const viewHeight=2.8,bottom=-viewHeight*44/height;
export function mountPet(canvas:HTMLCanvasElement,onError:()=>void):()=>void {
 const renderer=new WebGLRenderer({canvas,alpha:true,antialias:true});renderer.setClearColor(0,0);
 const scene=new Scene(),camera=new OrthographicCamera(-viewHeight*width/height/2,viewHeight*width/height/2,bottom+viewHeight,bottom,.1,20);
 camera.position.set(0,0,5);camera.updateMatrixWorld();
 const target=new WebGLRenderTarget(width,height),rgba=new Uint8Array(width*height*4),maskMaterial=new MeshBasicMaterial();
 const encodeMask=createHitMaskEncoder(width,height);
 const reduced=matchMedia('(prefers-reduced-motion: reduce)');
 let disposed=false,visible=true,contextLost=false,frame=0,timer=0,lastFrame=0,lastMask=0;
 let current:{character:LoadedCharacter;motion:CharacterMotion;profile:ReturnType<typeof characterProfile>}|undefined;
 let activity:Activity='idle',context:DesktopContext|undefined;
 let activityEvents=0,contextEvents=0,visibilityEvents=0;
 let pendingMask:string|null=null,maskBusy=false,lastSent='';
 const canDraw=()=>!disposed&&visible&&!!current&&!contextLost&&!document.hidden;
 const animates=()=>canDraw()&&current!.motion.animates;
 const acceptsFeedback=()=>canDraw()&&!reduced.matches&&activity!=='dreaming'&&!context?.interaction.dragging;

 async function uploadMask() {
  if(maskBusy)return;maskBusy=true;
  try{
   while(pendingMask!==null&&!disposed){
    const value=pendingMask;pendingMask=null;
    if(value!==lastSent){await desktop('SetPackedHitMask',value);lastSent=value;}
   }
  }catch{if(!disposed)onError();}finally{maskBusy=false;}
 }
 function updateMask() {
  const shadows=renderer.shadowMap.enabled;renderer.shadowMap.enabled=false;scene.overrideMaterial=maskMaterial;
  try{renderer.setRenderTarget(target);renderer.render(scene,camera);renderer.readRenderTargetPixels(target,0,0,width,height,rgba);}
  finally{renderer.setRenderTarget(null);scene.overrideMaterial=null;renderer.shadowMap.enabled=shadows;}
  const encoded=encodeMask(rgba);
  if(maskBusy||encoded!==lastSent){pendingMask=encoded;void uploadMask();}
 }
 function draw(now=performance.now(),forceMask=false) {
  if(!canDraw())return;
  current!.motion.view();renderer.shadowMap.needsUpdate=true;renderer.render(scene,camera);
  // At most 15 mask readbacks per second; coalesce IPC while a request is pending.
  if(forceMask||now-lastMask>=1000/15){lastMask=now;updateMask();}
 }
 function tick(now:number) {
  frame=0;if(!animates())return;
  const dt=Math.min((now-lastFrame)/1000,.1);lastFrame=now;
  current!.motion.update(dt,now,activity,context);draw(now);schedule();
 }
 function schedule() {
  // Throttle requests themselves to avoid waking the compositor at display rate.
  timer=window.setTimeout(()=>{timer=0;if(animates())frame=requestAnimationFrame(tick);},1000/(reduced.matches?20:30)-6);
 }
 function stopFrames() {
  clearTimeout(timer);cancelAnimationFrame(frame);timer=0;frame=0;
 }
 function redraw() {
  stopFrames();lastFrame=performance.now();
  draw(lastFrame,true);if(animates())schedule();
 }
 function lifecycleChanged() {
  current?.motion.rest(reduced.matches);redraw();
 }
 const visibility=(e:Event)=>{visibilityEvents++;visible=(e as CustomEvent<boolean>).detail;lifecycleChanged();};
 const activityChanged=(e:Event)=>{
  const value=(e as CustomEvent<Activity>).detail;if(!activities.includes(value))return;
  activityEvents++;activity=value;current?.motion.setActivity(activity,acceptsFeedback());redraw();
 };
 const contextChanged=(e:Event)=>{
  contextEvents++;context=(e as CustomEvent<DesktopContext>).detail;current?.motion.contextChanged(context);
 };
 const gesture=(e:Event)=>{
  const value=(e as CustomEvent<Gesture>).detail;
  if(gestures.includes(value)&&acceptsFeedback()&&current?.motion.react(value))redraw();
 };
 const touch=()=>{if(acceptsFeedback()&&current?.motion.react('touch'))redraw();};
 const lost=(e:Event)=>{e.preventDefault();contextLost=true;lifecycleChanged();};
 const restored=()=>{contextLost=false;lastSent='';redraw();};
 const events:Record<string,EventListener>={
  'pet-context':contextChanged,'pet-touch':touch,'pet-visibility':visibility,
  'pet-activity':activityChanged,'pet-gesture':gesture,
  'pet-face-preview':e=>{if(acceptsFeedback())current?.motion.preview('face',(e as CustomEvent<string>).detail,activity);},
  'pet-near-preview':e=>{if(acceptsFeedback())current?.motion.preview('near',(e as CustomEvent<string>).detail,activity);},
  'pet-preview':e=>{if(acceptsFeedback())current?.motion.preview('idle',(e as CustomEvent<string>).detail,activity);},
 };
 for(const [name,listener]of Object.entries(events))window.addEventListener(name,listener);
 const stopSizing=observeCharacterCanvas(renderer,draw);
 document.addEventListener('visibilitychange',lifecycleChanged);reduced.addEventListener('change',lifecycleChanged);
 canvas.addEventListener('webglcontextlost',lost);canvas.addEventListener('webglcontextrestored',restored);

 function releaseCurrent(){
  if(!current)return;
  const {character,motion,profile}=current;current=undefined;
  motion.dispose();profile.dispose();scene.remove(character.root);character.dispose();
 }
 let generation=0,requested='',latestAppearance=builtinAppearance;
 const selected=(value:Appearance)=>{
  if(disposed||value.revision<latestAppearance.revision)return;
  latestAppearance=value;
  const selection=JSON.stringify([value.key,value.model]);
  if(selection===requested)return;
  requested=selection;void load(value);
 };
 async function load(value:Appearance) {
  const mine=++generation,stale=()=>disposed||mine!==generation;
  let character:LoadedCharacter|undefined,motion:CharacterMotion|undefined,profile:ReturnType<typeof characterProfile>|undefined;
  try {
   try{character=await loadCharacter(characterSource(value.key,value.model));}
   catch(error){
    if(stale())return;
    if(value.key!==defaultCharacterKey)throw error;
    character=await loadCharacter(fallbackCharacter);
   }
   if(stale())return;
   motion=await CharacterMotion.load(character);
   if(stale())return;
   const a=activityEvents,c=contextEvents,v=visibilityEvents;
   const [placement,initialActivity,initialContext]=await Promise.all([desktop<Placement>('Placement'),desktop<Activity>('CharacterActivity'),desktop<DesktopContext|null>('DesktopContext')]);
   if(stale())return;
   if(visibilityEvents===v)visible=placement.visible;
   if(activityEvents===a)activity=initialActivity;
   if(contextEvents===c)context=initialContext??undefined;

   // Keep drawing the previous model while loading. Replace all layers together.
   motion.attach(scene,camera);
   motion.rest(reduced.matches);
   motion.setActivity(activity,visible&&!contextLost&&!document.hidden&&!reduced.matches);
   profile=characterProfile(renderer,scene,character.root,character.referenceLighting);
   stopFrames();releaseCurrent();pendingMask=null;lastSent='';
   scene.add(character.root);
   current={character,motion,profile};character=motion=profile=undefined;
   redraw();
  }catch{
   if(stale())return;
   if(value.key===defaultCharacterKey){onError();return;}
   const revision=latestAppearance.revision;
   try{
    const fallback=await desktop<Appearance>('FallbackAppearance',revision);
    if(!stale())selected(fallback);
   }catch{if(!stale())selected({...builtinAppearance,revision});}
  }finally{
   motion?.dispose();profile?.dispose();character?.dispose();
  }
 }
 const stopAppearance=observeAppearance(selected);
 return()=>{
  disposed=true;generation++;stopAppearance();stopFrames();pendingMask=null;releaseCurrent();
  for(const [name,listener]of Object.entries(events))window.removeEventListener(name,listener);
  stopSizing();document.removeEventListener('visibilitychange',lifecycleChanged);reduced.removeEventListener('change',lifecycleChanged);
  canvas.removeEventListener('webglcontextlost',lost);canvas.removeEventListener('webglcontextrestored',restored);
  target.dispose();maskMaterial.dispose();renderer.dispose();renderer.forceContextLoss();
 };
}
