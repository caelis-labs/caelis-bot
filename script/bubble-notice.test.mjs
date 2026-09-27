import test from 'node:test';
import assert from 'node:assert/strict';
import {reduceBubbleNotice,bubblePresentation} from '../frontend/src/bubble-notice.ts';

const pending={id:1,message:'Waiting for terminal confirmation',pending:true};
const failure={id:2,message:'Terminal did not open',pending:false};
test('ending an operation clears its pending hint, not newer feedback',()=>{
 assert.equal(reduceBubbleNotice(pending,{type:'clearPending'}),null);
 assert.equal(reduceBubbleNotice(failure,{type:'clearPending'}),failure);
});
test('expiration/dismissal cannot clear a successor notice',()=>{
 const replacement=reduceBubbleNotice(failure,{type:'show',notice:pending});
 assert.equal(reduceBubbleNotice(replacement,{type:'dismiss',id:failure.id}),pending);
 assert.equal(reduceBubbleNotice(replacement,{type:'dismiss',id:pending.id}),null);
});
test('notice uses the existing bubble without changing dismissed conversation state',()=>{
 assert.deepEqual(bubblePresentation('prior result',false,false,pending),{
  content:pending.message,wanted:true,showNotice:true,
 });
 assert.deepEqual(bubblePresentation('prior result',false,false,null),{
  content:'prior result',wanted:false,showNotice:false,
 });
});
test('approval/recovery attention takes priority and terminal completion preserves it',()=>{
 for(const notice of [pending,failure,null])assert.deepEqual(bubblePresentation('approval',true,true,notice),{
  content:'approval',wanted:true,showNotice:false,
 });
});
