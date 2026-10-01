import assert from 'node:assert/strict';
import {test,after,afterEach} from 'node:test';
import {createServer} from 'vite';
import {JSDOM} from 'jsdom';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';

// React 19's documented act()+createRoot path, with jsdom only as a DOM.
// https://react.dev/reference/react/act
// Pinned jsdom26.1.0 engines >=18: https://registry.npmjs.org/jsdom/26.1.0
// No browser layout, native GUI, native Edit-menu or visual acceptance is claimed.
const dom=new JSDOM('<!doctype html><html><body></body></html>');
for(const name of ['window','document','HTMLElement','Event','MouseEvent','KeyboardEvent','Node'])globalThis[name]=dom.window[name];
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const React=await import('react');
const {createRoot}=await import('react-dom/client');
const {act}=React;
const server=await createServer({server:{middlewareMode:true,ws:false},appType:'custom'});
const {NodeRuntimeSettings}=await server.ssrLoadModule('/src/NodeRuntimeSettings.tsx');
const {createNodeSettingsClient,managementDigestInput}=await server.ssrLoadModule('/src/settings/runtime/nodeClient.ts');
let root,container;
afterEach(async()=>{if(root)await act(async()=>root.unmount());container?.remove();root=null;});
after(async()=>{await server.close();dom.window.close();});
const runtime=(backend='caelis')=>({backend,version:'1.0',health:'healthy',authentication:'authenticated',roles:[{role:'bot',eligible:true,reason:''},{role:'worker',eligible:true,reason:''}]});
const catalog=()=>({revision:'catalog-1',selectedNodeId:'local',activeBotNodeId:'other',workerTarget:{nodeId:'local',backend:'codex',role:'worker'},broker:null,pendingOperations:[],nodes:[{id:'local',label:'Local',os:'darwin',join:'local',runtimes:[runtime(),runtime('codex')]},{id:'other',label:'Other',os:'linux',join:'outgoing',runtimes:[runtime()]}]});
const model=(id)=>({model:id,name:id,description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[]});
const config=(nodeId,backend)=>({guard:{nodeId,backend,revision:`guard-${nodeId}-${backend}`},configurationAvailable:true,installerAvailable:true,reviewedVersions:['1.0','2.0'],conversation:null,worker:null,configuration:{revision:`config-${nodeId}-${backend}`,main:{model:`${nodeId}-${backend}-a`,effort:'high',serviceTier:''},models:[model(`${nodeId}-${backend}-a`),model(`${nodeId}-${backend}-b`)],connections:[],team:{available:false,reason:'',revision:'team',roles:[],sets:[],activeSet:'',models:[]},oauthAvailable:false}});
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject};};
const click=async(el)=>{assert.ok(el);await act(async()=>el.dispatchEvent(new MouseEvent('click',{bubbles:true})));};
const choose=async(el,value)=>{assert.ok(el);await act(async()=>{el.value=value;el.dispatchEvent(new Event('change',{bubbles:true}));});};
const buttons=(name)=>[...container.querySelectorAll('button')].filter(el=>el.textContent.trim()===name);
const select=(name)=>container.querySelector(`select[aria-label="${name}"]`);
async function mount(invoke) {
 container=document.createElement('div');document.body.append(container);root=createRoot(container);
 const owner=createNodeSettingsClient(invoke);
 await act(async()=>root.render(React.createElement(NodeRuntimeSettings,{client:owner,call:async()=>({revision:1,nodes:[],issue:''})})));
 return owner;
}
const defaultInvoke=async(method,...args)=>method==='NodeCatalog'?catalog():config(...args);

