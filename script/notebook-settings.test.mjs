import assert from 'node:assert/strict';
import {test,after,afterEach} from 'node:test';
import {createServer} from 'vite';
import {JSDOM} from 'jsdom';
const dom=new JSDOM('<!doctype html><html><body></body></html>');
for(const name of ['window','document','HTMLElement','Event','MouseEvent','Node'])globalThis[name]=dom.window[name];
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const React=await import('react');
const {createRoot}=await import('react-dom/client');
const {act}=React;
const server=await createServer({configLoader:'runner',cacheDir:'.cache/notebook-test-vite',server:{middlewareMode:true,ws:false},appType:'custom'});
const {NotebookSyncSettings}=await server.ssrLoadModule('/src/settings/runtime/NotebookSyncSettings.tsx');
let root,container;
afterEach(async()=>{if(root)await act(async()=>root.unmount());container?.remove();root=null;});
after(async()=>{await server.close();dom.window.close();});
const catalog={revision:'1',activeBotNodeId:'local',nodes:[{id:'local',label:'This machine',join:'local',runtimes:[{backend:'codex'}]},{id:'backup',label:'Backup machine',join:'ssh',runtimes:[{backend:'codex'}]},{id:'outbound',label:'Outgoing only',join:'outgoing',runtimes:[{backend:'codex'}]}]};
async function mount({phase='ready',fail=false,remote=false,switchFailureState}={}) {
 let preferences={enabled:true,sourceNodeId:'local',intervalMinutes:5,targets:[{nodeId:'backup',backend:'codex'}]};
 let state={sourceNodeId:'local',targets:[{nodeId:'backup',phase,operationId:'previous-operation',lastSuccess:'2026-10-01T10:00:00Z'}]};
 if(remote){preferences.sourceNodeId='backup';preferences.targets=[{nodeId:'local',backend:'codex'}];state.sourceNodeId='backup';state.targets[0].nodeId='local';}
 const calls=[];
 const call=async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NotebookSyncSettings')return structuredClone(preferences);
  if(method==='NotebookSyncState')return structuredClone(state);
  if(method==='SaveNotebookSyncSettings'){preferences=args[0];return preferences;}
  if(method==='SyncNotebook'){state.targets[0].error='transfer-failed';if(fail)throw Error('synthetic');return state;}
  if(method==='SwitchNotebookNode'){if(switchFailureState){state={...state,...switchFailureState};throw Error('PRIVATE_NATIVE_FAILURE');}state.targets[0].phase=fail?'starting':remote?'restart-required':'switched';if(fail)throw Error('synthetic');return state;}
  throw Error('unexpected method');
 };
 container=document.createElement('div');document.body.append(container);root=createRoot(container);
 await act(async()=>root.render(React.createElement(NotebookSyncSettings,{catalog:remote?{...catalog,activeBotNodeId:'backup'}:catalog,call,host:async method=>{calls.push([method]);}})));
 return {calls};
}
const button=name=>[...container.querySelectorAll('button')].find(value=>value.textContent===name);
const click=async(element)=>{assert.ok(element);await act(async()=>element.click());};
test('backup surface resolves labels, successful time and ordinary manual copy without exposing paths',async()=>{
 const {calls}=await mount({fail:true});
 assert.match(container.textContent,/Backup machine/);assert.match(container.textContent,/Last successful backup/);assert.doesNotMatch(container.textContent,/Outgoing only|notebook-bot|\.auth|Session ID/);
 await click(button('Back up now'));
 assert.ok(calls.some(([method,node])=>method==='SyncNotebook'&&node==='backup'));
 assert.match(container.textContent,/Last successful backup/);assert.match(container.textContent,/latest backup failed/);
});
test('manual switch uses the existing node identity and locks uncertain outcome',async()=>{
 const {calls}=await mount({fail:true});
 await click(button('Switch to this node'));
 const dialog=container.querySelector('[role="dialog"]');assert.ok(dialog);assert.match(dialog.textContent,/Stop the current Bot/);
 await click([...dialog.querySelectorAll('button')].find(value=>value.textContent==='Switch to this node'));
 assert.ok(calls.some(([method,node])=>method==='SwitchNotebookNode'&&node==='backup'));
 assert.match(container.textContent,/switch is unconfirmed/);
 assert.match(dialog.querySelector('[role="alert"]').textContent,/switch is unconfirmed/);
 assert.equal([...dialog.querySelectorAll('button')].find(value=>value.textContent==='Switch to this node').disabled,true);
 assert.equal(button('Back up now').disabled,true);
 assert.equal(button('Save').disabled,true);
});
test('a fresh exact ready operation explains the pre-stop refusal inside the confirmation',async()=>{
 const {calls}=await mount({switchFailureState:{targets:[{nodeId:'backup',phase:'ready',operationId:'new-operation',error:'PRIVATE_BUSY'}]}});
 await click(button('Switch to this node'));
 const dialog=container.querySelector('[role="dialog"]'),confirm=[...dialog.querySelectorAll('button')].find(value=>value.textContent==='Switch to this node');
 await click(confirm);
 assert.match(dialog.querySelector('[role="alert"]').textContent,/blocked before stopping the Bot/);
 assert.doesNotMatch(dialog.textContent,/unconfirmed|PRIVATE_/);
 assert.equal(confirm.disabled,false);assert.equal(button('Back up now').disabled,false);
 assert.equal(calls.filter(([method])=>method==='SwitchNotebookNode').length,1);
 assert.equal(calls.some(([method])=>method==='RestartForRuntime'),false);
 await click([...dialog.querySelectorAll('button')].find(value=>value.textContent==='Cancel'));
 assert.equal(container.querySelector('[role="dialog"]'),null);
});
test('stale ready or another owner/target cannot classify a failed switch as safe to retry',async()=>{
 for(const next of [
  {targets:[{nodeId:'backup',phase:'ready',operationId:'previous-operation'}]},
  {targets:[{nodeId:'backup',phase:'ready'}]},
  {sourceNodeId:'other-source',targets:[{nodeId:'backup',phase:'ready',operationId:'new-operation'}]},
  {targets:[{nodeId:'other-target',phase:'ready',operationId:'new-operation'}]},
 ]){
  if(root){await act(async()=>root.unmount());container.remove();}
  const {calls}=await mount({switchFailureState:next});await click(button('Switch to this node'));
  const dialog=container.querySelector('[role="dialog"]'),confirm=[...dialog.querySelectorAll('button')].find(value=>value.textContent==='Switch to this node');await click(confirm);
  assert.match(dialog.querySelector('[role="alert"]').textContent,/switch is unconfirmed/);assert.equal(confirm.disabled,true);assert.equal(button('Back up now')?.disabled??true,true);
  await click(confirm);assert.equal(calls.filter(([method])=>method==='SwitchNotebookNode').length,1);
 }
});
test('already persisted non-ready intent disables new dispatch on reopening settings',async()=>{
 const {calls}=await mount({phase:'stopped'});
 assert.equal(button('Back up now').disabled,true);
 assert.equal(button('Switch to this node').disabled,true);
 assert.equal(calls.some(([method])=>method==='SwitchNotebookNode'),false);
});

test('remote source exposes this machine return and restarts through the normal host entry point',async()=>{
 const {calls}=await mount({remote:true});
 assert.match(container.textContent,/This machine/);
 await click(button('Switch to this node'));
 const dialog=container.querySelector('[role="dialog"]');
 await click([...dialog.querySelectorAll('button')].find(value=>value.textContent==='Switch to this node'));
 assert.ok(calls.some(([method,node])=>method==='SwitchNotebookNode'&&node==='local'));
 assert.ok(calls.some(([method])=>method==='RestartForRuntime'));
 assert.match(container.textContent,/Backup direction follows the active Bot automatically/);
 assert.equal(calls.some(([method])=>method==='SaveNotebookSyncSettings'),false);
});
test('confirmed remote move invokes normal APP relaunch',async()=>{
 const {calls}=await mount();
 await click(button('Switch to this node'));
 const dialog=container.querySelector('[role="dialog"]');
 await click([...dialog.querySelectorAll('button')].find(value=>value.textContent==='Switch to this node'));
 assert.ok(calls.some(([method])=>method==='RestartForRuntime'));
});
