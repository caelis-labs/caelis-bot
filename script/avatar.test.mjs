import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {validateAvatarSVG} from './avatar-svg.mjs';
import {readManifest,validateManifest} from './asset-pack.mjs';
import {avatarMotion,animateAvatar,neutralAvatar} from '../frontend/src/avatar-motion.ts';

test('layered art is graphics-only; reject active SVG even with a correct pack hash',()=>{
 const avatarPath=readManifest().branding.animatedAvatar;
 const art=avatarPath?readFileSync(avatarPath,'utf8'):'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><g data-avatar-part="head"><path d="M0 0"/><g data-avatar-part="eye-left"><g data-avatar-part="look-left"/></g><g data-avatar-part="eye-right"><g data-avatar-part="look-right"/></g></g></svg>';
 validateAvatarSVG(art);
 for(const bad of ['<script/>','<foreignObject/>','<image href="https://example.org/a.png"/>','<style/>','<animate/>','<use/>','<!DOCTYPE svg>','&xxe;'])
  assert.throws(()=>validateAvatarSVG(art.replace('</svg>',bad+'</svg>')),bad);
 for(const attribute of ['onload="alert(1)"','style="fill:red"','href="javascript:alert(1)"','fill="url(https://example.org/a)"','xmlns:x="https://example.org"'])
  assert.throws(()=>validateAvatarSVG(art.replace('<path ',`<path ${attribute} `)),attribute);
 assert.throws(()=>validateAvatarSVG(art.replace('data-avatar-part="head"','data-avatar-part="missing"')));
 assert.throws(()=>validateAvatarSVG(art.replace('</g>','')));
 const pack=readManifest(),old=structuredClone(pack);old.contractVersion=1;delete old.branding.animatedAvatar;old.files=old.files.filter(f=>!f.path.endsWith('.svg'));validateManifest(old);
 if(pack.contractVersion===2)assert.throws(()=>validateManifest({...pack,contractVersion:1}));
});

test('gaze and tilt stay bounded, blink recovers, long frame gaps do not catch up',()=>{
 let seed=42;const random=()=>((seed=(1664525*seed+1013904223)>>>0)/2**32);
 const advance=avatarMotion(random);let sawBlink=false,sawOpenAfter=false,sawLook=false;const gestures=new Set();
 for(let n=0;n<18000;n++) {
  const p=advance(1/30);
  assert.ok(Math.abs(p.gazeX)<=3.8&&p.gazeY>=-2.2&&p.gazeY<=1.5&&Math.abs(p.tilt)<=8);
  assert.ok(Math.abs(p.offsetX)<=2.2&&p.offsetY>=-3.5&&p.offsetY<=1.5);
  assert.ok(p.scaleX>=.96&&p.scaleX<=1.06&&p.scaleY>=.94&&p.scaleY<=1.05);gestures.add(p.gesture);
  assert.ok(p.openness>=.04&&p.openness<=1);
  if(p.openness<.2)sawBlink=true;if(sawBlink&&p.openness===1)sawOpenAfter=true;
  if(Math.abs(p.gazeX)>.8)sawLook=true;
 }
 assert.ok(sawBlink&&sawOpenAfter&&sawLook);
 const first=avatarMotion(()=>.5);let opening;
 for(let i=0;i<30;i++)opening=first(1/30);
 assert.ok(opening.offsetY<-2.5,'whole avatar should visibly rise within one second');
 assert.deepEqual([...gestures].sort(),['rest','bounce','peek','nod','listen','look-around'].sort());
 const a=avatarMotion(()=>.8),b=avatarMotion(()=>.8);
 for(let i=0;i<60;i++){a(1/30);b(1/30);}
 assert.deepEqual(a(1000),b(.05));
 const before=a(0);assert.deepEqual(a(NaN),before);
});

test('hidden, offscreen and reduced-motion callers can stop all frame work and resume neutrally',()=>{
 let id=0;const pending=new Map(),painted=[];
 const clock={request(fn){pending.set(++id,fn);return id;},cancel(id){pending.delete(id);}};
 const next=time=>{const callbacks=[...pending.values()];pending.clear();callbacks.forEach(fn=>fn(time));};
 const animator=animateAvatar(p=>painted.push(p),clock,()=>.8);
 assert.equal(pending.size,0);animator.setRunning(true);animator.setRunning(true);assert.equal(pending.size,1);
 for(let n=0;n<100;n++)next(n*40);
 assert.ok(painted.some(p=>p.gazeX!==0));
 animator.setRunning(false);assert.equal(pending.size,0);assert.deepEqual(painted.at(-1),neutralAvatar);
 const count=painted.length;next(50000);assert.equal(painted.length,count);
 animator.setRunning(true);next(60000);next(60040);assert.deepEqual(painted.at(-1),neutralAvatar);
 animator.dispose();assert.equal(pending.size,0);animator.setRunning(true);assert.equal(pending.size,0);
});
