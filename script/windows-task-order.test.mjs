import assert from 'node:assert/strict';
import test from 'node:test';
import {preserveTaskOrder} from '../frontend/src/windows-task-order.ts';
import {taskCardFrames} from '../frontend/src/windows-task-layout.ts';

test('hover keeps card order while accepting status, removals and new cards', () => {
  const previous = [{id:'a',status:'running'}, {id:'b',status:'running'}, {id:'removed',status:'completed'}];
  const incoming = [{id:'b',status:'approval'}, {id:'new',status:'running'}, {id:'a',status:'completed'}];
  assert.deepEqual(preserveTaskOrder(previous,incoming,true), [incoming[2],incoming[0],incoming[1]]);
  assert.deepEqual(preserveTaskOrder(previous,incoming,false), incoming);
  assert.equal(previous[0].status,'running');
});

test('dense task fans stay inside the window with an exposed edge for every card',()=>{
 for(const layout of [{width:568,height:196,vertical:false,reverse:false},{width:240,height:428,vertical:true,reverse:true},{width:240,height:300,vertical:true,reverse:false}]) {
  for(const count of [1,3,20,500]) {
   const frames=taskCardFrames(layout,count);
   assert.equal(frames.length,count);
   for(const [i,f] of frames.entries()) {
    assert.ok(f.left>=0&&f.top>=-1e-10&&f.left+f.width<=layout.width&&f.top+f.height<=layout.height-(layout.vertical?48:0));
    if(i)assert.notEqual(layout.vertical?f.top:f.left,layout.vertical?frames[i-1].top:frames[i-1].left);
   }
  }
 }
});