test('management digest agrees with Go Unicode vector and has unambiguous UTF8 lengths',()=>{
 const guard={nodeId:'local',backend:'caelis',revision:'7'};
 const change={action:'create-role',id:'reviewer',name:'',description:'审查<&>🌍',selection:{model:'',effort:'',serviceTier:''},expectedRevision:'7'};
 assert.equal(createHash('sha256').update(managementDigestInput(guard,{change,installation:null})).digest('hex'),'610b927a402fc536e2c071c988446f8433c704e61a29cab86bbd4c10daaeeb20');
 assert.throws(()=>managementDigestInput(guard,{change,installation:{action:'detect',version:'',expectedVersion:''}}));
});
test('unknown response deduplicates clicks and reconciles the exact original reference only',async()=>{
 const calls=[],delivery=deferred(),dispatched=deferred();delivery.promise.catch(()=>{});
 const owner=createNodeSettingsClient(async(method,...args)=>{calls.push([method,...args]);if(method==='ChangeNodeConfiguration'){dispatched.resolve();return delivery.promise;}if(method==='ReconcileNodeOperation')return {ref:args[0],outcome:'committed',revision:'2',message:''};});
 const guard={nodeId:'n',backend:'codex',revision:'opaque'};
 const payload={change:null,installation:{action:'update',version:'1.2',expectedVersion:'1.1'}};
 const first=owner.change(guard,payload);first.catch(()=>{});
 await assert.rejects(owner.change(guard,payload),e=>e.unknown);
 await dispatched.promise;
 delivery.reject(new Error('lost'));
 await assert.rejects(first,e=>e.unknown);
 await assert.rejects(owner.change(guard,payload),e=>e.unknown);
 const original=calls[0][1].ref;
 assert.deepEqual(owner.pending('n','codex'),original);
 await owner.reconcile('n','codex');
 assert.deepEqual(calls[1],['ReconcileNodeOperation',original]);
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,1);
 assert.equal(owner.pending('n','codex'),undefined);
});
test('node/backend switches preserve Bot owner and exact Worker; no activation is dispatched',async()=>{
 const calls=[];await mount(async(method,...args)=>{calls.push([method,...args]);return defaultInvoke(method,...args);});
 assert.equal(select('Node').value,'local');
 assert.match(container.textContent,/Bot running on: Other/);assert.match(container.textContent,/Worker: Local · Codex/);
 await choose(select('Execution backend'),'codex');
 assert.ok(calls.some(call=>call[0]==='NodeRuntimeConfiguration'&&call[1]==='local'&&call[2]==='codex'));
 await choose(select('Node'),'other');
 assert.equal(select('Node').value,'other');assert.match(container.textContent,/Bot running on: Other/);
 assert.ok(calls.every(call=>!['ActivateRuntime','SelectWorkTarget','RestartForRuntime','SelectNode'].includes(call[0])));
});
test('open model draft blocks node/backend change; cancel preserves keyboard focus',async()=>{
 await mount(defaultInvoke);
 const picker=container.querySelector('button.runtime-model-summary');picker.focus();
 await click(picker);
 const dialog=container.querySelector('[role="dialog"]');assert.ok(dialog);assert.equal(document.activeElement,dialog);
 const radio=dialog.querySelectorAll('input[type="radio"]')[1];await click(radio);assert.equal(radio.checked,true);
 await choose(select('Node'),'other');assert.equal(select('Node').value,'local');assert.ok(container.contains(dialog));assert.equal(radio.checked,true);
 await choose(select('Execution backend'),'codex');assert.equal(select('Execution backend').value,'caelis');
 assert.match(container.textContent,/Finish or cancel/);
 await act(async()=>dialog.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})));
 assert.equal(container.querySelector('[role="dialog"]'),null);assert.equal(document.activeElement,picker);
 await choose(select('Node'),'other');assert.equal(select('Node').value,'other');
});
test('reordered configuration reads cannot populate another selected node',async()=>{
 const old=deferred();await mount(async(method,...args)=>method==='NodeCatalog'?catalog():args[0]==='local'?old.promise:config(...args));
 await choose(select('Node'),'other');assert.match(container.textContent,/other-caelis-a/);
 await act(async()=>old.resolve(config('local','caelis')));
 assert.doesNotMatch(container.textContent,/local-caelis-a/);assert.match(container.textContent,/other-caelis-a/);
});
test('offline refresh retains committed model and disables edits; reconnect restores only confirmed state',async()=>{
 let online=true;await mount(async(method,...args)=>{if(method==='NodeCatalog'){const value=catalog();value.nodes[0].runtimes[0].health=online?'healthy':'unavailable';return value;}if(!online)throw new Error('offline');return config(...args);});
 assert.match(container.textContent,/local-caelis-a/);online=false;
 await click(buttons('Refresh configuration').at(-1));
 assert.match(container.textContent,/local-caelis-a/);assert.equal(container.querySelector('button.runtime-model-summary').disabled,true);assert.match(container.textContent,/not currently reachable/);
 online=true;await click(buttons('Refresh configuration').at(-1));
 assert.equal(container.querySelector('button.runtime-model-summary').disabled,false);assert.match(container.textContent,/local-caelis-a/);
});
test('save uses inspected node guard and config revision; repeated DOM clicks produce one operation',async()=>{
 const write=deferred(),calls=[];await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='ChangeNodeConfiguration')return write.promise;return defaultInvoke(method,...args);});
 await click(container.querySelector('button.runtime-model-summary'));
 await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);
 const save=buttons('Save')[0];
 await act(async()=>{save.dispatchEvent(new MouseEvent('click',{bubbles:true}));save.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 await act(async()=>new Promise(resolve=>setImmediate(resolve)));
 const command=calls.find(call=>call[0]==='ChangeNodeConfiguration')[1];
 assert.deepEqual(command.guard,{nodeId:'local',backend:'caelis',revision:'guard-local-caelis'});
 assert.equal(command.change.expectedRevision,'config-local-caelis');
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,1);
 await act(async()=>write.resolve({ref:command.ref,outcome:'rejected',revision:'',message:'Native refused'}));
 assert.match(container.textContent,/Native refused/);assert.ok(container.querySelector('[role="dialog"]'));
});
test('small window source uses wrapping bounded native selectors, without dashboard navigation',()=>{
 const css=readFileSync('frontend/src/settings/runtime/runtime.css','utf8');
 assert.match(css,/node-settings-selectors[^}]+flex-wrap:wrap/);assert.match(css,/@media\(max-width:560px\)[^\n]+node-settings-selectors select[^}]+min-width:0/);
});

