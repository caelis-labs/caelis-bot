// Compare the ink and skin actually exported in the front standing frame.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {AnimationMixer,Vector3} from 'three';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';

async function standingPortrait(path) {
  const bytes=readFileSync(path);
  const {scene,animations}=await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
  const mixer=new AnimationMixer(scene);
  mixer.clipAction(animations.find(clip=>clip.name==='idle')).play();
  mixer.setTime(0);
  scene.updateMatrixWorld(true);
  const groups=new Map();
  const painted=new Map();
  const names=['Warm ivory sclera','Porcelain sclera','Graduated sapphire','Indigo lash line',
    'Plum outer lash R','Plum outer lash L','Violet pupil R','Violet pupil L',
    'Warm lip tint','Smile interior','Peach blush gradient'];
  scene.traverse(mesh=>{
    const name=mesh.material?.name;
    if (!names.includes(name)) return;
    // Check the lip rim and tongue at an open smile; idle now starts with the lips together.
    const jaw = mesh.morphTargetDictionary?.MouthClose;
    if (jaw !== undefined) mesh.morphTargetInfluences[jaw] = 0;
    const geometry=mesh.geometry;
    if(['Indigo lash line','Smile interior'].includes(name)){
      const c=geometry.attributes.color;
      if(c && mesh.material.vertexColors){
        let min=Infinity,max=-Infinity;
        for(let i=0;i<c.count;i++){min=Math.min(min,c.getY(i));max=Math.max(max,c.getY(i));}
        painted.set(name,{min,max});
      }
    }
    const points=Array.from({length:geometry.attributes.position.count},(_,i)=>
      mesh.getVertexPosition(i,new Vector3()).applyMatrix4(mesh.matrixWorld));
    const triangles=groups.get(name)??[];
    groups.set(name,triangles);
    const indices=geometry.index.array;
    for (let i=0;i<indices.length;i+=3) {
      triangles.push([points[indices[i]],points[indices[i+1]],points[indices[i+2]]]);
    }
  });
  const eyes=[[-1,'R'],[1,'L']].map(([side,label])=>{
    const pieces=name=>groups.get(name).filter(triangle=>triangle[0].x*side>0);
    const eye=pieces(groups.has('Warm ivory sclera')?'Warm ivory sclera':'Porcelain sclera'), iris=pieces('Graduated sapphire');
    const lash=pieces('Indigo lash line'), outer=pieces('Plum outer lash '+label);
    const pupil=pieces('Violet pupil '+label);
    return measure(eye,iris,lash,outer,pupil);
  });
  return {eyes,groups,painted};
}

// At each X, union vertical intervals. The upper lash covers some of the
// sclera and iris. Corneal reflections count as iris, not exposed sclera.
function section(triangles,x) {
  const runs=[];
  for (const points of triangles) {
    const ends=[];
    for (let i=0;i<3;i++) {
      const a=points[i], b=points[(i+1)%3];
      if ((a.x<=x&&x<b.x)||(b.x<=x&&x<a.x)) ends.push(a.y+(b.y-a.y)*(x-a.x)/(b.x-a.x));
    }
    if (ends.length===2) runs.push([Math.min(...ends),Math.max(...ends)]);
  }
  runs.sort((a,b)=>a[0]-b[0]);
  const merged=[];
  for (const [lo,hi] of runs) {
    const previous=merged.at(-1);
    if (previous && lo<=previous[1]+1e-8) previous[1]=Math.max(previous[1],hi);
    else merged.push([lo,hi]);
  }
  return merged;
}

function bounds(triangles) {
  const points=triangles.flat();
  const xs=points.map(p=>p.x), ys=points.map(p=>p.y);
  return {left:Math.min(...xs),right:Math.max(...xs),low:Math.min(...ys),high:Math.max(...ys)};
}

function frontDepth(triangles,x,y) {
  let depth=-Infinity;
  for (const [a,b,c] of triangles) {
    const det=(b.y-c.y)*(a.x-c.x)+(c.x-b.x)*(a.y-c.y);
    if (Math.abs(det)<1e-12) continue;
    const u=((b.y-c.y)*(x-c.x)+(c.x-b.x)*(y-c.y))/det;
    const v=((c.y-a.y)*(x-c.x)+(a.x-c.x)*(y-c.y))/det;
    if (u>=0 && v>=0 && u+v<=1) depth=Math.max(depth,u*a.z+v*b.z+(1-u-v)*c.z);
  }
  return depth;
}

