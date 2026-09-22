import { Camera, Mesh, Object3D, Quaternion, Vector3, type BufferAttribute, type InterleavedBufferAttribute } from 'three';

type ViewProfile = {
 version: 1; quarterAngle: number; sideAngle: number;
 backFadeStart: number; backFadeEnd: number; headBone: string;
 targets: string[]; expressionTargets: string[]; crossSeparator: string;
};
const names = ['view3QLeft','view3QRight','viewSideLeft','viewSideRight'] as const;
const smooth = (t:number) => {t=Math.min(1,Math.max(0,t));return t*t*(3-2*t);};

/** Continuous front -> quarter -> side, fading out before the back view. */
export function viewWeights(yaw:number, profile:Pick<ViewProfile,'quarterAngle'|'sideAngle'|'backFadeStart'|'backFadeEnd'>) {
 const angle=Math.abs(Math.atan2(Math.sin(yaw),Math.cos(yaw)))*180/Math.PI;
 const weights={view3QLeft:0,view3QRight:0,viewSideLeft:0,viewSideRight:0};
 const side=yaw>=0?'Left':'Right';
 const fade=1-smooth((angle-profile.backFadeStart)/(profile.backFadeEnd-profile.backFadeStart));
 if(angle<=profile.quarterAngle) weights[`view3Q${side}`]=smooth(angle/profile.quarterAngle)*fade;
 else {const t=smooth((angle-profile.quarterAngle)/(profile.sideAngle-profile.quarterAngle));weights[`view3Q${side}`]=(1-t)*fade;weights[`viewSide${side}`]=t*fade;}
 return weights;
}

/** glTF has equal target counts per primitive. Drop only provably zero relative
 * targets before GPU allocation; the name dictionary preserves authoring semantics.
 * This asset has no weight animation tracks (expressions are driven by name).
 */
export function pruneEmptyViewTargets(root:Object3D) {
 if(root.userData.desktopPetViewMorphs?.version!==1)return;
 root.traverse(o=>{
  if(!(o instanceof Mesh)||!o.geometry.morphTargetsRelative||!o.morphTargetInfluences||!o.morphTargetDictionary)return;
  const geometry=o.geometry, attributes=Object.values(geometry.morphAttributes) as (Array<BufferAttribute|InterleavedBufferAttribute>|undefined)[], dictionary=o.morphTargetDictionary;
  const keep=o.morphTargetInfluences.map((_,i)=>i).filter(i=>attributes.some(list=>{
   const attr=list?.[i];if(!attr)return false;
   for(let v=0;v<attr.count;v++) if(attr.getX(v)!==0||attr.itemSize>1&&attr.getY(v)!==0||attr.itemSize>2&&attr.getZ(v)!==0||attr.itemSize>3&&attr.getW(v)!==0)return true;
   return false;
  }));
  if(keep.length===o.morphTargetInfluences.length)return;
  for(const key of Object.keys(geometry.morphAttributes) as (keyof typeof geometry.morphAttributes)[]) {
   const attrs=geometry.morphAttributes[key];if(!keep.length)delete geometry.morphAttributes[key];else if(attrs)geometry.morphAttributes[key]=keep.map(i=>attrs[i]);
  }
  o.morphTargetInfluences=keep.map(i=>o.morphTargetInfluences![i]);
  const remap=new Map(keep.map((old,i)=>[old,i]));
  o.morphTargetDictionary=Object.fromEntries(Object.entries(dictionary).filter(([,old])=>remap.has(old)).map(([name,old])=>[name,remap.get(old)!]));
 });
}

/** Presentation-only view morphs, composed AFTER facial expression weights.
 * No pose, task state, camera or native hit-test rules are changed. Render and
 * silhouette readback see exactly the same corrected skinned geometry.
 */
export class ViewCorrectives {
 private profile?:ViewProfile;private head?:Object3D;private meshes:Mesh[]=[];
 private bindInverse=new Quaternion();private rotation=new Quaternion();
 private direction=new Vector3();private headPosition=new Vector3();
 yaw=0;
 constructor(private root:Object3D) {
  const p=root.userData.desktopPetViewMorphs as ViewProfile|undefined;
  if(!p||p.version!==1||!Array.isArray(p.targets)||!names.every(n=>p.targets.includes(n))||!Array.isArray(p.expressionTargets)||!p.crossSeparator||
   !(p.quarterAngle>0&&p.sideAngle>p.quarterAngle&&p.backFadeStart>=p.sideAngle&&p.backFadeEnd>p.backFadeStart&&p.backFadeEnd<=180))return;
  this.head=root.getObjectByName(p.headBone);if(!this.head)return;
  this.profile=p;root.updateWorldMatrix(true,true);
  // Remove the bone's authored local bind orientation, keeping actual root AND
  // animated head turns. Parent/viewer transforms are not captured in this bind.
  root.getWorldQuaternion(this.rotation).invert();
  this.head.getWorldQuaternion(this.bindInverse).premultiply(this.rotation).invert();
  root.traverse(o=>{if(o instanceof Mesh&&names.some(n=>o.morphTargetDictionary?.[n]!==undefined))this.meshes.push(o);});
 }
 get available(){return !!this.profile&&this.meshes.length>0;}
 apply(camera:Camera) {
  if(!this.available)return;
  const p=this.profile!;this.root.updateWorldMatrix(true,true);camera.updateWorldMatrix(true,false);
  this.head!.getWorldQuaternion(this.rotation).multiply(this.bindInverse).invert();
  if('isOrthographicCamera' in camera)camera.getWorldDirection(this.direction).negate();
  else this.direction.copy(camera.getWorldPosition(this.direction)).sub(this.head!.getWorldPosition(this.headPosition)).normalize();
  this.direction.applyQuaternion(this.rotation);this.yaw=Math.atan2(this.direction.x,this.direction.z);
  const weights=viewWeights(this.yaw,p);
  for(const mesh of this.meshes){
   const dict=mesh.morphTargetDictionary!, values=mesh.morphTargetInfluences!;
   for(const view of names){
    const vi=dict[view];if(vi!==undefined)values[vi]=weights[view];
    for(const expression of p.expressionTargets){
     const cross=dict[view+p.crossSeparator+expression], ei=dict[expression];
     if(cross!==undefined)values[cross]=weights[view]*(ei===undefined?0:values[ei]);
    }
   }
  }
 }
}