test('native form draft blocks node switch; uncertain install exposes original receipt and does not resend',async()=>{
 const calls=[];await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='ChangeNodeConfiguration')throw new Error('delivery lost');if(method==='ReconcileNodeOperation')return {ref:args[0],outcome:'committed',revision:'new',message:''};return defaultInvoke(method,...args);});
 const version=container.querySelector('#node-program-version');
 await choose(version,'2.0');
 await choose(select('Node'),'other');assert.equal(select('Node').value,'local');assert.equal(version.value,'2.0');
 await click(buttons('Update')[0]);
 await click(buttons('Confirm')[0]);
 assert.match(container.textContent,/program change was not confirmed/);
 const command=calls.find(call=>call[0]==='ChangeNodeConfiguration')[1];assert.equal(command.installation.expectedVersion,'1.0');assert.equal(command.ref.nodeId,'local');
 assert.equal(buttons('Confirm')[0].disabled,true);
 await click([...container.querySelector('[role="dialog"]').querySelectorAll('button')].find(el=>el.textContent==='Cancel'));
 await click(buttons('Check original receipt')[0]);
 assert.deepEqual(calls.find(call=>call[0]==='ReconcileNodeOperation')[1],command.ref);
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,1);
});
test('failed detection after node selection cannot replace the new node health or display its error',async()=>{
 const detection=deferred();const owner=await mount(async(method,...args)=>defaultInvoke(method,...args));
 // Exercise the actual child with a closed native facade, never real SSH.
 await act(async()=>root.render(React.createElement(NodeRuntimeSettings,{client:owner,call:async(method)=>method==='DetectNode'?detection.promise:({})})));
 await click(buttons('Re-detect')[0]);await choose(select('Node'),'other');
 await act(async()=>detection.reject(new Error('old node failed')));
 assert.equal(select('Node').value,'other');assert.doesNotMatch(container.textContent,/old node failed|node list could not be refreshed/);
});
test('dialog Tab wraps within enabled controls and Escape returns focus to its opener',async()=>{
 await mount(defaultInvoke);const picker=container.querySelector('button.runtime-model-summary');picker.focus();await click(picker);
 const dialog=container.querySelector('[role="dialog"]');
 // jsdom has no layout. This marks test controls as visible for the existing
 // focus algorithm; it does not assert screen geometry or native GUI layout.
 for(const el of dialog.querySelectorAll('button,input,select'))el.getClientRects=()=>[{width:1,height:1}];
 const enabled=[...dialog.querySelectorAll('button:not(:disabled),input:not(:disabled),select:not(:disabled)')];
 await act(async()=>dialog.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true})));
 assert.equal(document.activeElement,enabled[0]);
 enabled[0].focus();await act(async()=>enabled[0].dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,bubbles:true,cancelable:true})));
 assert.equal(document.activeElement,enabled.at(-1));
 await act(async()=>dialog.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})));
 assert.equal(document.activeElement,picker);
});

