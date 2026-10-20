import test from 'node:test';
import assert from 'node:assert/strict';
import {canReturnToChat,settingsHasModal} from '../frontend/src/context-navigation.ts';

test('chat return is denied for setup, hidden windows, dialogs and dirty owners',()=>{
 for(const [setup,visible,modal,allowed] of [[true,true,false,true],[false,false,false,true],[false,true,true,true],[false,true,false,false]])assert.equal(canReturnToChat(setup,visible,modal,()=>allowed),false);
 assert.equal(canReturnToChat(false,true,false,()=>true),true);
});

test('hidden retained dialogs do not block navigation, while visible unsaved forms do',()=>{
 const root=(visible)=>({querySelectorAll:selector=>{
  assert.match(selector,/data-settings-dirty/);
  return visible.map(shown=>({getClientRects:()=>shown?[{}]:[]}));
 }});
 assert.equal(settingsHasModal(root([false,false])),false);
 assert.equal(settingsHasModal(root([false,true])),true);
});
