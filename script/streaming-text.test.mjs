import {test} from 'node:test';
import assert from 'node:assert/strict';
import {TextReveal} from '../frontend/src/streaming-text.ts';

test('chunked replies reveal many intermediate frames and retain full exact final Markdown',()=>{
 const reveal=new TextReveal();
 const first='这是流式输出。**内容会逐字呈现**，而不是一整块跳出来。';
 reveal.update(first,true,0);
 const frames=Array.from({length:27},(_,i)=>reveal.value(i*16));
 assert.ok(new Set(frames).size>15);
 for(let i=1;i<frames.length;i++)assert.ok(frames[i].startsWith(frames[i-1]));
 const final=first+'\n\n| 名称 | 状态 |\n| --- | --- |\n| 测试 | 完成 |';
 reveal.update(final,true,450);
 assert.equal(reveal.value(450),first);
 reveal.update(final,false,550);
 assert.notEqual(reveal.value(550),final);
 assert.equal(reveal.value(670),final);
 assert.equal(reveal.pending,false);
});
test('frequent provider deltas cannot starve progress and a large backlog has a bounded drain',()=>{
 const reveal=new TextReveal();let text='';
 for(let n=0;n<100;n++){
  text+='文';reveal.update(text,true,n*20);reveal.value(n*20+16);
 }
 assert.ok(reveal.value(2000).length>70);
 text+='末尾'.repeat(2000);reveal.update(text,true,2000);
 assert.notEqual(reveal.value(2200),text);
 assert.equal(reveal.value(2450),text);
});
test('graphemes stay whole, including emoji families, combining marks and flags',()=>{
 const text='👩🏽‍💻家👨‍👩‍👧‍👦e\u0301🇨🇳结束',reveal=new TextReveal();
 const prefixes=new Set(['']);let prefix='';
 for(const part of new Intl.Segmenter(undefined,{granularity:'grapheme'}).segment(text)){prefix+=part.segment;prefixes.add(prefix);}
 reveal.update(text,true,0);
 for(let n=0;n<500;n+=7)assert.ok(prefixes.has(reveal.value(n)));
 assert.equal(reveal.value(500),text);
});
test('history, replaced snapshots, reduced motion and hidden surfaces show authoritative text immediately',()=>{
 const reveal=new TextReveal();
 reveal.update('已有历史',false,0);assert.equal(reveal.value(0),'已有历史');
 reveal.update('已有历史，补充',true,10);
 reveal.update('服务端已修正',true,20);assert.equal(reveal.value(20),'服务端已修正');
 reveal.update('服务端已修正，完整内容',true,30,true);assert.equal(reveal.value(30),'服务端已修正，完整内容');
 assert.equal(reveal.pending,false);
});
