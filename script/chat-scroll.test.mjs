import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ChatScroll } from '../frontend/src/chat-scroll.ts';

function fixture() {
 let top=0;
 const viewport={
  scrollHeight:2000,clientHeight:700,
  get scrollTop(){return top;},
  set scrollTop(value){top=Math.max(0,Math.min(value,this.scrollHeight-this.clientHeight));},
 };
 const position=new ChatScroll(viewport);
 position.latest();
 return {viewport,position};
}

test('opening follows the final viewport after composer mount and delayed multiline draft restoration',()=>{
 const {viewport,position}=fixture();
 assert.equal(viewport.scrollTop,1300);
 viewport.clientHeight=610;
 // Browser may emit a scroll/anchor event before ResizeObserver.
 assert.equal(position.scrolled(),true);
 assert.equal(viewport.scrollTop,1390);
 viewport.clientHeight=510;
 position.layout();
 assert.equal(viewport.scrollTop,1490);
 viewport.scrollHeight+=300;
 position.layout();
 assert.equal(viewport.scrollTop,1790);
});

test('manual history reading survives content and viewport changes; recall returns to the latest message',()=>{
 const {viewport,position}=fixture();
 viewport.scrollTop=400;
 assert.equal(position.scrolled(),false);
 viewport.scrollHeight+=500;
 viewport.clientHeight=500;
 position.layout();
 assert.equal(viewport.scrollTop,400);
 assert.equal(position.following,false);
 position.latest();
 assert.equal(viewport.scrollTop,2000);
 assert.equal(position.following,true);
 // Subsequent window growth also remains bottom-aligned.
 viewport.clientHeight=900;
 position.layout();
 assert.equal(viewport.scrollTop,1600);
});

test('loading older messages preserves the caller-restored anchor and manual return resumes following',()=>{
 const {viewport,position}=fixture();
 position.following=false;
 viewport.scrollTop=0;
 viewport.scrollHeight+=800;
 viewport.scrollTop=800; // The existing message anchor after prepend.
 position.layout();
 assert.equal(viewport.scrollTop,800);
 assert.equal(position.scrolled(),false);
 viewport.scrollTop=viewport.scrollHeight;
 assert.equal(position.scrolled(),true);
 viewport.scrollHeight+=200;
 position.layout();
 assert.equal(viewport.scrollTop,2300);
});
