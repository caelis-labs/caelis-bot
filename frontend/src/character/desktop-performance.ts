import { Vector3, type Group, type Object3D, type OrthographicCamera, type Scene } from 'three';
import { desktop } from '../desktop';
import type { Activity, CharacterAnimation } from './animation';
import { petWidth, petHeight } from './canvas-resolution';
import { BehaviorDirector, PoseLayer, idleBehaviors, type IdleBehavior } from './behavior';
import { interactionBusy, type DesktopContext } from './context';
import { DragRunPose } from './drag-run';
import { FacialAnimation, type FacePreview } from './face';
import { HeldPlaneAnchor } from './held-plane';
import { releaseModel } from './load';
import { LocalPerformance, type NearGesture } from './performance';
import { flightPoint, type Flight } from './plane';
import { ViewCorrectives } from './view-correctives';

type FollowFlight=Flight & {originX:number;originY:number};
type ActiveFlight={id:string;accepted:boolean;interrupted:boolean;follow?:(FollowFlight & {started:number})};
export type PreviewKind='face'|'near'|'idle';

/** Procedural presentation for the reviewed desktop rig and its independent prop. */
export class DesktopPerformance {
 private director=new BehaviorDirector();
 private performance=new LocalPerformance();
 private pose:PoseLayer;
 private face:FacialAnimation;
 private views:ViewCorrectives;
 private running:DragRunPose;
 private anchor:HeldPlaneAnchor|undefined;
 private flight:ActiveFlight|undefined;
 private release=new Vector3();

 constructor(root:Object3D,private scene:Scene,private camera:OrthographicCamera,private plane?:Group){
  this.pose=new PoseLayer(root);this.face=new FacialAnimation(root);
  this.views=new ViewCorrectives(root);this.running=new DragRunPose(root);
  if(plane){
   plane.visible=false;plane.scale.setScalar(.28);scene.add(plane);
   const hand=root.getObjectByName('handR')??root.getObjectByName('hand.R');
   if(hand)this.anchor=new HeldPlaneAnchor(root,hand);
   window.addEventListener('pet-prop-result',this.propResult);
   window.addEventListener('pet-prop-flight',this.propFlight);
  }
 }
 private propResult=(event:Event)=>{
  if((event as CustomEvent<{id:string}>).detail.id===this.flight?.id)this.flight=undefined;
 };
 private propFlight=(event:Event)=>{
  const flight=(event as CustomEvent<FollowFlight>).detail;
  if(this.flight&&flight.id===this.flight.id)this.flight.follow={...flight,started:performance.now()};
 };
 private finish(id:string){void desktop('FinishPlane',id,false).catch(()=>{});}
 interrupt(){
  const flight=this.flight;this.flight=undefined;
  if(this.plane)this.plane.visible=false;
  if(!flight)return;
  flight.interrupted=true;
  if(flight.accepted)this.finish(flight.id);
 }
 private launch(){
  if(!this.plane||!this.anchor||this.flight)return;
  this.plane.getWorldPosition(this.release);this.release.project(this.camera);
  const flight={id:crypto.randomUUID(),accepted:false,interrupted:false};
  this.flight=flight;this.plane.visible=false;
  const clear=()=>{if(this.flight===flight)this.flight=undefined;};
  void desktop<boolean>('LaunchPlane',flight.id,(this.release.x+1)*petWidth/2,(this.release.y+1)*petHeight/2).then(accepted=>{
   if(!accepted){clear();return;}
   flight.accepted=true;
   // Input, visibility or replacement can change while admission is pending.
   if(flight.interrupted)this.finish(flight.id);
  }).catch(clear);
 }
 restore(){this.running.restore();this.pose.restore();}
 capture(){this.pose.capture();}
 resetPose(){this.running.rest();this.pose.rest();}
 settle(){
  this.interrupt();this.pose.capture();this.pose.settle();
  this.face.rest();this.performance.reset();this.director.reset();
 }
 touch(){this.director.touched();this.face.acknowledge();}
 preview(kind:PreviewKind,name:string,activity:Activity){
  if(kind==='face'&&['blink','happy','curious','relaxed','soft_smile','joy','focused','expectant'].includes(name))this.face.preview(name as FacePreview);
  if(kind==='near'&&['wave','think','ask'].includes(name))this.performance.preview(name as NearGesture);
  if(kind==='idle'&&activity==='idle'&&idleBehaviors.includes(name as IdleBehavior))this.director.preview(name as IdleBehavior);
 }
 update(dt:number,now:number,activity:Activity,context:DesktopContext|undefined,player:CharacterAnimation,quiet:boolean){
  const state={activity,feedback:player.feedback,progress:player.progress,director:this.director,context};
  if(quiet){this.face.update(dt,state);return;}
  const launch=this.director.update(dt,activity,context,player.oneShot);
  const flight=this.flight?.follow,point=flight&&flightPoint(flight,Math.min(1,(now-flight.started)/7500));
  const follow=point&&flight?{x:point.x+flight.originX,y:point.y+flight.originY}:undefined;
  const expression=this.performance.update(dt,state);
  this.pose.apply(dt,this.director,context,player.oneShot,follow,expression);
  this.running.apply(dt,context);
  this.face.update(dt,state,expression);
  if(this.plane&&this.anchor){
   this.plane.visible=activity==='idle'&&!player.oneShot&&!(expression.gesture&&expression.amount>.001)
    &&!interactionBusy(context)&&!context?.pointer.hovering&&!this.flight&&this.director.holdsPlane;
   this.anchor.place(this.plane);
   if(launch)this.launch();
  }
 }
 view(){this.views.apply(this.camera);}
 dispose(){
  this.interrupt();
  if(!this.plane)return;
  window.removeEventListener('pet-prop-result',this.propResult);
  window.removeEventListener('pet-prop-flight',this.propFlight);
  this.scene.remove(this.plane);releaseModel(this.plane);
 }
}
