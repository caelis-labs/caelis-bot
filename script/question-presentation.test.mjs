import {test} from 'node:test';
import assert from 'node:assert/strict';
import {questionStatusKey,questionCopy,optionWords} from '../frontend/src/question-presentation.ts';
test('closed states have distinct labels instead of unknown delivery',()=>{
 for(const [state,key]of [['answered','questionAnswered'],['dismissed','questionDismissed'],['expired','questionExpired'],['superseded','questionSuperseded'],['unknown','questionUnknown'],['dispatching','questionUnknown']])assert.equal(questionStatusKey(state),'chat.'+key);
});

test('legacy question typography preserves every character and Chinese words',()=>{
 const original='新版 Bot 视频中的真实对话需要可用的演示模型连接。你希望如何提供？会先继续准备素材和录制无需登录的流程，无需在这里发送密码或 API Key。';
 const copy=questionCopy(original);
 assert.equal(copy.title+copy.context,original);
 for(const option of ['我在隔离演示 Bot 中手动登录或配置模型','使用已有独立演示 Runtime，我提供位置','暂不配置，先完成无需登录的片段']) {
  const words=optionWords(option,'zh-CN');assert.equal(words.join(''),option);
  if(option.endsWith('模型'))assert.equal(words.at(-1),'模型');
  if(option.endsWith('位置'))assert.equal(words.at(-1),'位置');
 }
});

import {ChatScroll} from '../frontend/src/chat-scroll.ts';
test('pending question keeps first source and close at reading start after layout',()=>{
 const viewport={scrollTop:0,scrollHeight:1000,clientHeight:400};
 const scroll=new ChatScroll(viewport,()=>200);
 scroll.latest(true);assert.equal(viewport.scrollTop,200);
 viewport.clientHeight=300;scroll.layout();assert.equal(viewport.scrollTop,200);
 viewport.scrollTop=280;assert.equal(scroll.scrolled(),false);
 scroll.layout();assert.equal(viewport.scrollTop,280);
 scroll.latest();assert.equal(viewport.scrollTop,700);
 const normal=new ChatScroll(viewport);normal.latest();assert.equal(viewport.scrollTop,700);
});
