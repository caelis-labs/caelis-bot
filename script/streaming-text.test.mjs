import {test} from 'node:test';
import assert from 'node:assert/strict';
import {TextReveal} from '../frontend/src/streaming-text.ts';

function frames(reveal, start, end, interval=16) {
 return Array.from({length:Math.floor((end-start)/interval)+1},(_,i)=>reveal.value(start+i*interval));
}
function smooth(values, limit=2) {
 for(let i=1;i<values.length;i++) {
  assert.ok(values[i].startsWith(values[i-1]), 'visible text never regresses');
  assert.ok(values[i].length-values[i-1].length<=limit, 'a frame must not dump a chunk');
 }
}

test('450ms snapshots keep a continuous character budget instead of racing each chunk',()=>{
 const reveal=new TextReveal();let text='',values=[];
 for(let n=0;n<12;n++) {
  text+='这是流式正文，应该连续平滑地显示。'.repeat(2);
  reveal.update(text,true,n*450);
  values.push(...frames(reveal,n*450,n*450+448));
 }
 smooth(values,3);
 assert.ok(new Set(values).size>250);
 // No repeated finish-and-wait gaps at snapshot boundaries.
 let gap=0,maxGap=0;
 for(let i=1;i<values.length;i++){gap=values[i]===values[i-1]?gap+1:0;maxGap=Math.max(maxGap,gap);}
 assert.ok(maxGap<=1, `paused for ${maxGap} frames`);
 assert.equal(frames(reveal,5400,9000).at(-1),text);
});

test('large provider and final chunks remain paced, with exact final Markdown',()=>{
 const reveal=new TextReveal(),first='开头正文。';
 reveal.update(first,true,0);frames(reveal,0,200);
 const final=first+'内容'.repeat(300)+'\n\n| 名称 | 状态 |\n| --- | --- |\n| 测试 | 完成 |';
 reveal.update(final,true,208);
 const values=frames(reveal,208,1000);
 smooth(values);
 assert.ok(values.at(-1).length<150, 'must not drain a large final chunk in 120/450ms');
 assert.equal(frames(reveal,1008,9000).at(-1),final);
 assert.equal(reveal.pending,false);
});

test('frequent tiny deltas retain fractional progress and never starve',()=>{
 const reveal=new TextReveal();let text='';
 for(let now=0;now<=1000;now+=5){text+='文';reveal.update(text,true,now);}
 assert.ok(reveal.value(1005).length>70);
 assert.equal(frames(reveal,1008,5000).at(-1),text);
});

test('a stalled frame or an idle gap cannot cause a burst',()=>{
 const reveal=new TextReveal();reveal.update('文'.repeat(500),true,0);
 const before=reveal.value(16),after=reveal.value(3000);
 assert.ok(after.length-before.length<=6);
 reveal.update('历史',false,3010);
 reveal.update('历史'+'字'.repeat(200),true,9000);
 assert.equal(reveal.value(9000),'历史');
 assert.ok(reveal.value(9016).length<=4);
});

test('graphemes stay whole, including emoji families, combining marks and flags',()=>{
 const text='👩🏽‍💻家👨‍👩‍👧‍👦e\u0301🇨🇳结束',reveal=new TextReveal();
 const prefixes=new Set(['']);let prefix='';
 for(const part of new Intl.Segmenter(undefined,{granularity:'grapheme'}).segment(text)){prefix+=part.segment;prefixes.add(prefix);}
 reveal.update(text,true,0);
 for(const value of frames(reveal,0,500,7))assert.ok(prefixes.has(value));
 assert.equal(reveal.value(500),text);
});

test('history, corrections, inactive surfaces and reduced motion display immediately',()=>{
 const reveal=new TextReveal();
 reveal.update('已有历史',false,0);assert.equal(reveal.value(0),'已有历史');
 reveal.update('已有历史，补充',true,10);
 reveal.update('服务端已修正',true,20);assert.equal(reveal.value(20),'服务端已修正');
 reveal.update('服务端已修正，完整内容',true,30,true);assert.equal(reveal.value(30),'服务端已修正，完整内容');
 reveal.update('服务端已修正，完整内容以及新增内容',true,40);
 assert.equal(reveal.pending,true);
 reveal.update('服务端已修正，完整内容以及新增内容',false,50);
 assert.equal(reveal.pending,false);
});

test('terminal incomplete item flushes only bytes actually received, including after remount',()=>{
 const partial='Core #111 的流终止修';
 const reveal=new TextReveal();
 reveal.update(partial,true,0);
 assert.notEqual(reveal.value(16),partial);
 reveal.update(partial,false,17);
 assert.equal(reveal.value(17),partial);
 assert.equal(reveal.pending,false);
 const remounted=new TextReveal();
 remounted.update(partial,false,1000);
 assert.equal(remounted.value(1000),partial);
});

for (const [before, after] of [['e', 'e\u0301'], ['hello e', 'hello e\u0301'], ['👩', '👩🏽‍💻'], ['🇨', '🇨🇳']]) {
 test(`appended grapheme bytes never erase displayed text: ${before}`, () => {
  const reveal = new TextReveal();
  reveal.update(before, true, 0);
  assert.equal(frames(reveal,0,496).at(-1), before);
  reveal.update(after + ' next', true, 500);
  const values=[before,...frames(reveal,500,1000,10)];
  for(let i=1;i<values.length;i++)assert.ok(values[i].startsWith(values[i-1]));
  assert.equal(values.at(-1), after + ' next');
 });
}

test('the first visible reply contains a whole grapheme without dumping a chunk',()=>{
 const reveal=new TextReveal();reveal.update('👩🏽‍💻正在处理。'.repeat(30),true,0);
 assert.equal(reveal.value(0),'👩🏽‍💻');assert.ok(reveal.pending);
 const counts=frames(reveal,0,500).map(text=>Array.from(new Intl.Segmenter(undefined,{granularity:'grapheme'}).segment(text)).length);
 for(let i=1;i<counts.length;i++)assert.ok(counts[i]>=counts[i-1]&&counts[i]-counts[i-1]<=2);
});
