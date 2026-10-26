import {test} from 'node:test';
import assert from 'node:assert/strict';
import {workerMessages,workerMessageStart} from '../frontend/src/worker-messages.ts';
import {createWorkerOutputReader,retainWorkerOutput} from '../frontend/src/worker-output-read.ts';

const observation=(items,status='running')=>({task:{id:'original-task',status},items,revision:'fixture'});
const transcript=(text,status='running')=>({status,messages:[{id:'original-message',role:'assistant',text}]});

test('completed output survives an empty live window until its exact native history message exists',()=>{
 const original={id:'live-original',historyId:'native-original',kind:'assistant',status:'completed',text:'same text'};
 const other={...original,id:'live-other',historyId:'native-other'};
 assert.deepEqual(retainWorkerOutput([original,other],[],undefined),[original,other]);
 assert.deepEqual(retainWorkerOutput([original,other],[],{status:'completed',messages:[{id:'native-original',role:'assistant',text:'same text'}]}),[other]);
 assert.deepEqual(retainWorkerOutput([original],[{...original,text:'updated original'}],undefined),[{...original,text:'updated original'}]);
});
test('retained output has a bounded window and never deduplicates by prose',()=>{
 const values=Array.from({length:220},(_,i)=>({id:String(i),historyId:'native-'+i,text:'identical',kind:'assistant',status:'completed'}));
 const retained=retainWorkerOutput(values,[]);assert.equal(retained.length,200);assert.equal(retained[0].id,'20');
});
function reader(overrides={}){
 const state={observations:[],histories:[],observationFailures:[],historyFailures:[],time:0};
 const poll=createWorkerOutputReader({id:'original-task',readObservation:async()=>observation([]),readHistory:async()=>transcript('native history'),
  observation:value=>state.observations.push(value),history:value=>state.histories.push(value),
  observationFailed:value=>state.observationFailures.push(value),historyFailed:value=>state.historyFailures.push(value),now:()=>state.time,...overrides});
 return {...state,poll,advance:()=>{state.time+=3000}};
}

test('null Worker items are safe and the actual poller keeps reading the same task',async()=>{
 let reads=0;
 const f=reader({readObservation:async id=>{assert.equal(id,'original-task');return observation(++reads===1?null:[{id:'output',kind:'assistant',status:'streaming',text:'recovered'}])}});
 await f.poll.read();assert.deepEqual(f.observations[0].items,[]);
 await f.poll.read();assert.equal(reads,2);assert.equal(f.observations.at(-1).items[0].text,'recovered');
 assert.deepEqual(f.observationFailures,[false,false]);
});

test('missing collections and failed reads retain observed text and history while recovery continues',async()=>{
 const original=[{id:'output',kind:'assistant',status:'streaming',text:'already received'}];
 let reads=0;
 const f=reader({readObservation:async()=>{
  reads++;if(reads===3)throw Error('fixture disconnected');
  return reads===1?observation(original):reads===2?observation(null):observation([{...original[0],text:'continued original output'}]);
 },readHistory:async()=>{if(reads>1)throw Error('native history temporarily unavailable');return transcript('retained history')}});
 await f.poll.read();await f.poll.read();
 assert.deepEqual(f.observations.at(-1).items,original,'null collection cannot erase previously observed output');
 f.advance();await f.poll.read();
 assert.equal(f.observations.length,2,'failed observation cannot replace the prior successful projection');
 assert.equal(f.histories.at(-1).messages[0].text,'retained history');
 assert.equal(f.historyFailures.at(-1),true);assert.equal(f.observationFailures.at(-1),true);
 await f.poll.read();assert.equal(f.observations.at(-1).items[0].text,'continued original output');
 assert.equal(f.observationFailures.at(-1),false);
});

test('synchronous and malformed observation failures release the in-flight guard',async()=>{
 let reads=0;
 const f=reader({readObservation:()=>{
  reads++;if(reads===1)throw Error('synchronous host refusal');
  return Promise.resolve(reads===2?{task:null,items:[]}:observation([]));
 }});
 for(let n=0;n<3;n++)await f.poll.read();
 assert.equal(reads,3);assert.deepEqual(f.observationFailures,[true,true,false]);
 assert.equal(f.observations.length,1);
});

