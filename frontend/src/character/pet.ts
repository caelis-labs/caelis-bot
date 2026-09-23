import { characterAssets } from './assets';
import { observeAppearance, builtinAppearance, type Appearance } from '../appearance';
import { Mesh, MeshBasicMaterial, OrthographicCamera, Scene, Texture, WebGLRenderer, WebGLRenderTarget, Vector3, Box3, Group, type Material, type Object3D } from 'three';
import { loadCharacterModel } from './load';
import { desktop, type Placement } from '../desktop';
import { CharacterAnimation, isGesture, type Activity } from './animation';
import { characterProfile } from './profile';
import { LocalPerformance, type NearGesture } from './performance';
import { FacialAnimation, type FacePreview } from './face';
import { ViewCorrectives } from './view-correctives';
import { DragRunPose } from './drag-run';
import { BehaviorDirector, PoseLayer, idleBehaviors, type IdleBehavior } from './behavior';
import { interactionBusy, type DesktopContext } from './context';
import { disposePlane, flightPoint, type Flight } from './plane';
import { HeldPlaneAnchor } from './held-plane';

const width=180,height=240;
// Preserve the native 44-point foot anchor and the entire five-clip envelope.
const viewHeight=2.8,bottom=-viewHeight*44/height;
export function mountPet(canvas:HTMLCanvasElement,onError:()=>void):()=>void {
 const renderer=new WebGLRenderer({canvas,alpha:true,antialias:true});renderer.setClearColor(0,0);
 const scene=new Scene(),camera=new OrthographicCamera(-viewHeight*width/height/2,viewHeight*width/height/2,bottom+viewHeight,bottom,.1,20);
 camera.position.set(0,0,5);
 const target=new WebGLRenderTarget(width,height),rgba=new Uint8Array(width*height*4),maskMaterial=new MeshBasicMaterial();
 const reduced=matchMedia('(prefers-reduced-motion: reduce)');
 let disposed=false,visible=true,loaded=false,contextLost=false,frame=0,timer=0,lastFrame=0,lastMask=0;
 let root:Object3D|undefined,player:CharacterAnimation|undefined,profile:ReturnType<typeof characterProfile>|undefined;
 let activity:Activity='idle',activityEvents=0,contextEvents=0;
 let face:FacialAnimation|undefined;let views:ViewCorrectives|undefined;
 let running:DragRunPose|undefined;
 let context:DesktopContext|undefined,pose:PoseLayer|undefined,plane:Group|undefined,hand:Object3D|undefined;
 let planeAnchor:HeldPlaneAnchor|undefined;
 let flightID:string|undefined,nearAction=false;
 let flying:(Flight & {originX:number;originY:number;started:number})|undefined;
 const performanceDirector=new LocalPerformance();
 const director=new BehaviorDirector(),release=new Vector3();
 function cancelFlight(){const id=flightID;flightID=undefined;flying=undefined;if(id)void desktop('FinishPlane',id,false);}
 function attachPlane(){
  if(!plane||!hand||!root)return;
  plane.visible=animate()&&activity==='idle'&&!player?.oneShot&&!nearAction
   &&!interactionBusy(context)&&!context?.pointer.hovering&&!flightID&&director.holdsPlane;
  planeAnchor?.place(plane);
 }
 function launchPlane(){
  // The held prop has already been hidden on this release frame; its updated
  // hand anchor still supplies the detached surface's launch point.
  if(!plane||!hand||flightID||!animate())return;
  plane.getWorldPosition(release);release.project(camera);
  const id=crypto.randomUUID();flightID=id;plane.visible=false;
  void desktop<boolean>('LaunchPlane',id,(release.x+1)*width/2,(release.y+1)*height/2).then(accepted=>{
   if(!accepted&&flightID===id)flightID=undefined;
  }).catch(()=>{if(flightID===id)flightID=undefined;});
 }
 const propResult=(e:Event)=>{const result=(e as CustomEvent<{id:string}>).detail;if(result.id===flightID){flightID=undefined;flying=undefined;}};
 const propFlight=(e:Event)=>{const f=(e as CustomEvent<Flight & {originX:number;originY:number}>).detail;if(f.id===flightID)flying={...f,started:performance.now()};};
 const desktopChanged=(e:Event)=>{contextEvents++;context=(e as CustomEvent<DesktopContext>).detail;
  if(interactionBusy(context)||context.pointer.hovering)cancelFlight();
 };
 const touch=()=>{director.touched();face?.acknowledge();};
 const facePreview=(e:Event)=>{const name=(e as CustomEvent<string>).detail;if(['blink','happy','curious','relaxed','soft_smile','joy','focused','expectant'].includes(name)&&animate())face?.preview(name as FacePreview);};
 const preview=(e:Event)=>{const name=(e as CustomEvent<string>).detail;if(idleBehaviors.includes(name as IdleBehavior)&&activity==='idle'&&animate())director.preview(name as IdleBehavior);};
 const nearPreview=(e:Event)=>{const name=(e as CustomEvent<string>).detail;if(['wave','think','ask'].includes(name)&&animate())performanceDirector.preview(name as NearGesture);};
 let pendingMask:string|null=null,maskBusy=false,lastSent='';
 const canDraw=()=>!disposed&&visible&&loaded&&!contextLost&&!document.hidden;
 const animate=()=>canDraw()&&!reduced.matches&&!!player;
 function releaseModel(model:Object3D) {
  const materials=new Set<Material>(),textures=new Set<Texture>();
  model.traverse(o=>{if(o instanceof Mesh){o.geometry.dispose();for(const m of Array.isArray(o.material)?o.material:[o.material])materials.add(m);if('skeleton' in o)(o.skeleton as {dispose():void}).dispose();}});
  for(const m of materials){for(const value of Object.values(m))if(value instanceof Texture)textures.add(value);m.dispose();}
  textures.forEach(t=>t.dispose());
 }
 async function uploadMask() {
  if(maskBusy)return;maskBusy=true;
  try{while(pendingMask!==null&&!disposed){const value=pendingMask;pendingMask=null;await desktop('SetPackedHitMask',value);lastSent=value;}}
  catch{if(!disposed)onError();}finally{maskBusy=false;}
 }
 function updateMask() {
  const shadows=renderer.shadowMap.enabled;renderer.shadowMap.enabled=false;scene.overrideMaterial=maskMaterial;
  try{renderer.setRenderTarget(target);renderer.render(scene,camera);renderer.readRenderTargetPixels(target,0,0,width,height,rgba);}
  finally{renderer.setRenderTarget(null);scene.overrideMaterial=null;renderer.shadowMap.enabled=shadows;}
  const mask=new Uint8Array(width*height);
  // Bottom-up silhouette of the CURRENT skinned pose, with a small limb tolerance.
  for(let y=0;y<height;y++)for(let x=0;x<width;x++){
   if(rgba[(y*width+x)*4+3]<=16)continue;
   for(let dy=-2;dy<=2;dy++)for(let dx=-2;dx<=2;dx++)if(x+dx>=0&&x+dx<width&&y+dy>=0&&y+dy<height)mask[(y+dy)*width+x+dx]=1;
  }
  const packed=new Uint8Array(mask.length/8);
  for(let i=0;i<mask.length;i++)if(mask[i])packed[i>>3]|=1<<(i&7);
  const binary=String.fromCharCode(...packed);
  const encoded=btoa(binary);if(encoded!==lastSent){pendingMask=encoded;void uploadMask();}
 }
 function draw(now=performance.now(),forceMask=false) {
  if(!canDraw())return;
  views?.apply(camera);renderer.shadowMap.needsUpdate=true;renderer.render(scene,camera);
  // At most 15 mask readbacks per second; coalesce IPC while a request is pending.
  if(forceMask||now-lastMask>=1000/15){lastMask=now;updateMask();}
 }
 function tick(now:number) {
  frame=0;if(!animate())return;
  const dt=Math.min((now-lastFrame)/1000,.1);lastFrame=now;
  const launch=director.update(dt,activity,context,player!.oneShot);
  const point=flying?flightPoint(flying,Math.min(1,(now-flying.started)/7500)):undefined;
  const follow=point&&flying?{x:point.x+flying.originX,y:point.y+flying.originY}:undefined;
  running?.restore();pose?.restore();player!.update(dt);
  const state={activity,clip:player!.clip,progress:player!.progress,director,context};
  const expression=performanceDirector.update(dt,state);
  nearAction=!!expression.gesture&&expression.amount>.001;
  pose?.apply(dt,director,context,player!.oneShot,follow,expression);
  running?.apply(dt,context);
  face?.update(dt,state,expression);
  attachPlane();if(launch)launchPlane();draw(now);schedule();
 }
 function schedule() {
  // Throttle frame requests themselves: a permanent rAF still wakes WebKit's
  // compositor at the display refresh rate even when most callbacks skip drawing.
  timer=window.setTimeout(()=>{timer=0;if(animate())frame=requestAnimationFrame(tick);},1000/30-6);
 }
 function stopFrames() {
  clearTimeout(timer);cancelAnimationFrame(frame);timer=0;frame=0;
 }
 function resume() {
  stopFrames();lastFrame=performance.now();
  if(!canDraw()||reduced.matches){cancelFlight();running?.rest();pose?.rest();player?.rest();pose?.capture();pose?.settle();face?.rest();performanceDirector.reset();nearAction=false;director.reset();}
  if(!canDraw())return;
  attachPlane();
  draw(lastFrame,true);if(animate())schedule();
 }
 function resize() {
  // Cap actual backing pixels per on-screen point, including saved pet scale.
  renderer.setPixelRatio(Math.min(window.devicePixelRatio,1.5)*window.innerWidth/width);
  renderer.setSize(width,height,false);resume();
 }
 const visibility=(e:Event)=>{visible=(e as CustomEvent<boolean>).detail;resume();};
 const activityChanged=(e:Event)=>{
  const value=(e as CustomEvent<string>).detail;if(!['idle','working','waiting'].includes(value))return;
  activityEvents++;activity=value as Activity;running?.restore();pose?.restore();player?.setActivity(activity,animate());pose?.capture();if(activity!=='idle')cancelFlight();resume();
 };
 const gesture=(e:Event)=>{const value=(e as CustomEvent<string>).detail;if(!isGesture(value)||!animate())return;running?.restore();pose?.restore();player!.gesture(value);pose?.capture();cancelFlight();resume();};
 const lost=(e:Event)=>{e.preventDefault();contextLost=true;stopFrames();cancelFlight();};
 const restored=()=>{contextLost=false;lastSent='';resize();};
 window.addEventListener('pet-near-preview',nearPreview);window.addEventListener('pet-face-preview',facePreview);window.addEventListener('pet-context',desktopChanged);window.addEventListener('pet-touch',touch);window.addEventListener('pet-preview',preview);window.addEventListener('pet-prop-result',propResult);window.addEventListener('pet-prop-flight',propFlight);
 window.addEventListener('pet-visibility',visibility);window.addEventListener('pet-activity',activityChanged);window.addEventListener('pet-gesture',gesture);
 window.addEventListener('resize',resize);document.addEventListener('visibilitychange',resume);reduced.addEventListener('change',resume);
 canvas.addEventListener('webglcontextlost',lost);canvas.addEventListener('webglcontextrestored',restored);
 let generation=0,requested='',latestAppearance=builtinAppearance;
 const selected=(value:Appearance)=>{
  latestAppearance=value;
  if(value.key===requested)return;
  requested=value.key;void load(value);
 };
 async function load(value:Appearance) {
  const mine=++generation,stale=()=>disposed||mine!==generation;
  let incoming:Object3D|undefined,incomingPlane:Group|undefined,newPlayer:CharacterAnimation|undefined;
  let basic=value.basic;
  try {
   let gltf;
   try{gltf=await loadCharacterModel(value.model||characterAssets.model,!value.basic);}
   catch(error){
    if(value.key!=='builtin:caelis')throw error;
    gltf=await loadCharacterModel(characterAssets.fallback);basic=true;
   }
   incoming=gltf.scene;
   if(stale()){releaseModel(incoming);return;}
   if(basic){
    // Only basic geometry/clips are consumed from community files. Asset extras
    // cannot enable the default character's face/hand/desktop behavior metadata.
    const wrapper=new Group();wrapper.add(incoming);incoming=wrapper;
    const box=new Box3().setFromObject(wrapper),size=box.getSize(new Vector3());
    if(!Number.isFinite(size.length())||size.y<.001||size.x<.001)throw new Error('Invalid character bounds');
    const scale=Math.min(2.2/size.y,1.65/size.x);
    const center=box.getCenter(new Vector3());wrapper.scale.setScalar(scale);
    wrapper.position.set(-center.x*scale,-box.min.y*scale,-center.z*scale);
    wrapper.userData.desktopPetProfile='reference-v1';
   }
   newPlayer=new CharacterAnimation(incoming,gltf.animations,!basic);
   if(!basic){try{incomingPlane=(await loadCharacterModel(characterAssets.paperPlane)).scene;}catch{/* Optional built-in prop. */}}
   const observed=activityEvents,observedContext=contextEvents;
   const [placement,current,initialContext]=await Promise.all([desktop<Placement>('Placement'),desktop<Activity>('CharacterActivity'),desktop<DesktopContext|undefined>('DesktopContext')]);
   if(stale()){newPlayer.dispose();releaseModel(incoming);if(incomingPlane)disposePlane(incomingPlane);return;}
   // Commit only once the complete replacement is ready. Old content remains
   // visible during loading; late loads never replace a newer selection.
   stopFrames();cancelFlight();player?.dispose();profile?.dispose();
   if(root){scene.remove(root);releaseModel(root);}
   if(plane){scene.remove(plane);disposePlane(plane);}
   root=incoming;player=newPlayer;plane=incomingPlane;scene.add(root);
   profile=characterProfile(renderer,scene,root);
   pose=basic?undefined:new PoseLayer(root);running=basic?undefined:new DragRunPose(root);
   face=basic?undefined:new FacialAnimation(root);views=basic?undefined:new ViewCorrectives(root);
   hand=undefined;planeAnchor=undefined;
   if(plane){plane.visible=false;plane.scale.setScalar(.28);scene.add(plane);hand=root.getObjectByName('handR')??root.getObjectByName('hand.R');if(hand)planeAnchor=new HeldPlaneAnchor(root,hand);}
   performanceDirector.reset();director.reset();nearAction=false;lastSent='';
   visible=placement.visible;if(activityEvents===observed)activity=current;if(contextEvents===observedContext)context=initialContext;
   loaded=true;pose?.restore();player.setActivity(activity,visible&&!reduced.matches);pose?.capture();resize();
  }catch{
   newPlayer?.dispose();if(incoming&&incoming!==root)releaseModel(incoming);if(incomingPlane&&incomingPlane!==plane)disposePlane(incomingPlane);
   if(stale())return;
   if(value.key!=='builtin:caelis'){
    try{selected(await desktop<Appearance>('FallbackAppearance',latestAppearance.revision));}
    catch{selected({...builtinAppearance,revision:latestAppearance.revision});}
   }else onError();
  }
 }
 const stopAppearance=observeAppearance(selected);
 return()=>{
  disposed=true;generation++;stopAppearance();stopFrames();pendingMask=null;cancelFlight();
  window.removeEventListener('pet-near-preview',nearPreview);window.removeEventListener('pet-face-preview',facePreview);window.removeEventListener('pet-context',desktopChanged);window.removeEventListener('pet-touch',touch);window.removeEventListener('pet-preview',preview);window.removeEventListener('pet-prop-result',propResult);window.removeEventListener('pet-prop-flight',propFlight);
  window.removeEventListener('pet-visibility',visibility);window.removeEventListener('pet-activity',activityChanged);window.removeEventListener('pet-gesture',gesture);
  window.removeEventListener('resize',resize);document.removeEventListener('visibilitychange',resume);reduced.removeEventListener('change',resume);
  canvas.removeEventListener('webglcontextlost',lost);canvas.removeEventListener('webglcontextrestored',restored);
  player?.dispose();profile?.dispose();if(plane)disposePlane(plane);if(root)releaseModel(root);target.dispose();maskMaterial.dispose();renderer.dispose();renderer.forceContextLoss();
 };
}
