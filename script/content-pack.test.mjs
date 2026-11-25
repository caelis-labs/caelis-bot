import {after,test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createServer} from 'vite';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import {AnimationClip,NumberKeyframeTrack,Object3D,OrthographicCamera,Scene} from 'three';
import validator from 'gltf-validator';

const server=await createServer({server:{middlewareMode:true,ws:false},appType:'custom'});
after(()=>server.close());
const {CharacterAnimation}=await server.ssrLoadModule('/src/character/animation.ts');
const {characterSource,defaultCharacterKey,fallbackCharacter}=await server.ssrLoadModule('/src/character/assets.ts');
const {prepareCharacter}=await server.ssrLoadModule('/src/character/load.ts');
const {CharacterMotion}=await server.ssrLoadModule('/src/character/character-motion.ts');
const {installTransport}=await server.ssrLoadModule('/src/transport.ts');

async function parse(file){
 const bytes=readFileSync(file);
 return new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength),'');
}
const fromShipped=async source=>prepareCharacter(await parse('frontend/public'+source.url),source);
const track=(name,duration=1)=>new AnimationClip(name,duration,[new NumberKeyframeTrack('.position[y]',[0,duration],[0,1])]);
const step=(player,seconds)=>{
 for(let i=0;i<Math.ceil(seconds*30);i++)player.update(1/30);
};

test('static v1 community geometry and the recovery model need no animation or approved rig metadata',async()=>{
 for(const [file,source]of [
  ['internal/contentpack/testdata/basic.glb',characterSource('example.pack@1.0.0/character/old/static','/content/static.glb')],
  ['frontend/public'+fallbackCharacter.url,fallbackCharacter],
 ]){
  const bytes=readFileSync(file),report=await validator.validateBytes(new Uint8Array(bytes));
  assert.equal(report.issues.numErrors,0,JSON.stringify(report.issues));
  const model=await parse(file);
  assert.equal(model.animations.length,0);
  model.scene.userData.desktopPetLocomotion='drag-run-v1';
  const character=prepareCharacter(model,source);
  const {player}=character;
  assert.equal(character.presentation.rig,undefined);
  assert.equal(player.animates,false);
  player.setActivity('working');
  assert.equal(player.react('celebrate'),false);
  step(player,.4);
  assert.ok(Number.isFinite(player.progress));
  assert.equal(player.oneShot,false);
  player.rest(true);
  assert.equal(player.animates,false);
  character.dispose();
 }
});

test('idle candidates rotate only at loop boundaries, exclude the last choice, and stay quiet on request',()=>{
 const clips=['idle','look-around','nod'].map(name=>track(name));
 const random=[.9,.2,.4];
 const player=new CharacterAnimation(new Object3D(),clips,{idle:clips.map(c=>c.name)},{random:()=>random.shift()});
 assert.equal(player.clip,'idle');
 step(player,.8);
 assert.equal(player.clip,'idle','a partial cycle must keep its candidate');
 step(player,.25);
 assert.equal(player.clip,'nod','a name from the legacy gesture contract may be mapped as an idle');
 assert.equal(player.oneShot,false);
 step(player,1);assert.equal(player.clip,'idle');
 step(player,1.1);assert.equal(player.clip,'look-around');
 player.rest(true);
 const progress=player.progress;
 step(player,3);
 assert.equal(player.clip,'idle');assert.equal(player.progress,progress);assert.equal(player.animates,false);
 player.dispose();
});

test('feedback uses its explicit mode and finishes back at the latest activity',()=>{
 const clips=[track('stand'),track('ready'),track('read'),track('idle',.7)];
 const player=new CharacterAnimation(new Object3D(),clips,{
  idle:['stand'],activities:{working:'read',waiting:'ready'},feedback:{touch:'idle',waiting:'idle'},
 });
 assert.equal(player.react('touch'),true);assert.equal(player.clip,'idle');assert.equal(player.oneShot,true);
 step(player,.2);
 player.setActivity('working');player.setActivity('waiting');
 assert.equal(player.feedback,'touch','activity facts must leave an existing one-shot intact');
 step(player,.7);
 assert.equal(player.clip,'ready');assert.equal(player.oneShot,false);
 player.setActivity('working');
 assert.equal(player.clip,'read');
 player.setActivity('waiting');assert.equal(player.feedback,'waiting');
 player.setActivity('working');step(player,1);
 assert.equal(player.clip,'read');assert.equal(player.oneShot,false);
 player.react('touch');player.setActivity('dreaming');
 assert.equal(player.oneShot,false);assert.equal(player.clip,'stand');
 player.dispose();
});

