import { test } from 'node:test';
import assert from 'node:assert/strict';
import { botMenuPlugins, nativeMenuPlugins, attachmentMenuDescription } from '../frontend/src/attachment-menu-model.ts';
import { attachmentMenuLayout } from '../frontend/src/attachment-menu-layout.ts';

test('menu lists enabled packages and native plugins as packages, not their individual skills',()=>{
 const bot=botMenuPlugins([
  {id:'notion',title:'Notion',description:'Notes and tasks',installed:true,enabled:true,status:'enabled'},
  {id:'old',title:'Old',description:'',installed:true,enabled:false,status:'disabled'},
  {id:'broken',title:'Broken',description:'',installed:true,enabled:true,status:'failed'},
 ]);
 const native=nativeMenuPlugins([
  {id:'native-notion',name:'Notion',description:'Native',source:'codex'},
  {id:'native-notion',name:'Notion',description:'Duplicate',source:'codex'},
  {id:'other',name:'Other',description:'',source:'caelis'},
 ]);
 assert.deepEqual(bot.map(item=>item.id),['bot-plugin:notion']);
 assert.deepEqual(native.map(item=>item.id),['codex-plugin:native-notion']);
 assert.equal(bot[0].source,'bot');
});

test('popover matches the composer width and never overlaps the input',()=>{
 const anchor={left:40,top:568,bottom:630,width:560};
 const menu=attachmentMenuLayout(anchor,{width:640,height:700},360);
 assert.equal(menu.width,anchor.width);
 assert.equal(menu.height,360);
 assert.ok(menu.top+menu.height<anchor.top);
 assert.equal(menu.left,anchor.left);
 const narrow=attachmentMenuLayout({...anchor,left:15,top:90,bottom:152,width:270},{width:300,height:300},360);
 assert.ok(narrow.left>=12 && narrow.left+narrow.width<=288);
 assert.ok(narrow.height<=136);
 assert.ok(narrow.top>=12);
 const fileOnly=attachmentMenuLayout(anchor,{width:640,height:700},48);
 assert.equal(fileOnly.width,anchor.width);
 assert.ok(fileOnly.top+fileOnly.height<anchor.top);
});

test('short descriptions remain a single useful preview',()=>{
 assert.equal(attachmentMenuDescription('First sentence. Second sentence.'),'First sentence.');
 assert.ok(attachmentMenuDescription('Long '.repeat(30)).length<=72);
});
