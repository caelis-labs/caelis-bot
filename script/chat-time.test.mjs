import assert from 'node:assert/strict';
import test from 'node:test';
import { chatTimeline, chatTimeDivider, chatTimeLabel } from '../frontend/src/chat-time.ts';
import { withOutgoing } from '../frontend/src/chat-presentation.ts';

const at=(value)=>new Date(value).getTime()*1000;

test('timestamps divide only a new adjacent time window or local day',()=>{
 assert.equal(chatTimeDivider(undefined,at('2026-10-11T10:00:00')),false);
 assert.equal(chatTimeDivider(at('2026-10-11T10:00:00'),at('2026-10-11T10:04:59')),false);
 assert.equal(chatTimeDivider(at('2026-10-11T10:00:00'),at('2026-10-11T10:05:00')),true);
 assert.equal(chatTimeDivider(at('2026-10-10T23:58:00'),at('2026-10-11T00:01:00')),true);
 assert.equal(chatTimeDivider(undefined,undefined),false);
 assert.equal(chatTimeDivider(undefined,0),false);
});

test('time labels use the local conversation date',()=>{
 const now=new Date('2026-10-11T12:00:00');
 assert.equal(chatTimeLabel(at('2026-10-11T10:00:00'),'zh-CN',now),'10:00');
 assert.equal(chatTimeLabel(at('2026-10-10T18:30:00'),'zh-CN',now),'昨天 18:30');
});

const message=(id,time)=>({id,requestId:'',kind:'assistant',seenAt:time===undefined?undefined:at(time),text:'same text',status:'completed'});
const prior=message('history','2026-10-11T10:00:00');
const user={...message('outgoing:one','2026-10-11T10:06:00'),requestId:'one',kind:'user',status:'sending'};
const dividers=timeline=>timeline.rows.map(row=>row.dividerAt);

test('the first message is undivided; an outgoing message with older history gets its boundary immediately',()=>{
 assert.deepEqual(dividers(chatTimeline([user])),[undefined]);
 assert.deepEqual(dividers(chatTimeline([prior,user])),[undefined,user.seenAt]);
 assert.deepEqual(dividers(chatTimeline([prior,{...user,seenAt:prior.seenAt+60e6}])),[undefined,undefined]);
});

test('native receipt replacement and delayed timestamps cannot remove, insert or relabel visible dividers',()=>{
 let shown=chatTimeline([prior,user]);
 const native={...user,id:'native-user',status:'completed',seenAt:undefined};
 for(const seenAt of [undefined,at('2026-10-11T10:04:00'),at('2026-10-12T00:01:00')]){
  shown=chatTimeline([prior,{...native,seenAt}],shown.anchors);
  assert.deepEqual(dividers(shown),[undefined,user.seenAt]);
  assert.equal(shown.rows[1].item.status,'completed','presentation does not freeze receipt/content');
 }
});

test('a receipt crossing the gap/day boundary cannot add a divider to a just-sent message',()=>{
 for(const [before,sent,confirmed] of [
  ['2026-10-11T10:00:00','2026-10-11T10:04:59','2026-10-11T10:05:01'],
  ['2026-10-11T23:58:00','2026-10-11T23:59:59','2026-10-12T00:00:01'],
 ]){
  const history=message('history',before),local={...user,seenAt:at(sent)};
  const first=chatTimeline([history,local]);
  const receipt={...local,id:'native',status:'completed',seenAt:at(confirmed)};
  const settled=chatTimeline([history,receipt],first.anchors);
  assert.deepEqual(dividers(first),[undefined,undefined]);
  assert.deepEqual(dividers(settled),dividers(first));
  // Even a receipt arriving before the first paint retains the staged send time.
  assert.deepEqual(dividers(chatTimeline(withOutgoing([history,receipt],[local]))),dividers(first));
  assert.equal(receipt.seenAt,at(confirmed),'native timestamps remain untouched');
 }
});

test('late historical timestamps stay undated in place and inform future incoming rows',()=>{
 let shown=chatTimeline([message('history'),message('later')]);
 shown=chatTimeline([prior,{...user,id:'later',requestId:''}],shown.anchors);
 assert.deepEqual(dividers(shown),[undefined,undefined]);
 shown=chatTimeline([prior,{...user,id:'later',requestId:''},message('new','2026-10-11T10:12:00')],shown.anchors);
 assert.deepEqual(dividers(shown),[undefined,undefined,at('2026-10-11T10:12:00')]);
});

test('explicit earlier pagination may add a boundary, while removed rows release their presentation state',()=>{
 const current=chatTimeline([user]);
 const earlier=chatTimeline([prior,user],current.anchors);
 assert.deepEqual(dividers(earlier),[undefined,user.seenAt]);
 assert.equal(chatTimeline([],earlier.anchors).anchors.size,0);
 const differentRequest={...user,requestId:'two',seenAt:at('2026-10-11T10:01:00')};
 assert.deepEqual(dividers(chatTimeline([prior,differentRequest],earlier.anchors)),[undefined,undefined]);
});