test('both shipped GLBs follow the production mapping; Whale Girl turns and resets on real drag samples',async()=>{
 const defaultSource=characterSource(defaultCharacterKey,'');
 const caelis=await fromShipped(defaultSource);
 try{
  assert.equal(caelis.presentation.rig,'desktop-pet-v1');
  assert.equal(caelis.player.react('touch'),false,'the desktop rig supplies its own touch');
  caelis.player.setActivity('working');
  assert.equal(caelis.player.clip,'working');
  caelis.player.setActivity('waiting');
  assert.equal(caelis.player.clip,'attention');
  step(caelis.player,3);
  assert.equal(caelis.player.clip,'idle');
 }finally{caelis.dispose();}
 const source=characterSource('builtin:whale-girl/maid','');
 const gltf=await parse('frontend/public'+source.url);
 const whale=prepareCharacter(gltf,source);
 const motion=await CharacterMotion.load(whale);
 motion.attach(new Scene(),new OrthographicCamera());
 try{
  assert.equal(motion.react('touch'),true);assert.equal(whale.player.clip,'nod');
  motion.setActivity('working',true);
  assert.equal(whale.player.clip,'nod','working updates the base while the touch finishes');
  for(let i=0;i<90;i++)motion.update(1/30,i*33,'working');
  assert.equal(whale.player.clip,'working');
  assert.equal(whale.player.oneShot,false);
  motion.react('celebrate');assert.equal(whale.player.clip,'celebrate');
  for(let i=0;i<30;i++){
   const context={sampledAtUptime:i/30,actor:{x:0,width:180},pointer:{x:120+i*18,y:100},interaction:{dragging:true}};
   motion.contextChanged(context);
   motion.update(1/30,i*33,'working',context);
  }
  assert.equal(whale.player.oneShot,false,'native drag preempts feedback');
  assert.ok(whale.root.rotation.y>.2&&whale.root.rotation.y<=.4);
  motion.rest(true);
  assert.equal(whale.root.rotation.y,0);assert.equal(whale.player.clip,'idle');assert.equal(motion.animates,false);
  motion.rest(false);
  assert.equal(whale.player.clip,'working');
 }finally{motion.dispose();whale.dispose();}
 assert.equal(whale.player.mixer.existingAction(gltf.animations.find(clip=>clip.name==='working')),null);
});

test('disposing the desktop rig removes its prop and cancels a late native launch once',async t=>{
 const window=new EventTarget();
 const originalFetch=globalThis.fetch, originalProgress=globalThis.ProgressEvent;
 const plane=readFileSync('frontend/public/models/xiaoai-paper-plane.glb');
 globalThis.window=window;
 globalThis.fetch=async request=>{
  assert.match(request.url,/\/models\/xiaoai-paper-plane\.glb$/);
  return new Response(plane);
 };
 globalThis.ProgressEvent??=class {constructor(type,init){Object.assign(this,{type},init);}};
 t.after(()=>{
  delete globalThis.window;globalThis.fetch=originalFetch;
  if(originalProgress)globalThis.ProgressEvent=originalProgress;else delete globalThis.ProgressEvent;
  installTransport(undefined);
 });
 let complete;
 const calls=[];
 installTransport(async(_service,method,args)=>{
  calls.push([method,...args]);
  if(method==='LaunchPlane')return new Promise(resolve=>{complete=resolve;});
 });
 const caelis=await fromShipped(characterSource(defaultCharacterKey,''));
 // Node's Request requires absolute URLs; only the plane fetch is substituted.
 const Request=globalThis.Request;
 globalThis.Request=class extends Request{
  constructor(input,options){super(typeof input==='string'&&input.startsWith('/')?'http://fixture.test'+input:input,options);}
 };
 t.after(()=>{globalThis.Request=Request;});
 const motion=await CharacterMotion.load(caelis);
 const scene=new Scene(),camera=new OrthographicCamera(-1.05,1.05,2.29,-.51,.1,20);
 camera.position.z=5;camera.updateMatrixWorld();
 motion.attach(scene,camera);
 assert.equal(scene.children.length,1);
 motion.preview('idle','plane_play','idle');
 for(let i=0;i<70;i++)motion.update(1/30,i*33,'idle');
 const [launch]=calls;
 assert.equal(launch[0],'LaunchPlane');assert.ok(launch[2]>1&&launch[2]<180&&launch[3]>1&&launch[3]<240);
 motion.dispose();caelis.dispose();
 assert.equal(scene.children.length,0);
 assert.equal(calls.length,1);
 complete(true);
 await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(calls,[launch,['FinishPlane',launch[1],false]]);
});
