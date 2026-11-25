import type { Group, Object3D, OrthographicCamera, Scene } from 'three';
import type { Activity, Feedback } from './animation';
import { characterAssets } from './assets';
import { Spring } from './behavior';
import { interactionBusy, type DesktopContext } from './context';
import { DesktopPerformance, type PreviewKind } from './desktop-performance';
import { DragMotion } from './drag-run';
import { loadCharacterModel, releaseModel, type LoadedCharacter } from './load';

class PointerTurn {
 private drag=new DragMotion();
 private yaw=new Spring();
 constructor(private root:Object3D,private turn:number){}
 update(dt:number,activity:Activity,context?:DesktopContext){
  const drag=this.drag.update(dt,context);
  let direction=0;
  if(context?.interaction.dragging)direction=drag.yaw*drag.amount/1.15;
  else if(context?.pointer.hovering&&activity!=='dreaming'&&!interactionBusy(context)){
   const offset=(context.pointer.x-context.actor.x)/Math.max(context.actor.width,1)-.5;
   direction=Math.max(-1,Math.min(1,offset*2));
  }
  this.root.rotation.y=this.yaw.step(direction*this.turn,dt,.28);
 }
 rest(){this.drag.reset();this.yaw.reset();this.root.rotation.y=0;}
}

/** Per-model animation and optional pose layers. A replacement owns fresh state. */
export class CharacterMotion {
 private rig:DesktopPerformance|undefined;
 private turn:PointerTurn|undefined;
 private quiet=false;
 private constructor(private character:LoadedCharacter,private plane?:Group){}
 static async load(character:LoadedCharacter){
  let plane:Group|undefined;
  if(character.presentation.rig){try{plane=(await loadCharacterModel(characterAssets.paperPlane)).scene;}catch{/* Optional prop. */}}
  return new CharacterMotion(character,plane);
 }
 attach(scene:Scene,camera:OrthographicCamera){
  const {root,presentation}=this.character;
  if(presentation.rig){this.rig=new DesktopPerformance(root,scene,camera,this.plane);this.plane=undefined;}
  if(presentation.turn)this.turn=new PointerTurn(root,presentation.turn);
 }
 get animates(){return this.character.player.animates||!!this.rig||!this.quiet&&!!this.turn;}
 setActivity(activity:Activity,animate:boolean){
  this.rig?.restore();this.character.player.setActivity(activity,animate);this.rig?.capture();
  if(activity!=='idle')this.rig?.interrupt();
 }
 react(feedback:Feedback){
  this.rig?.restore();
  if(feedback==='touch')this.rig?.touch();
  const played=this.character.player.react(feedback);
  this.rig?.capture();this.rig?.interrupt();
  return played;
 }
 contextChanged(context?:DesktopContext){
  if(interactionBusy(context)||context?.pointer.hovering)this.rig?.interrupt();
  if(context?.interaction.dragging&&this.character.player.oneShot){
   this.rig?.restore();this.character.player.rest();this.rig?.capture();
  }
 }
 preview(kind:PreviewKind,name:string,activity:Activity){this.rig?.preview(kind,name,activity);}
 update(dt:number,now:number,activity:Activity,context?:DesktopContext){
  const {player}=this.character;
  if(!this.quiet)this.rig?.restore();
  player.update(dt);
  this.rig?.update(dt,now,activity,context,player,this.quiet);
  if(!this.quiet)this.turn?.update(dt,activity,context);
 }
 rest(quiet:boolean){
  this.quiet=quiet;
  this.rig?.resetPose();this.turn?.rest();
  this.character.player.rest(quiet);
  this.rig?.settle();
 }
 view(){this.rig?.view();}
 dispose(){
  this.rig?.dispose();
  if(this.plane)releaseModel(this.plane);
 }
}