// Require visible ground on both sides at pupil height. A white mesh hidden
// behind the cheek or a lash still exists in the file but contributes no white.
function checkSclera(groups) {
  const white=groups.get('Warm ivory sclera');
  const skin=groups.get('Peach blush gradient');
  for(const [sign,side] of [[1,'L'],[-1,'R']]){
    const half=triangles=>triangles.filter(triangle=>triangle[0].x*sign>0);
    const opening=half(white);
    const b=bounds(opening),width=b.right-b.left;
    const pupil=bounds(groups.get('Violet pupil '+side));
    const y=(pupil.high+pupil.low)/2;
    const nearby=triangles=>triangles.filter(tri=>tri.some(p=>p.y<=y)&&tri.some(p=>p.y>=y));
    const ground=nearby(opening);
    const other=nearby([...skin,...half(groups.get('Graduated sapphire')),
      ...half(groups.get('Indigo lash line')),...groups.get('Plum outer lash '+side)]);
    let before=0,after=0;
    for(let i=0;i<100;i++){
      const x=b.left+width*(i+.5)/100;
      if(frontDepth(ground,x,y)>frontDepth(other,x,y)){
        if(x<pupil.left)before++;
        if(x>pupil.right)after++;
      }
    }
    assert.ok(before>=8&&after>=8,'iris or skin covers lateral sclera in '+side);
  }
}

function checkLip(groups) {
  const lip=groups.get('Warm lip tint'), inside=groups.get('Smile interior');
  const {left,right,low,high}=bounds(lip);
  const cheek=groups.get('Peach blush gradient').filter(points=>
    !points.every(p=>p.x<left) && !points.every(p=>p.x>right) &&
    !points.every(p=>p.y<low) && !points.every(p=>p.y>high));
  // Sample the stroke itself, including the lower arc where the cheek
  // turns under the chin. A valid lip mesh can still be buried in skin.
  const opening=bounds(inside);
  for (let i=1;i<40;i++) {
    const x=opening.left+(opening.right-opening.left)*i/40;
    const [[bottom,top]]=section(lip,x), [[innerBottom,innerTop]]=section(inside,x);
    assert.ok(bottom<innerBottom && top>innerTop,'opening covers lip outline');
    for (const y of [(bottom+innerBottom)/2,(top+innerTop)/2]) {
      assert.ok(frontDepth(lip,x,y)>frontDepth(cheek,x,y),'lip outline buried in cheek');
    }
  }
}

function measure(eye,iris,lash,wing,pupil) {
  const {left,right,low,high}=bounds(eye);
  const width=right-left, height=high-low;
  const irisBounds=bounds(iris), pupilBounds=bounds(pupil);
  const occluders=[...iris,...lash,...wing];
  const samples=100;
  let aperture=0,white=0,centralThickness=0,centralSamples=0;
  for (let i=0;i<samples;i++) {
    const t=(i+.5)/samples, x=left+width*t;
    const ink=section(occluders,x);
    for (const [lo,hi] of section(eye,x)) {
      aperture+=hi-lo;
      white+=hi-lo;
      for (const [a,b] of ink) white-=Math.max(0,Math.min(b,hi)-Math.max(a,lo));
    }
    if (t>.28 && t<.72) {
      for (const [lo,hi] of section(lash,x)) centralThickness+=hi-lo;
      centralSamples++;
    }
  }
  return {width,height,low,high,pupilY:(pupilBounds.high+pupilBounds.low)/2,
    pupilX:(pupilBounds.left+pupilBounds.right)/2,irisWidth:(irisBounds.right-irisBounds.left)/width,
    white:white/aperture,roundness:aperture/(samples*height),lash:centralThickness/centralSamples};
}

// Measurements from the shipped 1.16 standing frame, before the wider aperture.
const baseline={width:.1181761,height:.1057175,pupilX:-.1076035,lash:.01937154};
const [currentPath]=process.argv.slice(2);
const {eyes,groups,painted}=await standingPortrait(currentPath);
for(const name of ['Indigo lash line','Smile interior']){
  const color=painted.get(name);
  assert.ok(color && color.max-color.min>.01 && color.min<.60,'lost exported face paint on '+name);
}
checkLip(groups);
checkSclera(groups);
for (const key of ['width','height','low','high','pupilY','roundness','irisWidth','white','lash']) {
  assert.ok(Math.abs(eyes[0][key]-eyes[1][key])<1e-5,'resting eyes differ in '+key);
}
assert.ok(Math.abs(eyes[0].pupilX+eyes[1].pupilX)<1e-5,'pupils differ in distance from midline');
for (const eye of eyes) {
  const scale=key=>(eye[key]/Math.abs(eye.pupilX))/(baseline[key]/Math.abs(baseline.pupilX));
  assert.ok(scale('width')>1.05 && scale('width')<1.15,'standing eye width at equal pupil spacing');
  assert.ok(scale('height')>1.00 && scale('height')<1.10,'standing eye height at equal pupil spacing');
  assert.ok(eye.white>.22 && eye.white<.40,'sclera must remain visible on either side of the iris');
  assert.ok(eye.irisWidth>.59 && eye.irisWidth<.73,'iris does not match the illustrated opening');
  assert.ok(eye.lash>baseline.lash*.61,'upper lash lost its weight');
}
console.log('Whale Girl standing eyes',JSON.stringify(eyes));
