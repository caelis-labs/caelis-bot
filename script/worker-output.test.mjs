import {test} from 'node:test';
import assert from 'node:assert/strict';
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
 let finish,reads=0;
 const f=reader({readObservation:()=>{reads++;return new Promise(resolve=>finish=resolve)}});
 const first=f.poll.read();await Promise.resolve();await f.poll.read();assert.equal(reads,1);
 f.poll.close();finish(observation([{id:'late',text:'old surface'}]));await first;await f.poll.read();
 assert.equal(reads,1);assert.deepEqual(f.observations,[]);assert.deepEqual(f.histories,[]);
});

test('completed is separate from history readability and history read resumes after failure',async()=>{
 let histories=0;
 const f=reader({readObservation:async()=>observation([],'completed'),readHistory:async()=>{if(++histories===1)throw Error('history not ready');return transcript('final native result','completed')}});
 await f.poll.read();assert.equal(f.historyFailures.at(-1),true);assert.equal(f.observations[0].task.status,'completed');
 f.advance();await f.poll.read();assert.equal(f.histories.at(-1).messages[0].text,'final native result');
 f.advance();await f.poll.read();assert.equal(histories,2,'settled terminal history does not need repeated reads');
});
