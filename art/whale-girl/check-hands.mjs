// Check the two continuous arm-to-fingertip surfaces after glTF export.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {AnimationMixer, FrontSide, Quaternion, Vector3} from 'three';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';

const bytes=readFileSync(process.argv[2]);
const {scene,animations}=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
const continuous=process.argv.includes('--continuous');
const compact=process.argv.includes('--compact');
const mesh=scene.getObjectByName(continuous?'body':'hands');
assert.ok(mesh?.isSkinnedMesh,'missing skinned hand surface');
const {position,skinIndex,skinWeight}=mesh.geometry.attributes;
const edgeCounts=new Map();
const neighbors=new Map();
const coords=new Map();
const joints=mesh.skeleton.bones;
const ids=[];
const wrists={L:0,R:0};
const shoulders={L:0,R:0}, hips={L:0,R:0};
const weighted=new Set();
for(let i=0;i<position.count;i++){
  // UV and shading seams may split one spatial vertex in the GLB.
  const coord=[position.getX(i),position.getY(i),position.getZ(i)].join(',');
  if(!coords.has(coord))coords.set(coord,coords.size);
  ids.push(coords.get(coord));
  let total=0;
  const blend=new Set();
  for(let j=0;j<4;j++){
    const w=skinWeight.getComponent(i,j);
    total+=w;
    if(w>.04){
      const name=joints[skinIndex.getComponent(i,j)].name;
      blend.add(name);
      weighted.add(name);
    }
  }
  assert.ok(Math.abs(total-1)<.0002,'unnormalized arm skin weights');
  for(const side of ['L','R']){
    if(blend.has('chest')&&blend.has('upper_arm'+side))shoulders[side]++;
    if(blend.has('pelvis')&&blend.has('leg'+side))hips[side]++;
    if((blend.has('forearm'+side)||blend.has('forearm_twist'+side))&&blend.has('hand'+side))wrists[side]++;
  }
}
const indices=mesh.geometry.index.array;
for(let i=0;i<indices.length;i+=3){
  const tri=[ids[indices[i]],ids[indices[i+1]],ids[indices[i+2]]];
  for(let j=0;j<3;j++){
    const a=tri[j],b=tri[(j+1)%3];
    assert.notEqual(a,b,'collapsed hand triangle');
    const key=a<b?[a,b].join(','):[b,a].join(',');
    edgeCounts.set(key,(edgeCounts.get(key)??0)+1);
    if(!neighbors.has(a))neighbors.set(a,new Set());
    neighbors.get(a).add(b);
  }
}
for(const count of edgeCounts.values())assert.equal(count,2,'open or non-manifold hand edge');
let components=0;
const seen=new Set();
for(const id of neighbors.keys()){
  if(seen.has(id))continue;
  components++;
  const queue=[id];
  while(queue.length){
    const at=queue.pop();
    if(seen.has(at))continue;
    seen.add(at);
    for(const next of neighbors.get(at))queue.push(next);
  }
}
assert.equal(components,continuous?1:2,'disconnected skin in exported figure');
const mixer=new AnimationMixer(scene);
mixer.clipAction(animations.find(a=>a.name==='idle')).play();
mixer.setTime(0);
scene.updateMatrixWorld(true);
const point=name=>scene.getObjectByName(name).getWorldPosition(new Vector3());
for(const [side,sign] of [['L',1],['R',-1]]){
  if(continuous){
    assert.ok(shoulders[side]>8 && hips[side]>8,'missing shoulder or hip transition '+side);
    for(const digit of ['thumb','index','middle','ring','pinky'])
      assert.ok(weighted.has(digit+'02'+side),'unbound finger '+digit+side);
  }
  assert.ok(wrists[side]>8,'no blended wrist '+side);
  const elbow=point('forearm'+side),wrist=point('hand'+side),palm=point('middle01'+side);
  const shoulder=point('upper_arm'+side);
  assert.ok(shoulder.y-elbow.y>(compact ? .115 : .15) && elbow.y-wrist.y>(compact ? .10 : .14),
    'arm must hang beside the torso');
  const upper=elbow.clone().sub(shoulder), fore=wrist.clone().sub(elbow);
  if(compact){
    assert.ok(upper.length()>.16 && upper.length()<.18 && fore.length()>.145 && fore.length()<.16,
      'arm segments must match the shorter dress fitting');
    assert.ok(point('thumb02'+side).z>point('pinky02'+side).z+.025,
      'relaxed thumb must lead the little finger toward the front');
  }
  const flexion=upper.angleTo(fore)*180/Math.PI;
  assert.ok(flexion>4 && flexion<20,'resting elbow should stay softly extended');
  assert.ok(sign*(wrist.x-shoulder.x)>.10 && sign*(wrist.x-shoulder.x)<.21,
    'resting arm must clear the skirt without spreading sideways');
  const across=point('pinky01'+side).sub(point('index01'+side));
  const normal=across.cross(palm.clone().sub(wrist)).normalize().multiplyScalar(sign);
  assert.ok(sign*normal.x<-.65 && normal.y<.25,'relaxed palm must face the thigh');
  const bend=wrist.clone().sub(elbow).angleTo(palm.sub(wrist))*180/Math.PI;
  assert.ok(bend<13,'excessive resting wrist bend '+side);
}
// Check the release of the wave as well as its two endpoint poses. An
// antipodal pair of quaternion keys can send the wrist around the long arc.
mixer.stopAllAction();
const wave=animations.find(a=>a.name==='celebrate');
mixer.clipAction(wave).play();
if(compact){
  mixer.setTime(.60);
  scene.updateMatrixWorld(true);
  const span=point('pinky01R').sub(point('index01R'));
  const fingers=point('middle01R').sub(point('handR'));
  assert.ok(span.cross(fingers).normalize().negate().z>.72,'greeting must show the palm');
}
const previous=new Map();
for(let frame=0;frame<wave.duration*60;frame++){
  mixer.setTime(frame/60);
  scene.updateMatrixWorld(true);
  for(const side of ['L','R'])for(const joint of ['upper_arm','forearm','hand']){
    const name=joint+side;
    const q=scene.getObjectByName(name).getWorldQuaternion(new Quaternion());
    if(previous.has(name))assert.ok(q.angleTo(previous.get(name))<Math.PI/5,'abrupt wave rotation '+name);
    previous.set(name,q);
  }
}
const palette=new Map();
scene.traverse(o=>{if(o.isMesh)palette.set(o.material.name,o);});
const skirt=palette.get('Indigo skirt cloth');
assert.ok(skirt?.material.isMeshStandardMaterial && skirt.material.color.b>2*skirt.material.color.r,
  'the skirt must retain its indigo dye in the exported pack');
for(const key of ['Marine contour','Warm skin contour']){
  assert.equal(palette.get(key)?.material.side,FrontSide,'outline must cull its back faces');
}
for(const key of ['Cel / Whale hair / painted indigo locks','Cel / Indigo whale with blue belly']){
  const part=palette.get(key);
  assert.ok(part?.material.isMeshBasicMaterial&&part.material.vertexColors,'missing portable cel paint '+key);
  const c=part.geometry.attributes.color;
  let min=1,max=0;
  for(let i=0;i<c.count;i++){min=Math.min(min,c.getZ(i));max=Math.max(max,c.getZ(i));}
  assert.ok(max-min>.02,'flattened cel paint '+key);
}
console.log('Whale Girl: closed skin, blended wrists, relaxed idle pose and exported cel paints passed');
