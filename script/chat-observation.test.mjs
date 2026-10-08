import {test} from 'node:test';
import assert from 'node:assert/strict';
import {ConversationOrder,FileObservationOrder,SubmissionProgress,DraftQueue,acceptedDraftSettled,visibleDraftFiles,readDraftAndFiles,retryRead,sameComposerSnapshot} from '../frontend/src/chat-observation.ts';
import {settingsDestination,settingsSections} from '../frontend/src/settings-navigation.ts';

test('a late poll cannot replace a refreshed outbox at the same backend revision',()=>{
 const state=new ConversationOrder();state.reset();
 const poll=state.request(),refresh=state.request();
 assert.equal(state.accept(refresh,12),true);
 assert.equal(state.accept(poll,12),false);
 assert.equal(state.accept(state.request(),11),false);
 assert.equal(state.accept(state.request(),12),true);
});
test('closing and reopening creates a fresh baseline and rejects hidden-surface reads',()=>{
 const state=new ConversationOrder();const closing=state.request();state.accept(closing,25);
 state.reset();assert.equal(state.revision,0);assert.equal(state.accept(closing,30),false);
 assert.equal(state.accept(state.request(),25),true);
});
test('polling and direct acceptance consume the draft once, including delayed completion',async()=>{
 const state=new SubmissionProgress();let resolve,count=0;
 const read=()=>{count++;return new Promise(r=>resolve=r);};
 state.observe('accepted');const poll=state.synchronize(read);
 state.observe('accepted');const direct=state.synchronize(read);
 assert.equal(poll,direct);await Promise.resolve();assert.equal(count,1);
 resolve();await Promise.all([poll,direct]);assert.equal(state.observe('unknown'),'accepted');
});
test('accepted send remains accepted while only failed draft reads recover',async()=>{
 const state=new SubmissionProgress();let reads=0;
 assert.throws(()=>state.synchronize(async()=>{}));state.observe('accepted');
 const read=()=>{reads++;return Promise.reject(new Error('draft offline'));};
 await assert.rejects(state.synchronize(read));assert.equal(state.outcome,'accepted');
 await assert.rejects(state.synchronize(read));assert.equal(reads,2);
 await state.synchronize(async()=>{reads++;});
 await state.synchronize(read);assert.equal(reads,3);
 assert.equal(state.observe('rejected'),'accepted');
});
test('accepted image send settles from the backend journal even while selection persistence fails',()=>{
 const request={text:'',fileIds:['pasted-image'],referenceIds:[]};
 const old={revision:4,text:'',referenceIds:[]};
 assert.equal(acceptedDraftSettled(request,4,old),false);
 const failed={...old,revision:5,cleanupPending:true,consumedFileIds:['pasted-image'],notice:'cleanup not saved'};
 assert.equal(acceptedDraftSettled(request,4,failed),true);
 assert.equal(acceptedDraftSettled(request,4,{...failed,consumedFileIds:['other-image']}),false);
 assert.deepEqual(visibleDraftFiles([{id:'pasted-image'},{id:'new-dropped-image'}],failed),[{id:'new-dropped-image'}]);
 assert.equal(acceptedDraftSettled(request,4,{...old,revision:5}),true);
 const captioned={text:'describe',fileIds:['dropped-file'],referenceIds:['reference-one']};
 assert.equal(acceptedDraftSettled(captioned,7,{revision:8,text:'new draft',referenceIds:[]}),true);
});
test('a slow accepted Draft read cannot overwrite a newer files-changed selection',async()=>{
 const order=new FileObservationOrder();let releaseDraft,releaseFiles;
 const slowDraft=new Promise(resolve=>releaseDraft=resolve);
 const acceptedTicket=order.request();
 let fileReads=0;
 const acceptedRead=readDraftAndFiles(()=>slowDraft,()=>{fileReads++;return new Promise(resolve=>releaseFiles=resolve);});
 assert.equal(fileReads,0,'DraftFiles started before draft cleanup');
 releaseDraft({revision:2,text:'',referenceIds:[]});
 await Promise.resolve();
 assert.equal(fileReads,1);
 const eventTicket=order.request();
 let visible=[];
 if(order.accept(eventTicket))visible=[{id:'new-image'}];
 releaseFiles([]); // The older in-flight native read returns after the event.
 const [,oldFiles]=await acceptedRead;
 if(order.accept(acceptedTicket))visible=oldFiles;
 assert.deepEqual(visible,[{id:'new-image'}]);
 order.reset();assert.equal(order.accept(eventTicket),false);
});
test('a failed initial observation recovers without reopening the surface and stops after success',async(t)=>{
 t.mock.timers.enable({apis:['setTimeout']});let reads=0,failures=0;const observed=[];
 const cancel=retryRead(async()=>{if(++reads===1)throw new Error('host unavailable');return 'original draft';},v=>observed.push(v),()=>failures++);
 await Promise.resolve();await Promise.resolve();assert.equal(failures,1);
 t.mock.timers.tick(3000);await Promise.resolve();await Promise.resolve();
 assert.deepEqual(observed,['original draft']);assert.equal(reads,2);
 t.mock.timers.tick(6000);await Promise.resolve();assert.equal(reads,2);cancel();
});
test('closing a surface cancels retries and rejects a delayed original read',async(t)=>{
 t.mock.timers.enable({apis:['setTimeout']});let resolve,reads=0;const observed=[];
 const cancel=retryRead(()=>{reads++;return new Promise(r=>resolve=r);},v=>observed.push(v));
 cancel();resolve('old draft');await Promise.resolve();assert.deepEqual(observed,[]);
 const stop=retryRead(async()=>{reads++;throw new Error('offline');},v=>observed.push(v));
 await Promise.resolve();await Promise.resolve();stop();t.mock.timers.tick(6000);await Promise.resolve();
 assert.equal(reads,2);assert.deepEqual(observed,[]);
});
test('uncertain submissions stay uncertain until that exact request is confirmed',()=>{
 const state=new SubmissionProgress();assert.equal(state.observe(''),'unknown');
 assert.throws(()=>state.synchronize(async()=>{}));assert.equal(state.observe('accepted'),'accepted');
});
test('settings separate models, accounts and machines while native repair links remain valid',()=>{
 assert.deepEqual(settingsSections,['general','appearance','models','connections','chatConnections','plugins','machines','extras','permissions','updates']);
 assert.equal(settingsDestination('runtime'),'connections');assert.equal(settingsDestination('machines'),'machines');
 assert.equal(settingsDestination('connections'),'connections');assert.equal(settingsDestination('telegram'),'telegram');assert.equal(settingsDestination('chat'),'chatConnections');assert.equal(settingsDestination('capture'),'extras');
 assert.equal(settingsDestination('execution'),'permissions');assert.equal(settingsDestination('setup'),'setup');
 assert.equal(settingsDestination('invalid'),null);
});

