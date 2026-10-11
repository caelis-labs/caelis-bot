import assert from 'node:assert/strict';
import test from 'node:test';
import { chatTimeDivider, chatTimeLabel } from '../frontend/src/chat-time.ts';

const at=(value)=>new Date(value).getTime()*1000;

test('timestamps divide only a new adjacent time window or local day',()=>{
 assert.equal(chatTimeDivider(undefined,at('2026-10-11T10:00:00+08:00')),true);
 assert.equal(chatTimeDivider(at('2026-10-11T10:00:00+08:00'),at('2026-10-11T10:04:59+08:00')),false);
 assert.equal(chatTimeDivider(at('2026-10-11T10:00:00+08:00'),at('2026-10-11T10:05:00+08:00')),true);
 assert.equal(chatTimeDivider(at('2026-10-10T23:58:00+08:00'),at('2026-10-11T00:01:00+08:00')),true);
 assert.equal(chatTimeDivider(undefined,undefined),false);
 assert.equal(chatTimeDivider(undefined,0),false);
});

test('time labels use the local conversation date',()=>{
 const now=new Date('2026-10-11T12:00:00+08:00');
 assert.equal(chatTimeLabel(at('2026-10-11T10:00:00+08:00'),'zh-CN',now),'10:00');
 assert.equal(chatTimeLabel(at('2026-10-10T18:30:00+08:00'),'zh-CN',now),'昨天 18:30');
});
