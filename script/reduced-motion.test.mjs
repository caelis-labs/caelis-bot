import {test,after} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'vite';
import {AnimationClip,NumberKeyframeTrack,Object3D} from 'three';
import {animateAvatar,quietAvatarScale} from '../frontend/src/avatar-motion.ts';
import {portraitClock} from '../frontend/src/portrait-player.ts';
const server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});
after(()=>server.close());
const {CharacterAnimation}=await server.ssrLoadModule('/src/character/animation.ts');

test('reduced pet keeps bounded slow idle and rejects full gestures, then restores activity live',()=>{
 const root=new Object3D();
 const clips=['idle','working','attention','nod','celebrate'].map(name=>new AnimationClip(name,2,[new NumberKeyframeTrack('.position[x]',[0,1,2],[2,3,2])]));
 const player=new CharacterAnimation(root,clips);
 player.setActivity('working');player.setReducedMotion(true);
 player.gesture('celebrate');assert.equal(player.clip,'idle');
 assert.equal(root.position.x,2,'reduced idle must preserve the authored neutral pose, not the bind pose');
 player.update(1);assert.ok(root.position.x>2&&root.position.x<=2.18,'idle must move with reduced amplitude');
 const first=root.position.x;player.update(.5);assert.notEqual(root.position.x,first);
 player.setActivity('waiting');assert.equal(player.clip,'idle');
 player.setActivity('working');player.setReducedMotion(false);assert.equal(player.clip,'working');
 player.gesture('celebrate');assert.equal(player.clip,'celebrate');player.dispose();
});

function clock(){
 let id=0;const pending=new Map();
 return {request:fn=>{pending.set(++id,fn);return id;},cancel:id=>pending.delete(id),
  step(time){const callbacks=[...pending.values()];pending.clear();callbacks.forEach(fn=>fn(time));},get size(){return pending.size;}};
}
test('quiet avatar keeps feedback, obeys pause/disposal and changes policy without restarting',()=>{
 const frames=[],scheduler=clock(),avatar=animateAvatar(p=>frames.push(p),scheduler,()=>.5);
 avatar.setReducedMotion(true);avatar.setRunning(true);
 for(let i=0;i<80;i++)scheduler.step(i*40);
 assert.ok(new Set(frames.map(p=>p.scaleY)).size>20);
 assert.ok(frames.every(p=>p.tilt===0&&p.offsetX===0&&p.offsetY===0&&p.scaleY>=1&&p.scaleY<=1.02));
 avatar.setRunning(false);assert.equal(scheduler.size,0);
 const stopped=frames.length;scheduler.step(99999);assert.equal(frames.length,stopped);
 avatar.setReducedMotion(false);avatar.setRunning(true);
 for(let i=0;i<80;i++)scheduler.step(100000+i*40);
 assert.ok(frames.some(p=>p.gesture!=='rest'));
 avatar.dispose();assert.equal(scheduler.size,0);
});
test('portrait quiet breath uses visible time, not elapsed hidden time',()=>{
 const scheduler=clock(),scales=[],player=portraitClock(t=>scales.push(quietAvatarScale(t)),scheduler);
 player.setRunning(true);for(let i=0;i<30;i++)scheduler.step(i*40);
 assert.ok(scales.at(-1)>scales[0]);player.setRunning(false);assert.equal(scheduler.size,0);
 const before=scales.at(-1);player.setRunning(true);scheduler.step(99999);assert.equal(scales.at(-1),before);
 player.dispose();assert.equal(scheduler.size,0);
});