test('remounted owner recovers native pending refs and ignores a stale catalog after original receipt resolution',async()=>{
 const original={nodeId:'local',backend:'caelis',operationId:'persisted-operation',requestDigest:'f'.repeat(64)};
 const calls=[];const owner=await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return {...catalog(),pendingOperations:[original]};if(method==='ReconcileNodeOperation')return {ref:args[0],outcome:'committed',revision:'2',message:''};return defaultInvoke(method,...args);});
 assert.equal(owner.pending('local','caelis').operationId,original.operationId);assert.ok(buttons('Check original receipt')[0]);
 assert.equal(container.querySelector('button.runtime-model-summary').disabled,true);
 await click(buttons('Check original receipt')[0]);
 assert.deepEqual(calls.find(call=>call[0]==='ReconcileNodeOperation')[1],original);
 assert.equal(owner.pending('local','caelis'),undefined);
 assert.equal(buttons('Check original receipt').length,0);
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,0);
});
test('missing installer and configuration capability keep last native facts and cannot submit installation',async()=>{
 const calls=[];await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return catalog();return {...config(...args),configurationAvailable:false,installerAvailable:false,reviewedVersions:[]};});
 assert.match(container.textContent,/Program management is not available/);
 assert.equal(container.querySelector('#node-program-version').disabled,true);assert.equal(buttons('Update')[0].disabled,true);
 await click(buttons('Update')[0]);assert.equal(container.querySelector('[role="dialog"]'),null);
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,0);
});

test('background configuration refresh cannot advance a draft captured native guard or expected revision',async()=>{
 let revision=1;const calls=[];
 const invoke=async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return {...catalog(),revision:`catalog-${revision}`};if(method==='ChangeNodeConfiguration')return {ref:args[0].ref,outcome:'conflicted',revision:'2',message:'Draft conflict'};const value=config(...args);value.guard.revision=`guard-${revision}`;value.configuration.revision=`config-${revision}`;return value;};
 const owner=await mount(invoke);
 await click(container.querySelector('button.runtime-model-summary'));
 await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);
 revision=2;
 await act(async()=>root.render(React.createElement(NodeRuntimeSettings,{client:owner,call:async()=>({}),refreshKey:1})));
 assert.ok(container.querySelector('[role="dialog"]'));
 await click(buttons('Save')[0]);
 const command=calls.find(call=>call[0]==='ChangeNodeConfiguration')[1];
 assert.equal(command.guard.revision,'guard-1');assert.equal(command.change.expectedRevision,'config-1');assert.match(container.textContent,/Draft conflict/);
});

test('roaming availability needs both reachable coordinator and an eligible native Bot runtime',async()=>{
 await mount(async(method,...args)=>{if(method==='NodeCatalog'){const value=catalog();value.broker={nodeId:'other',reachable:true,automaticRoaming:true,reason:''};for(const node of value.nodes)for(const runtime of node.runtimes)runtime.roles=runtime.roles.map(role=>({...role,eligible:role.role==='worker'}));return value;}return config(...args);});
 assert.match(container.textContent,/Automatic roaming is unavailable/);
 assert.doesNotMatch(container.textContent,/Connected · automatic roaming available/);
});