test('overlapping ticks do not duplicate reads and a closed task view ignores late replies',async()=>{
 let finish,finishHistory,reads=0;
 const f=reader({readObservation:()=>{reads++;return new Promise(resolve=>finish=resolve)},readHistory:()=>new Promise(resolve=>finishHistory=resolve)});
 const first=f.poll.read();await Promise.resolve();await f.poll.read();assert.equal(reads,1);
 f.poll.close();finish(observation([{id:'late',text:'old surface'}]));finishHistory(transcript('late history'));await first;await f.poll.read();
 assert.equal(reads,1);assert.deepEqual(f.observations,[]);assert.deepEqual(f.histories,[]);
});

test('completed is separate from history readability and history read resumes after failure',async()=>{
 let histories=0;
 const f=reader({readObservation:async()=>observation([],'completed'),readHistory:async()=>{if(++histories===1)throw Error('history not ready');return transcript('final native result','completed')}});
 await f.poll.read();assert.equal(f.historyFailures.at(-1),true);assert.equal(f.observations[0].task.status,'completed');
 f.advance();await f.poll.read();assert.equal(f.histories.at(-1).messages[0].text,'final native result');
 f.advance();await f.poll.read();assert.equal(histories,2,'settled terminal history does not need repeated reads');
});


test('the unified Worker message protocol joins only exact Runtime identities and includes live user input',()=>{
 const user={id:'native-user-live',historyId:'msg-user',kind:'user',text:'same',status:'completed'};
 const reply={id:'native-reply-live',historyId:'msg-reply',kind:'assistant',text:'same',status:'inProgress'};
 const live=workerMessages(undefined,[user,reply]);
 assert.deepEqual(live.map(m=>[m.id,m.role]),[['msg-user','user'],['msg-reply','assistant']]);
 const history={messages:[{id:'msg-user',role:'user',text:'same'},{id:'msg-reply',role:'assistant',text:'same'}]};
 assert.deepEqual(workerMessages(history,[user,{...reply,status:'completed'}]).map(m=>m.id),['msg-user','msg-reply']);
 assert.equal(workerMessages(history,[{...reply,historyId:'',id:'legacy'}]).length,3,'legacy IDs cannot deduplicate by matching prose');
});
test('history and bounded streaming snapshots preserve a canonical message key and current text',()=>{
 const h={messages:[{id:'message',role:'assistant',text:'old progress'}]};
 const i={id:'live',historyId:'message',kind:'assistant',status:'inProgress',text:'new progress'};
 assert.equal(workerMessages(h,[i])[0].text,'new progress');
 assert.equal(workerMessages({messages:[{id:'message',role:'assistant',text:'new progress completed'}]},[i])[0].text,'new progress completed');
 assert.equal(workerMessages(undefined,[i])[0].id,workerMessages(h,[i])[0].id);
 const resumed={...i,text:'old progress extended',status:'completed'};
 assert.equal(retainWorkerOutput([], [resumed],h).length,1,'a completed native extension awaits its own full history');
 assert.equal(workerMessages(h,[resumed])[0].text,resumed.text);
});
test('unfinished replies stay between their completed native history anchors',()=>{
 const h={messages:[{id:'first',role:'user',text:'instruction'},{id:'third',role:'user',text:'steer'}]};
 const items=[['first','user'],['second','assistant'],['third','user']].map(([id,kind])=>({id:'live-'+id,historyId:id,kind,text:id,status:'inProgress'}));
 assert.deepEqual(workerMessages(h,items).map(m=>m.id),['first','second','third']);
});
test('slow native history does not block live messages, and close fences both channels',async()=>{
 let finish,reads=0;
 const f=reader({readHistory:()=>new Promise(resolve=>finish=resolve),readObservation:async()=>observation([{id:'u',historyId:'msg-u',kind:'user',text:'input '+(++reads),status:'completed'}])});
 const first=f.poll.read();await new Promise(resolve=>setImmediate(resolve));
 await f.poll.read();assert.equal(reads,2);assert.equal(f.observations.at(-1).items[0].text,'input 2');
 f.poll.close();finish(transcript('late history'));await first;
 assert.deepEqual(f.histories,[]);
});
test('a restarted view retains received messages while both native reads are unavailable',async()=>{
 const cached=observation([{id:'u',historyId:'msg-u',kind:'user',text:'preserved',status:'completed'}]);
 const f=reader({initialObservation:cached,readObservation:async()=>observation(null),readHistory:async()=>{throw Error('offline')}});
 await f.poll.read();assert.equal(workerMessages(undefined,f.observations[0].items)[0].text,'preserved');
});


