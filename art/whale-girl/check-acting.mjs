// Verify that the delivered face curves survived the Blender action merge.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {AnimationMixer, LoopOnce, Raycaster, Vector3} from 'three';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';

const data=readFileSync(process.argv[2]);
const model=await new GLTFLoader().parseAsync(data.buffer.slice(data.byteOffset,data.byteOffset+data.byteLength),'');
const mixer=new AnimationMixer(model.scene);
const faces=[];
model.scene.traverse(node=>{
  if(node.morphTargetDictionary?.Joy!==undefined) faces.push(node);
});
assert.ok(faces.length>=2,'missing animated eye or ink mesh');
for(const [name,key,seconds] of [
  ['idle','Blink',80/30], ['working','Focus',2], ['celebrate','Joy',20/30],
  ['attention','Alert',13/30], ['nod','Focus',12/30],
]){
  const clip=model.animations.find(clip=>clip.name===name);
  assert.ok(clip,name);
  assert.ok(clip.tracks.some(track=>track.name.endsWith('.quaternion')),name+' bone rotation');
  const action=mixer.clipAction(clip).setLoop(LoopOnce,1);
  action.clampWhenFinished=true;
  action.play();
  const weights=time=>{
    mixer.setTime(time);
    return faces.map(mesh=>mesh.morphTargetInfluences[mesh.morphTargetDictionary[key]]);
  };
  const start=weights(0);
  const peak=weights(seconds);
  const end=weights(clip.duration);
  for(let i=0;i<faces.length;i++){
    assert.ok(peak[i] > (name === 'nod' ? .35 : .97),name+' expression peak');
    assert.ok(Math.abs(start[i]-end[i])<.005,name+' endpoints');
    assert.ok(peak[i]-start[i]>.08,name+' curve was flattened');
  }
  action.stop();
}
console.log('Whale Girl: five paired skeleton and face clips passed');

if (process.argv.includes('--jaw')) {
  const materialPart = name => faces.find(mesh => mesh.material.name === name);
  const lip = materialPart('Warm lip tint');
  const tooth = materialPart('Ivory milk tooth');
  const interior = materialPart('Smile interior');
  for (const part of [lip, tooth, interior]) assert.ok(part?.morphTargetDictionary.MouthClose !== undefined);
  const bounds = mesh => {
    const p = new Vector3(), lo = new Vector3(Infinity,Infinity,Infinity), hi = lo.clone().negate();
    for (let i=0; i<mesh.geometry.attributes.position.count; i++) {
      mesh.getVertexPosition(i,p);
      lo.min(p); hi.max(p);
    }
    return {lo,hi,height:hi.y-lo.y};
  };
  for (const clip of model.animations) {
    const action = mixer.clipAction(clip).reset().setLoop(LoopOnce,1).play();
    action.clampWhenFinished = true;
    const values=[];
    for (let frame=0; frame<=60; frame++) {
      mixer.setTime(clip.duration*frame/60);
      values.push(lip.morphTargetInfluences[lip.morphTargetDictionary.MouthClose]);
    }
    assert.ok(Math.max(...values)-Math.min(...values)>.10,'mouth does not articulate in '+clip.name);
    action.stop();
  }
  mixer.clipAction(model.animations.find(a=>a.name==='idle')).reset().play();
  mixer.setTime(0);
  model.scene.updateMatrixWorld(true);
  for (const x of [-.025,0,.025]) {
    for (const y of [.999,1.004,1.010]) {
      const ray=new Raycaster(new Vector3(x,y,1),new Vector3(0,0,-1));
      assert.equal(ray.intersectObject(model.scene,true)[0]?.object.material.name,'Caelis / soft hand skin',
        'a visible band of neck must separate the collar and jaw');
    }
  }
  const closed=bounds(lip);
  assert.ok(lip.morphTargetInfluences[lip.morphTargetDictionary.MouthClose]>.99);
  for (const mesh of [lip,tooth,interior]) mesh.morphTargetInfluences[mesh.morphTargetDictionary.MouthClose]=0;
  const open=bounds(lip),fang=bounds(tooth);
  assert.ok(open.height>closed.height*5,'mouth aperture stays open in rest');
  assert.ok(fang.height<open.height*.22 && fang.hi.x<0 && fang.lo.y>open.lo.y+open.height*.60,
            'canine should be small and seated in the upper rim');
  for (const mesh of [lip,tooth,interior]) mesh.morphTargetInfluences[mesh.morphTargetDictionary.MouthClose]=.55;
  const parted=bounds(interior);
  const ray=new Raycaster(new Vector3(0,(parted.lo.y+parted.hi.y)/2,1),new Vector3(0,0,-1));
  assert.equal(ray.intersectObject(model.scene,true)[0]?.object.material.name,'Smile interior',
    'the tongue must stay visible while the lips are parted');
  console.log('Five jaw curves, closed rest, parted tongue, upper canine and visible neckline passed');
}
