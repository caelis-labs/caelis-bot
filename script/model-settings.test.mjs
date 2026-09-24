import { test } from 'node:test';
import assert from 'node:assert/strict';
import { saveModelChanges } from '../frontend/src/model-settings.ts';
const conversation={model:'default-preview',effort:'high',serviceTier:'',approvalMode:'auto'};
const work={model:'',effort:'',serviceTier:''};

test('saving only work leaves untouched conversation defaults and permission policy inherited',async()=>{
 const saved={conversation,work},next={conversation,work:{...work,model:'worker'}};
 const writes=[],accepted=[];
 await saveModelChanges(next,saved,async(method,value)=>writes.push([method,value]),scope=>accepted.push(scope));
 assert.deepEqual(writes,[['SaveWorkExecutionSettings',next.work]]);
 assert.deepEqual(accepted,['work']);
});

test('partial save keeps successful receipt; retry only writes the remaining change',async()=>{
 let saved={conversation,work};
 const next={conversation:{...conversation,model:'chat',approvalMode:'user'},work:{...work,model:'worker'}};
 const writes=[];
 const accepted=scope=>{saved={...saved,[scope]:next[scope]};};
 await assert.rejects(saveModelChanges(next,saved,async(method)=>{
  writes.push(method);if(method==='SaveWorkExecutionSettings')throw new Error('工作仍在执行');
 },accepted),/对话设置已保存；工作任务设置未保存：工作仍在执行/);
 assert.equal(saved.conversation,next.conversation);assert.equal(saved.work,work);
 await saveModelChanges(next,saved,async(method)=>writes.push(method),accepted);
 assert.deepEqual(writes,['SaveExecutionSettings','SaveWorkExecutionSettings','SaveWorkExecutionSettings']);
 await saveModelChanges(next,saved,async()=>assert.fail('unchanged settings wrote again'),accepted);
});

test('rejected conversation change never attempts work settings',async()=>{
 const writes=[];
 await assert.rejects(saveModelChanges({conversation:{...conversation,serviceTier:'fast'},work:{...work,model:'worker'}},{conversation,work},async method=>{
  writes.push(method);throw new Error('等待审批');
 },()=>assert.fail('accepted rejected settings')),/对话设置未保存/);
 assert.deepEqual(writes,['SaveExecutionSettings']);
});