test('typing waits for a quiet period, and send flush waits for the newest replacement',async(t)=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const queue=new DraftQueue(),writes=[];let release;
 queue.enqueue(async()=>{writes.push('first');await new Promise(resolve=>release=resolve);});
 t.mock.timers.tick(399);await Promise.resolve();assert.deepEqual(writes,[]);
 t.mock.timers.tick(1);await Promise.resolve();assert.deepEqual(writes,['first']);
 queue.enqueue(async()=>{writes.push('intermediate');});
 queue.enqueue(async()=>{writes.push('newest');});
 let flushed=false;const sending=queue.flush().then(()=>flushed=true);
 await Promise.resolve();assert.equal(flushed,false);release();await sending;
 assert.deepEqual(writes,['first','newest']);assert.equal(flushed,true);
 queue.enqueue(async()=>{writes.push('after send');});
 t.mock.timers.tick(400);await queue.flush();assert.equal(writes.at(-1),'after send');
});

test('draft debounce reduces rapid input to one durable replacement and switch flushes immediately',async(t)=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const queue=new DraftQueue(),saved=[];
 for(let n=1;n<=35;n++){
  queue.enqueue(async()=>{saved.push(n);});
  t.mock.timers.tick(32);
 }
 await Promise.resolve();assert.deepEqual(saved,[]);
 await queue.flush();assert.deepEqual(saved,[35]);
 queue.enqueue(async()=>{saved.push(36);});
 t.mock.timers.tick(399);await Promise.resolve();assert.deepEqual(saved,[35]);
 t.mock.timers.tick(1);await queue.flush();assert.deepEqual(saved,[35,36]);
});

test('history and streaming snapshots preserve Composer props until a control fact changes',()=>{
 const before={connection:'ready',phase:'working',maintenance:'',canSend:false,canSteer:true,canInterrupt:true,lastReceipt:{id:'original',outcome:'unknown'},references:[],items:[{id:'one',text:'first'}],revision:3};
 const stream={...before,revision:4,items:[{id:'one',text:'stream chunk'}]};
 assert.equal(sameComposerSnapshot(before,stream),true);
 assert.equal(sameComposerSnapshot(before,{...stream,lastReceipt:{id:'original',outcome:'accepted'}}),false);
 assert.equal(sameComposerSnapshot(before,{...stream,canSend:true}),false);
 assert.equal(sameComposerSnapshot(before,{...stream,references:[{id:'reference'}]}),false);
});

test('a failed draft write rejects the send and window-switch barrier',async()=>{
 const queue=new DraftQueue();
 queue.enqueue(async()=>{throw new Error('disk failed');});
 await assert.rejects(queue.flush(),/disk failed/);
 queue.enqueue(async()=>{});
 await queue.flush();
});
