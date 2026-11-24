import {test} from 'node:test';
import assert from 'node:assert/strict';
import {observeCharacterCanvas} from '../frontend/src/character/canvas-resolution.ts';

test('resize and a monitor-only density change update the visible character buffer', t => {
 const canvas = {clientWidth:180, clientHeight:240};
 const display = {devicePixelRatio:2};
 const frames = [];
 let media, resize, disconnected = false;
 const define = (name, value) => {
  const original = Object.getOwnPropertyDescriptor(globalThis,name);
  Object.defineProperty(globalThis,name,{configurable:true,value});
  t.after(()=>{if(original) Object.defineProperty(globalThis,name,original); else delete globalThis[name];});
 };
 define('window',display);
 define('matchMedia',query=>{
  assert.equal(query,`(resolution: ${display.devicePixelRatio}dppx)`);
  return media = new EventTarget();
 });
 define('ResizeObserver',class {
  constructor(callback){resize=callback;}
  observe(target){assert.equal(target,canvas);}
  disconnect(){disconnected=true;}
 });
 const stop = observeCharacterCanvas({
  domElement:canvas,
  setDrawingBufferSize(w,h,dpr){frames.push([w*dpr,h*dpr]);}
 },()=>{});
 assert.deepEqual(frames.at(-1),[360,480]);
 Object.assign(canvas,{clientWidth:288,clientHeight:384});
 resize();
 assert.deepEqual(frames.at(-1),[576,768]);
 display.devicePixelRatio=1.25;
 media.dispatchEvent(new Event('change'));
 assert.deepEqual(frames.at(-1),[360,480]);
 stop();
 assert.ok(disconnected);
 media.dispatchEvent(new Event('change'));
 assert.equal(frames.length,3);
});
