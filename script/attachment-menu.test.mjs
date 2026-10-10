import { test } from 'node:test';
import assert from 'node:assert/strict';
import { attachmentMenuReferences, attachmentMenuDescription } from '../frontend/src/attachment-menu-model.ts';
import { attachmentMenuLayout } from '../frontend/src/attachment-menu-layout.ts';

test('menu hides resident core and deduplicates identical skill sources without changing references',()=>{
 const references=[
  {id:'core',name:'bot-core',description:'Resident guide',kind:'skill'},
  {id:'a',name:'acpx',description:'Use acpx as a CLI.',kind:'skill'},
  {id:'b',name:'acpx',description:'Use acpx as a CLI.',kind:'skill'},
  {id:'c',name:'acpx',description:'Plugin workflow.',kind:'plugin'},
 ];
 const menu=attachmentMenuReferences(references);
 assert.deepEqual(menu.skills.map(item=>item.id),['a']);
 assert.deepEqual(menu.plugins.map(item=>item.id),['c']);
 assert.equal(references.length,4);
});

test('popover stays above the plus button, bounded and scrollable in a chat viewport',()=>{
 const anchor={left:80,top:568,bottom:600,width:32};
 const menu=attachmentMenuLayout(anchor,{width:640,height:700},360);
 assert.equal(menu.width,344);
 assert.equal(menu.height,360);
 assert.ok(menu.top+menu.height<anchor.top);
 assert.ok(menu.left>=12 && menu.left+menu.width<=628);
 const narrow=attachmentMenuLayout({...anchor,left:15,top:90,bottom:122},{width:300,height:300},360);
 assert.ok(narrow.left>=12 && narrow.left+narrow.width<=288);
 assert.ok(narrow.height<=166);
 assert.ok(narrow.top>=12);
});

test('short descriptions remain a single useful preview',()=>{
 assert.equal(attachmentMenuDescription('First sentence. Second sentence.'),'First sentence.');
 assert.ok(attachmentMenuDescription('Long '.repeat(30)).length<=72);
});