test('terminal history arriving before the first running snapshot cannot stop history polling',async()=>{
 let resolveFirst,histories=0,observations=0;
 const f=reader({readHistory:async()=>{histories++;return transcript('finished previous turn','completed')},readObservation:()=>{observations++;return observations===1?new Promise(resolve=>resolveFirst=resolve):Promise.resolve(observation([],'working'))}});
 const first=f.poll.read();await new Promise(resolve=>setImmediate(resolve));
 resolveFirst(observation([],'working'));await first;f.advance();await f.poll.read();
 assert.equal(histories,2);
});


test('completed native history covers the real Codex prefix and Caelis UTF-8 tail bounds',()=>{
 const full='原始消息🙂'.repeat(16000)+'complete';
 const prefix=Array.from(full).slice(0,16000).join(''),codex=prefix+'…';
 assert.equal(workerMessages({messages:[{id:'message',role:'assistant',text:prefix}]},[{id:'live',historyId:'message',kind:'assistant',status:'inProgress',text:codex}])[0].text,codex,'a shorter history row does not cover a truncated extension');
 const encoded=new TextEncoder().encode(full),tail=encoded.slice(-(128<<10));
 let start=0;while((tail[start]&0xc0)===0x80)start++;
 const caelis=new TextDecoder().decode(tail.slice(start));
 for(const text of [codex,caelis]){
  const item={id:'native-live',historyId:'message',kind:'assistant',status:'inProgress',text};
  const history={messages:[{id:'message',role:'assistant',text:full}]};
  assert.equal(workerMessages(history,[item])[0].text,full,'an older bounded snapshot cannot replace full completed text');
  assert.deepEqual(retainWorkerOutput([], [item],history),[]);
 }
});

test('native activity files remain accessible without exposing tool prose or inventing file IDs',()=>{
 const artifacts=[{id:'task-file:original-task:image-output',name:'result.png'},{id:'task-file:original-task:file-change',name:'report.md'}];
 const tool={id:'tool-output',historyId:'native-tool',kind:'activity',status:'completed',text:'PRIVATE_TOOL_BODY',artifacts};
 const retained=retainWorkerOutput([tool],[],{messages:[{id:'native-answer',role:'assistant',text:'Ready'}]});
 const messages=workerMessages(undefined,retained);
 assert.deepEqual(messages,[{id:'native-tool',role:'assistant',text:'',status:'completed',artifacts}]);
 assert.deepEqual(workerMessages({messages:[{id:'native-tool',role:'tool',text:'PRIVATE_TOOL_BODY',artifacts}]}),messages);
 assert.equal(workerMessages(undefined,[{...tool,id:'no-files',artifacts:[]}]).length,0);
 assert.equal(workerMessages(undefined,[{...tool,id:'',artifacts}]).length,0,'unbound files cannot become message identity');
});

test('history text alone cannot discard validated files attached to the original observation',()=>{
 const artifact={id:'task-file:original-task:output',name:'report.md'};
 const item={id:'live',historyId:'message',kind:'assistant',status:'completed',text:'Done',artifacts:[artifact]};
 const history={messages:[{id:'message',role:'assistant',text:'Done in full'}]};
 const retained=retainWorkerOutput([], [item],history);
 assert.deepEqual(retained,[item]);
 const [merged]=workerMessages(history,retained);
 assert.equal(merged.text,'Done in full');assert.deepEqual(merged.artifacts,[artifact]);
 history.messages[0].artifacts=[artifact];
 assert.deepEqual(retainWorkerOutput(retained,[],history),[],'history now owns both text and exact file');
 assert.deepEqual(workerMessages(history,[item])[0].artifacts,[artifact],'same native artifact remains one entry');
});

test('paused Worker history keeps its starting message through append and earlier-page expansion',()=>{
 const messages=Array.from({length:90},(_,index)=>({id:'message-'+(index+1)}));
 const first=messages.slice(0,50),start=workerMessageStart(first,40),anchor=first[start].id;
 assert.equal(anchor,'message-11');
 const appended=messages.slice(0,51);
 assert.equal(appended[workerMessageStart(appended,40,anchor)+9].id,'message-20','appending does not shift the same reading offset to message 21');
 const expandedAnchor=appended[Math.max(0,workerMessageStart(appended,40,anchor)-40)].id;
 assert.equal(expandedAnchor,'message-1');
 assert.equal(workerMessageStart(messages,51,expandedAnchor),0,'loading earlier then appending retains the expanded original window');
 assert.equal(workerMessageStart(messages,40),50,'explicit latest resumes the bounded tail');
 assert.equal(workerMessageStart(messages,40,'missing-original-id'),50,'missing identity has a safe bounded fallback');
});
