import { Euler, Quaternion, Vector3, type Object3D } from 'three';

/** Shift the body while retaining the two planted soles. The inherited rig has
 * feet parented to thighs, so their world transforms are restored explicitly.
 * Only used for small standing performances; locomotion owns its own gait.
 */
export function groundedPose(root:Object3D,shift:number,drop:number,roll:number,pitch:number){
 const get=(name:string)=>root.getObjectByName(name.replaceAll('.',''))??root.getObjectByName(name);
 const body=get('root');if(!body)return;
 root.updateMatrixWorld(true);
 const legs=['L','R'].map(side=>{
  const thigh=get('leg.'+side),knee=get('knee.'+side),foot=get('foot.'+side);
  if(!thigh||!knee||!foot)return undefined;
  const hip=thigh.getWorldPosition(new Vector3()),joint=knee.getWorldPosition(new Vector3()),ankle=foot.getWorldPosition(new Vector3());
  return {thigh,knee,foot,ankle,rotation:foot.getWorldQuaternion(new Quaternion()),a:hip.distanceTo(joint),b:joint.distanceTo(ankle)};
 });
 if(legs.some(leg=>!leg))return;
 body.position.x+=shift;body.position.y-=drop;
 body.quaternion.multiply(new Quaternion().setFromEuler(new Euler(pitch,0,roll)));
 root.updateMatrixWorld(true);
 const aim=(bone:Object3D,from:Vector3,to:Vector3)=>{
  const world=bone.getWorldQuaternion(new Quaternion()).premultiply(new Quaternion().setFromUnitVectors(from.normalize(),to.normalize()));
  bone.quaternion.copy(bone.parent!.getWorldQuaternion(new Quaternion()).invert().multiply(world));root.updateMatrixWorld(true);
 };
 for(const leg of legs){
  if(!leg)continue;
  const {thigh,knee,foot,ankle,rotation,a,b}=leg;
  const hip=thigh.getWorldPosition(new Vector3()),axis=ankle.clone().sub(hip);
  const d=Math.min(a+b-.00001,Math.max(Math.abs(a-b)+.00001,axis.length()));axis.normalize();
  const along=(a*a-b*b+d*d)/(2*d),height=Math.sqrt(Math.max(0,a*a-along*along));
  const forward=new Vector3(0,0,1).applyQuaternion(root.getWorldQuaternion(new Quaternion()));
  forward.addScaledVector(axis,-forward.dot(axis)).normalize();
  const joint=hip.clone().addScaledVector(axis,along).addScaledVector(forward,height);
  aim(thigh,knee.getWorldPosition(new Vector3()).sub(hip),joint.sub(hip));
  aim(knee,new Vector3(0,1,0).applyQuaternion(knee.getWorldQuaternion(new Quaternion())),ankle.clone().sub(knee.getWorldPosition(new Vector3())));
  foot.position.copy(foot.parent!.worldToLocal(ankle.clone()));
  foot.quaternion.copy(foot.parent!.getWorldQuaternion(new Quaternion()).invert().multiply(rotation));
  root.updateMatrixWorld(true);
 }
}
