import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createHitMaskEncoder} from '../frontend/src/character/hit-mask.ts';
import {legacyHitMask,maskFrames} from './pet-hit-mask-fixture.mjs';

const width=180,height=240;
function pixel(rgba,width,x,y,alpha){rgba[(y*width+x)*4+3]=alpha;}
function bits(encoded){return Uint8Array.from(atob(encoded),c=>c.charCodeAt(0));}
function hit(packed,width,x,y){const i=y*width+x;return (packed[i>>3]>>(i&7))&1;}

test('radius-two mask equals the running algorithm at every corner, edge, threshold and packed boundary',()=>{
 const encode=createHitMaskEncoder(width,height),rgba=new Uint8Array(width*height*4);
 for(const [x,y] of [[0,0],[1,0],[179,0],[0,239],[179,239],[0,120],[179,120],[90,0],[90,239],[6,3],[7,8],[8,17],[89,119]]){
  for(const alpha of [0,1,16,17,254,255]){
   rgba.fill(0);pixel(rgba,width,x,y,alpha);
   assert.equal(encode(rgba),legacyHitMask(rgba,width,height),`(${x},${y}), alpha=${alpha}`);
  }
 }
});

test('bottom-up coordinates and least-significant-first bit order remain exact for first input',()=>{
 const rgba=new Uint8Array(width*height*4);pixel(rgba,width,0,0,17);
 const encoded=createHitMaskEncoder(width,height)(rgba),packed=bits(encoded);
 assert.equal(packed.length,5400);
 assert.equal(packed[0],0b00000111);
 for(let y=0;y<height;y++)for(let x=0;x<width;x++)assert.equal(hit(packed,width,x,y),x<=2&&y<=2?1:0);
 assert.equal(hit(packed,width,0,239),0,'WebGL bottom row must not be vertically flipped');
});

test('full-resolution masks and changing poses never retain previous opaque coverage',()=>{
 const encode=createHitMaskEncoder(width,height),frames=maskFrames(width,height);
 const original=frames.body.slice();
 for(const frame of [frames.blank,frames.dense,frames.body,frames.shifted,frames.blank,frames.body,frames.dense,frames.blank])assert.equal(encode(frame),legacyHitMask(frame,width,height));
 assert.deepEqual(frames.body,original,'the GPU readback buffer remains caller-owned');
 let seed=0x53c901;
 const rgba=new Uint8Array(width*height*4);
 for(let pose=0;pose<24;pose++){
  rgba.fill(0);
  for(let i=0;i<width*height/7;i++){
   seed=(Math.imul(seed,1664525)+1013904223)>>>0;
   const index=seed%(width*height);rgba[index*4+3]=[0,16,17,255][seed>>>30];
  }
  assert.equal(encode(rgba),legacyHitMask(rgba,width,height),`pose ${pose}`);
 }
});

test('small dimensions clamp both separable passes without wrapping rows or padding bits',()=>{
 for(const [w,h] of [[1,1],[1,8],[8,1],[2,3],[3,2],[4,4],[9,8]]){
  const encode=createHitMaskEncoder(w,h),rgba=new Uint8Array(w*h*4);
  for(let i=0;i<w*h;i++){
   rgba.fill(0);rgba[i*4+3]=255;
   assert.equal(encode(rgba),legacyHitMask(rgba,w,h),`${w}x${h} source ${i}`);
  }
 }
});

test('unchanged packed bytes reuse encoding, while a newly selected or cleared pose updates immediately',t=>{
 const original=globalThis.btoa;let conversions=0;
 t.mock.method(globalThis,'btoa',value=>{conversions++;return original(value);});
 const encode=createHitMaskEncoder(width,height),frames=maskFrames(width,height);
 const blank=encode(frames.blank);assert.equal(conversions,1,'the first transparent mask must still be encoded');
 assert.equal(encode(frames.blank),blank);assert.equal(conversions,1);
 const body=encode(frames.body);assert.equal(conversions,2);assert.notEqual(body,blank);
 assert.equal(encode(frames.body),body);assert.equal(conversions,2);
 assert.notEqual(encode(frames.shifted),body);assert.equal(conversions,3);
 assert.equal(encode(frames.blank),blank);assert.equal(conversions,4);
 const replacement=createHitMaskEncoder(width,height);
 assert.equal(replacement(frames.body),body);assert.equal(conversions,5,'a replacement renderer owns its own cache');
});
