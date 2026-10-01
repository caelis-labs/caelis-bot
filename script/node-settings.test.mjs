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
const {createNodeSettingsClient,createNodeRuntimeClient,managementDigestInput}=await server.ssrLoadModule('/src/settings/runtime/nodeClient.ts');
const {I18nProvider}=await server.ssrLoadModule('/src/i18n/index.tsx');
const {translator}=await server.ssrLoadModule('/src/i18n/core.ts');
let root,container;
afterEach(async()=>{if(root)await act(async()=>root.unmount());container?.remove();root=null;});
after(async()=>{await server.close();dom.window.close();});
const runtime=(backend='caelis')=>({backend,version:'1.0',health:'healthy',authentication:'authenticated',roles:[{role:'bot',eligible:true,reason:''},{role:'worker',eligible:true,reason:''}]});
const catalog=()=>({revision:'catalog-1',selectedNodeId:'local',activeBotNodeId:'other',workerTarget:{nodeId:'local',backend:'codex',role:'worker'},broker:null,pendingOperations:[],nodes:[{id:'local',label:'Local',os:'darwin',join:'local',runtimes:[runtime(),runtime('codex')]},{id:'other',label:'Other',os:'linux',join:'outgoing',runtimes:[runtime()]}]});
const model=(id)=>({model:id,name:id,description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[]});
const config=(nodeId,backend)=>({guard:{nodeId,backend,revision:`guard-${nodeId}-${backend}`},configurationAvailable:true,installerAvailable:true,installation:{installed:true,version:'1.0',latestVersion:'2.0'},reviewedVersions:['1.0','2.0'],conversation:null,worker:null,configuration:{revision:`config-${nodeId}-${backend}`,main:{model:`${nodeId}-${backend}-a`,effort:'high',serviceTier:''},models:[model(`${nodeId}-${backend}-a`),model(`${nodeId}-${backend}-b`)],connections:[],team:{available:false,reason:'',revision:'team',roles:[],sets:[],activeSet:'',models:[]},oauthAvailable:false}});
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject};};
const click=async(el)=>{assert.ok(el);await act(async()=>el.dispatchEvent(new MouseEvent('click',{bubbles:true})));};
const choose=async(el,value)=>{assert.ok(el);await act(async()=>{el.value=value;el.dispatchEvent(new Event('change',{bubbles:true}));});};
const buttons=(name)=>[...container.querySelectorAll('button')].filter(el=>el.textContent.trim()===name);
const select=(name)=>container.querySelector(`select[aria-label="${name}"]`);
async function mount(invoke,call=async()=>({revision:1,nodes:[],issue:''}),strict=false,locale) {
 container=document.createElement('div');document.body.append(container);root=createRoot(container);
 const owner=createNodeSettingsClient(invoke,locale?translator(locale).t:undefined);
 await act(async()=>{let page=React.createElement(NodeRuntimeSettings,{client:owner,call});if(locale)page=React.createElement(I18nProvider,{bridge:{read:async()=>({preference:locale,locale,revision:1}),subscribe:()=>()=>{}}},page);root.render(strict?React.createElement(React.StrictMode,null,page):page);});
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
 const write=deferred(),dispatched=deferred(),calls=[];await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='ChangeNodeConfiguration'){dispatched.resolve();return write.promise;}return defaultInvoke(method,...args);});
 await click(container.querySelector('button.runtime-model-summary'));
 await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);
 const save=buttons('Save')[0];
 await act(async()=>{save.dispatchEvent(new MouseEvent('click',{bubbles:true}));save.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 await act(async()=>dispatched.promise);
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
 const calls=[],dispatched=deferred(),delivery=deferred();await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='ChangeNodeConfiguration'){dispatched.resolve();return delivery.promise;}if(method==='ReconcileNodeOperation')return {ref:args[0],outcome:'committed',revision:'new',message:''};return defaultInvoke(method,...args);});
 const version=container.querySelector('#node-program-version');
 await choose(version,'2.0');
 await choose(select('Node'),'other');assert.equal(select('Node').value,'local');assert.equal(version.value,'2.0');
 await click(buttons('Update')[0]);
 const dialog=container.querySelector('[role="dialog"]');assert.ok(dialog);
 const confirm=[...dialog.querySelectorAll('button')].filter(button=>button.textContent.trim()==='Confirm');assert.equal(confirm.length,1);
 await click(confirm[0]);
 await act(async()=>{await dispatched.promise;delivery.reject(new Error('delivery lost'));});
 assert.match(container.textContent,/program change was not confirmed/);
 const command=calls.find(call=>call[0]==='ChangeNodeConfiguration')[1];assert.equal(command.installation.expectedVersion,'1.0');assert.equal(command.ref.nodeId,'local');
 assert.equal(confirm[0].disabled,true);
 await click([...dialog.querySelectorAll('button')].find(el=>el.textContent==='Cancel'));
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
 let revision=1;const calls=[],dispatched=deferred(),delivery=deferred();
 const invoke=async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return {...catalog(),revision:`catalog-${revision}`};if(method==='ChangeNodeConfiguration'){dispatched.resolve(args[0]);return delivery.promise;}const value=config(...args);value.guard.revision=`guard-${revision}`;value.configuration.revision=`config-${revision}`;return value;};
 const owner=await mount(invoke);
 await click(container.querySelector('button.runtime-model-summary'));
 await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);
 revision=2;
 await act(async()=>root.render(React.createElement(NodeRuntimeSettings,{client:owner,call:async()=>({}),refreshKey:1})));
 assert.ok(container.querySelector('[role="dialog"]'));
 await click(buttons('Save')[0]);
 await act(async()=>{const command=await dispatched.promise;delivery.resolve({ref:command.ref,outcome:'conflicted',revision:'2',message:'Draft conflict'});});
 const command=calls.find(call=>call[0]==='ChangeNodeConfiguration')[1];
 assert.equal(command.guard.revision,'guard-1');assert.equal(command.change.expectedRevision,'config-1');assert.match(container.textContent,/Draft conflict/);
});

test('catalog capability alone cannot advertise roaming without a native controller',async()=>{
 await mount(async(method,...args)=>{if(method==='NodeCatalog'){const value=catalog();value.broker={nodeId:'other',reachable:true,automaticRoaming:true,reason:''};for(const node of value.nodes)for(const runtime of node.runtimes)runtime.roles=runtime.roles.map(role=>({...role,eligible:role.role==='worker'}));return value;}return config(...args);});
 assert.match(container.textContent,/Automatic roaming is unavailable/);
 assert.doesNotMatch(container.textContent,/Connected · automatic roaming available/);
});

test('paired product invoker retains original unknown id and refuses another remote mutation',async()=>{
 const {createPairedRuntimeInvoker}=await server.ssrLoadModule('/src/settings/runtime/pairedClient.ts');
 const calls=[];const state={binding:'paired-binding',available:true,capabilities:{execution:true},pending:[]};
 const invoke=createPairedRuntimeInvoker(async(method,...args)=>{calls.push([method,...args]);if(method==='RemoteRuntime')return state;if(method==='ReconcileRemoteManagement')return {id:args[1],outcome:'accepted'};throw new Error('lost delivery');},'paired-binding',()=> 'Unknown');
 const original={id:'original',binding:'paired-binding',target:'work',expectedRevision:'5',selection:{model:'m',effort:'high'}};
 await assert.rejects(invoke('ChangeRemoteExecutionSettings',original),e=>e.unknown&&e.receipt.operationId==='original');
 await assert.rejects(invoke('ChangeRemoteExecutionSettings',{...original,id:'second'}),e=>e.unknown);
 assert.equal(calls.filter(call=>call[0]==='ChangeRemoteExecutionSettings').length,1);
 await invoke('ReconcileRemoteManagement','paired-binding','original');
 assert.deepEqual(calls.at(-1),['ReconcileRemoteManagement','paired-binding','original']);
 state.binding='different-binding';
 await assert.rejects(invoke('RemoteExecutionSettings','paired-binding'));
 assert.equal(calls.filter(call=>call[0]==='RemoteExecutionSettings').length,0);
});

function pairedFixture() {
 const calls=[];let executionRevision='paired-execution-1',pending=[];
 const fixtureCatalog=()=>({...catalog(),activeBotNodeId:'paired-node',pairedRuntime:{nodeId:'paired-node',binding:'paired-binding'},nodes:[catalog().nodes[0],{id:'paired-node',label:'Paired host',os:'unknown',join:'ssh',runtimes:[]}]});
 const remoteModels=[model('paired-a'),model('paired-b')];
 const remoteState=()=>({binding:'paired-binding',label:'Paired host',available:true,capabilities:{configuration:true,execution:true,installation:true},releases:[{runtime:'codex',version:'2.0'},{runtime:'caelis',version:'2.0'}],pending});
 const invoke=async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return fixtureCatalog();
  if(method==='NodeRuntimeConfiguration')return config(...args);
  if(method==='RemoteRuntime')return remoteState();
  if(method==='RemoteRuntimeConfiguration')return {...config('paired','caelis').configuration,main:{model:'paired-a',effort:'high',serviceTier:''},models:remoteModels};
  if(method==='RemoteExecutionSettings')return {binding:'paired-binding',revision:executionRevision,conversationDefault:false,conversation:{model:'paired-a',effort:'high'},work:{model:'paired-a',effort:'high'},models:remoteModels};
  if(method==='RemoteRuntimeStatus')return {installed:true,version:'1.0',latestVersion:'2.0'};
  if(method==='ChangeRemoteExecutionSettings'){executionRevision='paired-execution-2';return {id:args[0].id,outcome:'accepted'};}
  if(method==='ChangeRemoteRuntimeConfiguration')return {id:args[0].id,outcome:'accepted',configuration:{operationId:args[0].id,outcome:'committed',message:''}};
  if(method==='ManageRemoteRuntime')return {id:args[0].id,outcome:'accepted',status:{installed:true,version:'2.0',latestVersion:'2.0'}};
  if(method==='ReconcileRemoteManagement'){pending=[];return {id:args[1],outcome:'accepted'};}
  throw new Error(`Unexpected method ${method}`);
 };
 return {calls,invoke,fixtureCatalog,remoteState,setPending:value=>pending=value};
}
async function mountPaired(fixture) {
 const owner=createNodeSettingsClient(fixture.invoke);
 container=document.createElement('div');document.body.append(container);root=createRoot(container);
 await act(async()=>root.render(React.createElement(NodeRuntimeSettings,{client:owner,call:fixture.invoke})));
 return owner;
}
test('thin paired node preserves original remote models and installation, while Local never calls remote APIs',async()=>{
 const fixture=pairedFixture();await mountPaired(fixture);
 assert.equal(fixture.calls.filter(call=>call[0].includes('Remote')).length,0);
 await choose(select('Node'),'paired-node');
 assert.match(container.textContent,/Connections and models for the Bot paired with this app/);
 assert.match(container.textContent,/paired-a/);assert.equal(select('Execution backend'),null);
 const conversation=container.querySelector('button[aria-label="Configure Bot conversation model"]');
 await click(conversation);await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);await click(buttons('Save')[0]);
 const modelChange=fixture.calls.find(call=>call[0]==='ChangeRemoteExecutionSettings')[1];
 assert.equal(modelChange.binding,'paired-binding');assert.equal(modelChange.target,'conversation');assert.equal(modelChange.expectedRevision,'paired-execution-1');assert.equal(modelChange.selection.model,'paired-b');
 await click(buttons('Update')[0]);
 const confirm=buttons('Confirm')[0];await act(async()=>{confirm.dispatchEvent(new MouseEvent('click',{bubbles:true}));confirm.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 const install=fixture.calls.find(call=>call[0]==='ManageRemoteRuntime')[1];
 assert.equal(install.binding,'paired-binding');assert.equal(install.runtime,'codex');assert.equal(install.action,'update');assert.equal(install.expectedVersion,'1.0');
 assert.equal(fixture.calls.filter(call=>call[0]==='ManageRemoteRuntime').length,1);
 assert.equal(fixture.calls.filter(call=>call[0]==='NodeRuntimeConfiguration'&&call[1]==='paired-node').length,0);
 const count=fixture.calls.length;await choose(select('Node'),'local');
 assert.equal(select('Node').value,'local');assert.equal(fixture.calls.slice(count).filter(call=>call[0].includes('Remote')).length,0);
 assert.ok(fixture.calls.every(call=>!['ActivateRuntime','SelectWorkTarget','RestartForRuntime','SelectNode'].includes(call[0])));
});
test('paired model response loss renders original receipt and reconcile never sends a new model command',async()=>{
 const fixture=pairedFixture(),base=fixture.invoke;let original;
 fixture.invoke=async(method,...args)=>{if(method==='ChangeRemoteExecutionSettings'){fixture.calls.push([method,...args]);original=args[0];throw new Error('lost response');}return base(method,...args);};
 await mountPaired(fixture);await choose(select('Node'),'paired-node');
 await click(container.querySelector('button[aria-label="Configure Bot conversation model"]'));await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);await click(buttons('Save')[0]);
 assert.ok(buttons('Check original receipt')[0]);
 await click(buttons('Cancel')[0]);await click(buttons('Check original receipt')[0]);
 const lookup=fixture.calls.find(call=>call[0]==='ReconcileRemoteManagement');
 assert.deepEqual(lookup,['ReconcileRemoteManagement','paired-binding',original.id]);
 assert.equal(fixture.calls.filter(call=>call[0]==='ChangeRemoteExecutionSettings').length,1);
});

test('detected PATH runtime installs a separate managed copy with empty managed expected version',async()=>{
 const calls=[],dispatched=deferred(),delivery=deferred();await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog'){const value=catalog();value.nodes[0].runtimes[0].version='0.158.0';return value;}if(method==='ChangeNodeConfiguration'){dispatched.resolve(args[0]);return delivery.promise;}return {...config(...args),installation:{installed:false,version:'',latestVersion:'2.0'}};});
 assert.match(container.textContent,/Detected program0.158.0/);assert.match(container.textContent,/Managed programNot installed/);assert.match(container.textContent,/separate managed copy/);
 await choose(container.querySelector('#node-program-version'),'2.0');await click(buttons('Install managed copy')[0]);const dialog=container.querySelector('[role="dialog"]');await click([...dialog.querySelectorAll('button')].find(button=>button.textContent.trim()==='Confirm'));
 await act(async()=>{const command=await dispatched.promise;delivery.resolve({ref:command.ref,outcome:'committed',revision:'next',message:''});});
 const command=calls.find(call=>call[0]==='ChangeNodeConfiguration')[1];
 assert.deepEqual(command.installation,{action:'install',version:'2.0',expectedVersion:''});
});
test('missing managed installation status disables writes even when a PATH runtime and installer are detected',async()=>{
 const calls=[];await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return catalog();return {...config(...args),installerAvailable:true,installation:null};});
 assert.equal(container.querySelector('#node-program-version').disabled,true);assert.equal(buttons('Install managed copy')[0].disabled,true);
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,0);
});
test('closed role diagnostics render human capability explanations and never internal reason codes',async()=>{
 await mount(async(method,...args)=>{if(method==='NodeCatalog'){const value=catalog();value.nodes[0].runtimes[0].roles=[{role:'bot',eligible:false,reason:'shared-runtime-not-fenceable'},{role:'worker',eligible:false,reason:'runtime-owner-unavailable'}];return value;}return config(...args);});
 assert.match(container.textContent,/Bot cannot safely move between nodes/);assert.match(container.textContent,/Worker unavailable · Runtime not ready/);assert.doesNotMatch(container.textContent,/shared-runtime-not-fenceable|runtime-owner-unavailable/);
});

function localCodexStatusFixture({health='unavailable',authentication='required',state='models',accountType=''}={}){
 const profile={runtime:'codex',cliPath:'/usr/bin/false',caelisStore:''},calls=[];
 const value={health,authentication,state,accountType},inspection={pending:null};
 const invoke=async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return {revision:`catalog-${value.health}-${value.authentication}`,selectedNodeId:'local',activeBotNodeId:'local',workerTarget:{nodeId:'local',backend:'codex',role:'worker'},broker:null,pendingOperations:[],nodes:[{id:'local',label:'This machine',os:'darwin',join:'local',runtimes:[{...runtime('codex'),health:value.health,authentication:value.authentication,roles:['bot','worker'].map(role=>({role,eligible:value.health==='healthy',reason:value.health==='healthy'?'':'runtime-owner-unavailable'}))}]}]};
  if(method==='RuntimeSettings')return profile;
  if(method==='SetupOverview')return {active:'codex',pending:''};
  if(method==='InspectSetup')return inspection.pending??{settings:profile,state:value.state,message:'',installation:{installed:true,path:'',version:'0.158.0',latestVersion:'',updateState:'',message:''},models:[],selectedModel:'',accountType:value.accountType};
  if(['ExecutionSettings','WorkExecutionSettings'].includes(method))return null;
  if(method==='Models')return [];
  if(method==='NodeRuntimeConfiguration')return {...config(...args),configurationAvailable:false,conversation:null,worker:null,configuration:{revision:'',models:[],main:null,connections:[],team:null}};
  throw new Error(`Unexpected fixture method: ${method}`);
 };
 return {invoke,calls,value,inspection,profile};
}

for(const locale of ['en','zh-CN'])test(`fresh local unconfigured Codex uses truthful account and concise role status (${locale})`,async()=>{
 const fixture=localCodexStatusFixture(),t=translator(locale).t;
 await mount(fixture.invoke,undefined,false,locale);
 assert.equal(container.querySelector('.runtime-section-title h2').textContent,t('runtime.connectionsHeading'));
 assert.ok(buttons(t('runtime.connectAccount'))[0]);assert.equal(buttons(t('runtime.manageAccount')).length,0);
 assert.equal(buttons(t('runtime.connectAccount'))[0].disabled,false);
 assert.ok(container.textContent.includes(t('runtime.notConnected')));
 const roleRows=[...container.querySelectorAll('.runtime-active p')].filter(row=>row.textContent.includes(t('settings.nodeOwnerUnavailableReason')));
 assert.equal(roleRows.length,1);
 assert.ok(roleRows[0].textContent.includes(t('settings.nodeBotUnavailable')));
 assert.ok(roleRows[0].textContent.includes(t('settings.nodeWorkerUnavailable')));
 assert.ok(!container.textContent.includes(t('runtime.connectedViaChatGPT')));
 assert.ok(!container.textContent.includes(t('runtime.connectedViaApiKey')));
 assert.ok(fixture.calls.every(call=>!['BeginNodeRuntimeConnection','StartRuntimeConnection','StartNodeRuntimeConnection','ActivateRuntime','SaveExecutionSettings'].includes(call[0])));
});

test('unknown Node authentication never displays cached local account success',async()=>{
 const fixture=localCodexStatusFixture({health:'unknown',authentication:'unknown',state:'ready',accountType:'chatgpt'});
 await mount(fixture.invoke);
 assert.ok(buttons('Connect account')[0]);assert.equal(buttons('Manage account').length,0);
 assert.ok(container.textContent.includes(translator('en').t('runtime.connectionUnknown')));
 assert.ok(!container.textContent.includes(translator('en').t('runtime.connectedViaChatGPT')));
 assert.equal(container.querySelectorAll('button.runtime-model-summary').length,0);
});

test('disconnected Node suppresses cached login success while local inspection is still pending',async()=>{
 const fixture=localCodexStatusFixture({health:'healthy',authentication:'authenticated',state:'ready',accountType:'chatgpt'});
 await mount(fixture.invoke);
 assert.ok(buttons('Manage account')[0]);assert.ok(container.textContent.includes(translator('en').t('runtime.connectedViaChatGPT')));
 const pending=deferred();fixture.value.health='unavailable';fixture.value.authentication='required';fixture.inspection.pending=pending.promise;
 await click(container.querySelector('.node-runtime-settings > button.text-action'));
 assert.ok(buttons('Connect account')[0]);assert.equal(buttons('Manage account').length,0);
 assert.ok(!container.textContent.includes(translator('en').t('runtime.connectedViaChatGPT')));
 await act(async()=>pending.resolve({settings:fixture.profile,state:'ready',message:'',installation:{installed:true,path:'',version:'0.158.0',latestVersion:'',updateState:'',message:''},models:[],selectedModel:'',accountType:'chatgpt'}));
 assert.ok(buttons('Connect account')[0]);assert.equal(buttons('Manage account').length,0);
 assert.ok(!container.textContent.includes(translator('en').t('runtime.connectedViaChatGPT')));
});

test('alternate local Caelis preparation never reads a global profile; active local Codex retains ordinary preparation',async()=>{
 const calls=[],profile={runtime:'codex',cliPath:'/usr/bin/false',caelisStore:''};
 const setup={settings:profile,state:'models',message:'',installation:{installed:true,path:'',version:'0.158.0',latestVersion:'',updateState:'',message:''},models:[],selectedModel:'',accountType:'',serviceState:'',serviceVersion:'',serviceUpdateAvailable:false,loginPending:false};
 const invoke=async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return {...catalog(),activeBotNodeId:'local'};
  if(method==='RuntimeSettings')return profile;
  if(method==='NodeRuntimeConfiguration')return {...config(...args),configurationAvailable:false,configuration:{revision:'',models:[],main:null,connections:[],team:null}};
  if(method==='SetupOverview')return {active:'codex',pending:''};
  if(method==='InspectSetup')return setup;
  if(method==='ComposerSnapshot')return {connection:'unavailable'};
  if(method==='SetupProfile'){assert.equal(args[0],'codex');return profile;}
  throw new Error(`Unexpected fixture method: ${method}`);
 };
 await mount(invoke,invoke);
 assert.equal(select('Execution backend').value,'caelis');
 await click(buttons('Manage')[0]);
 assert.ok(container.querySelector('[role="dialog"]').textContent.includes(translator('en').t('settings.nodeScopedPreparation')));
 assert.equal(calls.filter(call=>['SetupProfile','InspectSetup','SetupOverview'].includes(call[0])).length,0);
 assert.equal(calls.filter(call=>/NodeRuntimeConnection/.test(call[0])).length,0);
 await click(container.querySelector('[role="dialog"] button.runtime-close'));
 await choose(select('Execution backend'),'codex');
 await click(buttons('Connect account')[0]);
 assert.deepEqual(calls.filter(call=>call[0]==='SetupProfile'),[['SetupProfile','codex']]);
 assert.ok(calls.filter(call=>call[0]==='InspectSetup').every(call=>call[1].runtime==='codex'&&call[1].caelisStore===''));
 await click(container.querySelector('[role="dialog"] button.runtime-close'));
 await click(buttons(translator('en').t('runtime.switch'))[0]);
 await click([...container.querySelectorAll('[role="dialog"] .runtime-choices button')].find(button=>button.querySelector('strong')?.textContent==='Caelis'));
 assert.ok(container.querySelector('[role="dialog"]').textContent.includes(translator('en').t('settings.nodeScopedPreparation')));
 assert.deepEqual(calls.filter(call=>call[0]==='SetupProfile'),[['SetupProfile','codex']]);
 assert.ok(!calls.some(call=>call[0]==='ActivateRuntime'||call[0]==='ApplySetup'));
});

test('thin paired shared-model configuration keeps its existing native binding and revision',async()=>{
 const fixture=pairedFixture();await mountPaired(fixture);await choose(select('Node'),'paired-node');
 const main=[...container.querySelectorAll('button.runtime-model-summary')].find(el=>el.getAttribute('aria-label').includes('Caelis main model'));
 await click(main);await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);await click(buttons('Save')[0]);
 const command=fixture.calls.find(call=>call[0]==='ChangeRemoteRuntimeConfiguration')[1];
 assert.equal(command.binding,'paired-binding');assert.equal(command.change.action,'main');assert.equal(command.change.expectedRevision,'config-paired-caelis');assert.equal(command.change.selection.model,'paired-b');
 assert.equal(fixture.calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,0);
});
test('paired read completion from a replaced native binding is rejected before populating a new view',async()=>{
 const {createPairedRuntimeInvoker}=await server.ssrLoadModule('/src/settings/runtime/pairedClient.ts');
 const read=deferred(),dispatched=deferred();let binding='original';const calls=[];
 const invoke=createPairedRuntimeInvoker(async(method,...args)=>{calls.push([method,...args]);if(method==='RemoteRuntime')return {binding,available:true,pending:[]};dispatched.resolve();return read.promise;},'original',()=> 'Connection changed');
 const waiting=invoke('RemoteRuntimeConfiguration','original');waiting.catch(()=>{});
 await dispatched.promise;binding='replacement';read.resolve(config('remote','caelis').configuration);
 await assert.rejects(waiting,e=>e.receipt.outcome==='rejected');
 assert.deepEqual(calls.filter(call=>call[0]==='RemoteRuntimeConfiguration'),[['RemoteRuntimeConfiguration','original']]);
});

const {NodeCoordinator}=await server.ssrLoadModule('/src/settings/runtime/NodeSetup.tsx');
const {createNodeRoamingClient}=await server.ssrLoadModule('/src/settings/runtime/roamingClient.ts');
const roamingState=(fields={})=>({available:true,enabled:false,coordinatorNodeId:'other',activeBotNodeId:'',state:'disabled',reason:'',operationId:'',outcome:'accepted',...fields});
const roamingPlan=(fields={})=>({id:'native-plan-1',coordinatorNodeId:'other',requiresConfirmation:true,actions:[{nodeId:'other',label:'Other',action:'prepare-coordinator'},{nodeId:'local',label:'Local',action:'stop-source'},{nodeId:'other',label:'Other',action:'start-bot'}],...fields});
const coordinatorCatalog=()=>({...catalog(),broker:{nodeId:'other',reachable:false,automaticRoaming:false,reason:'broker-offline'}});
async function mountCoordinator(call,value=coordinatorCatalog(),owner=createNodeRoamingClient(call)) {
 container=document.createElement('div');document.body.append(container);root=createRoot(container);
 await act(async()=>root.render(React.createElement(NodeCoordinator,{catalog:value,roaming:owner,call,onChanged:()=>{}})));
 return owner;
}

test('roaming is explicit, requires a real controller and saved coordinator, but can prepare an offline coordinator',async()=>{
 const calls=[];let ready=true;
 const call=async(method,...args)=>{calls.push([method,...args]);if(method==='PrepareNodeRoaming')return roamingPlan();return roamingState({available:ready});};
 const owner=await mountCoordinator(call);
 const enable=buttons('Enable automatic roaming')[0];assert.equal(enable.disabled,false);
 await choose(container.querySelector('#node-coordinator'),'local');
 assert.equal(enable.disabled,true);assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
 const nav=new Event('settings-navigate',{cancelable:true});assert.equal(window.dispatchEvent(nav),false);
 await click(buttons('Cancel')[0]);assert.equal(container.querySelector('#node-coordinator').value,'other');assert.equal(enable.disabled,false);
 ready=false;await act(async()=>owner.read());assert.equal(enable.disabled,true);
 assert.doesNotMatch(container.textContent,/broker-offline/);
});

test('repeated roaming clicks keep one original operation; lost delivery is checked without replay and mismatched receipts stay blocked',async()=>{
 const calls=[],delivery=deferred();let result=roamingState();
 const call=async(method,...args)=>{calls.push([method,...args]);if(method==='PrepareNodeRoaming')return roamingPlan();if(method==='EnableNodeRoaming')return delivery.promise;return result;};
 await mountCoordinator(call);
 await click(buttons('Enable automatic roaming')[0]);
 const confirm=buttons('Confirm')[0];
 await act(async()=>{confirm.dispatchEvent(new MouseEvent('click',{bubbles:true}));confirm.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 const commands=calls.filter(c=>c[0]==='EnableNodeRoaming');assert.equal(commands.length,1);
 assert.equal(commands[0][1].expectedCatalogRevision,'catalog-1');assert.ok(commands[0][1].id);
 const preparation=calls.find(c=>c[0]==='PrepareNodeRoaming');assert.equal(preparation[1].id,commands[0][1].id);assert.equal(preparation[1].allowPersistentExecution,false);assert.equal(preparation[1].reviewedPlanId,'');assert.equal(commands[0][1].reviewedPlanId,'native-plan-1');assert.equal(commands[0][1].allowPersistentExecution,true);
 await act(async()=>delivery.reject(new Error('response lost')));
 assert.equal(confirm.disabled,true);assert.match(container.textContent,/original operation is not confirmed/);
 await click(buttons('Cancel')[0]);
 result=roamingState({enabled:true,state:'ready',operationId:'different',activeBotNodeId:'other'});
 await click(buttons('Check original receipt')[0]);assert.equal(buttons('Stop automatic roaming')[0].disabled,true);
 result=roamingState({enabled:true,state:'waiting',operationId:commands[0][1].id});
 await click(buttons('Check original receipt')[0]);assert.equal(buttons('Stop automatic roaming')[0].disabled,false);
 assert.match(container.textContent,/Waiting for an available Bot node/);assert.doesNotMatch(container.textContent,/Bot is available on its confirmed node/);
 assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,1);
 assert.ok(calls.every(c=>!['ActivateRuntime','SelectWorkTarget'].includes(c[0])));
});

test('roaming confirmation captures catalog revision and cannot apply after node configuration changes',async()=>{
 const calls=[],call=async(method,...args)=>{calls.push([method,...args]);if(method==='PrepareNodeRoaming')return roamingPlan();return roamingState();};
 const owner=await mountCoordinator(call);await click(buttons('Enable automatic roaming')[0]);
 await act(async()=>root.render(React.createElement(NodeCoordinator,{catalog:{...coordinatorCatalog(),revision:'catalog-2'},roaming:owner,call,onChanged:()=>{}})));
 assert.equal(buttons('Confirm')[0].disabled,true);assert.match(container.textContent,/settings changed while this confirmation/);
 await click(buttons('Confirm')[0]);assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
});

test('disconnect retains confirmed roaming state but disables changes; only matching eligible live owner is shown ready',async()=>{
 let online=true;const call=async()=>{if(!online)throw new Error('offline');return roamingState({enabled:true,state:'ready',activeBotNodeId:'other'});};
 const value=coordinatorCatalog();value.broker.reachable=true;value.broker.automaticRoaming=true;
 const owner=await mountCoordinator(call,value);assert.match(container.textContent,/Bot is available on its confirmed node/);
 online=false;await act(async()=>owner.read());assert.match(container.textContent,/last confirmed state is shown/);assert.equal(buttons('Stop automatic roaming')[0].disabled,true);
 online=true;await act(async()=>owner.read());assert.equal(buttons('Stop automatic roaming')[0].disabled,false);
 const ineligible={...value,nodes:value.nodes.map(node=>({...node,runtimes:node.runtimes.map(runtime=>({...runtime,roles:runtime.roles.map(role=>({...role,eligible:false}))}))}))};
 await act(async()=>root.render(React.createElement(NodeCoordinator,{catalog:ineligible,roaming:owner,call,onChanged:()=>{}})));
 assert.doesNotMatch(container.textContent,/Bot is available on its confirmed node/);assert.match(container.textContent,/Waiting for an available Bot node/);
});

test('native unknown roaming operation is recovered after renderer remount and never replaced with a fresh ID',async()=>{
 const calls=[],call=async(method,...args)=>{calls.push([method,...args]);if(method==='PrepareNodeRoaming')return roamingPlan();return roamingState({enabled:true,state:'unknown',operationId:'native-original',outcome:'unknown'});};
 const owner=await mountCoordinator(call);assert.equal(owner.snapshot().pending,'native-original');assert.equal(buttons('Stop automatic roaming')[0].disabled,true);
 await click(buttons('Check original receipt')[0]);assert.equal(owner.snapshot().pending,'native-original');assert.equal(calls.length,2);assert.ok(calls.every(c=>c[0]==='NodeRoamingState'));
});

test('model drafts block explicit roaming confirmation and viewed-node selection never enables roaming',async()=>{
 const calls=[],call=async(method,...args)=>{calls.push([method,...args]);if(method==='PrepareNodeRoaming')return roamingPlan();if(method==='NodeRoamingState')return roamingState();return defaultInvoke(method,...args);};
 await mount(async(method,...args)=>method==='NodeCatalog'?coordinatorCatalog():config(...args),call);
 await choose(select('Node'),'other');assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
 await click(container.querySelector('button.runtime-model-summary'));
 await click(buttons('Enable automatic roaming')[0]);assert.equal(container.querySelectorAll('[role="dialog"]').length,1);assert.match(container.textContent,/Finish or cancel/);
 assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
 await click(buttons('Cancel')[0]);await click(buttons('Enable automatic roaming')[0]);assert.match(container.querySelector('[role="dialog"]').textContent,/This changes where Bot runs/);
});

test('busy disable rejection preserves confirmed owner and uses one stable command ID',async()=>{
 const calls=[],delivery=deferred();const call=async(method,...args)=>{calls.push([method,...args]);if(method==='DisableNodeRoaming')return delivery.promise;return roamingState({enabled:true,state:'waiting',activeBotNodeId:'other'});};
 await mountCoordinator(call);await click(buttons('Stop automatic roaming')[0]);
 const confirm=buttons('Confirm')[0];await act(async()=>{confirm.dispatchEvent(new MouseEvent('click',{bubbles:true}));confirm.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 const command=calls.find(c=>c[0]==='DisableNodeRoaming');assert.equal(calls.filter(c=>c[0]==='DisableNodeRoaming').length,1);
 await act(async()=>delivery.resolve(roamingState({enabled:true,state:'waiting',activeBotNodeId:'other',operationId:command[1].id,outcome:'rejected',reason:'source-busy'})));
 assert.match(container.textContent,/change was not confirmed/);assert.doesNotMatch(container.textContent,/source-busy/);assert.match(container.textContent,/Waiting for an available Bot node/);
 await click(buttons('Cancel')[0]);assert.equal(buttons('Stop automatic roaming')[0].disabled,false);
 assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
});

test('deployment plan is read-only, human reviewed, and cancellation does not enable persistent execution',async()=>{
 const calls=[],call=async(method,...args)=>{calls.push([method,...args]);return method==='PrepareNodeRoaming'?roamingPlan({actions:[{nodeId:'other',label:'Other',action:'prepare-coordinator'},{nodeId:'other',label:'Other',action:'connect-outgoing'},{nodeId:'local',label:'Local',action:'stop-source'}]}):roamingState();};
 await mountCoordinator(call);await click(buttons('Enable automatic roaming')[0]);
 const dialog=container.querySelector('[role='+'"dialog"'+']');assert.match(dialog.textContent,/Prepare the always-on service on Other/);assert.match(dialog.textContent,/Connect the enrolled outgoing node Other/);assert.match(dialog.textContent,/Safely stop the current Bot on Local/);
 assert.doesNotMatch(dialog.textContent,/prepare-coordinator|connect-outgoing|native-plan-1/);
 assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);await click(buttons('Cancel')[0]);assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
});

test('late or unrecognized deployment plans cannot be confirmed or execute a roaming change',async()=>{
 const calls=[],late=deferred();let unknown=false;
 const call=async(method,...args)=>{calls.push([method,...args]);return method==='PrepareNodeRoaming'?unknown?roamingPlan({actions:[{nodeId:'other',label:'Other',action:'raw-shell'}]}):late.promise:roamingState();};
 const owner=await mountCoordinator(call);await click(buttons('Enable automatic roaming')[0]);
 await act(async()=>root.render(React.createElement(NodeCoordinator,{catalog:{...coordinatorCatalog(),revision:'catalog-2'},roaming:owner,call,onChanged:()=>{}})));
 await act(async()=>late.resolve(roamingPlan()));assert.equal(container.querySelector('[role="dialog"]'),null);assert.match(container.textContent,/Node settings changed/);
 unknown=true;await click(buttons('Enable automatic roaming')[0]);assert.equal(container.querySelector('[role="dialog"]'),null);assert.match(container.textContent,/Could not prepare the deployment plan/);
 assert.equal(calls.filter(c=>c[0]==='EnableNodeRoaming').length,0);
});

test('enabled and unresolved roaming lock coordinator clear/change; confirmed disabled state restores editing',async()=>{
 const calls=[];let native=roamingState({enabled:true,state:'ready',activeBotNodeId:'other'});
 const call=async(method,...args)=>{calls.push([method,...args]);return method==='NodeRoamingState'?native:coordinatorCatalog();};
 const owner=await mountCoordinator(call);
 for(const current of [native,roamingState({enabled:true,state:'waiting'}),roamingState({enabled:true,state:'unavailable'}),roamingState({state:'enabling'}),roamingState({enabled:true,state:'disabling'}),roamingState({state:'unknown',operationId:'original',outcome:'unknown'})]) {
  native=current;await act(async()=>owner.read());
  const coordinator=container.querySelector('#node-coordinator');assert.equal(coordinator.disabled,true,current.state);assert.equal(buttons('Save')[0].disabled,true,current.state);
  await choose(coordinator,'');assert.equal(coordinator.value,'other');
  await choose(coordinator,'local');assert.equal(coordinator.value,'other');await click(buttons('Save')[0]);
  assert.match(container.textContent,/Stop automatic roaming before changing the always-on node/);
 }
 assert.equal(calls.filter(call=>call[0]==='SetNodeCoordinator').length,0);
 native=roamingState({state:'disabled',operationId:'original',outcome:'accepted'});await act(async()=>owner.read());
 assert.equal(container.querySelector('#node-coordinator').disabled,false);
 await choose(container.querySelector('#node-coordinator'),'local');assert.equal(buttons('Save')[0].disabled,false);await click(buttons('Save')[0]);
 await choose(container.querySelector('#node-coordinator'),'');await click(buttons('Save')[0]);
 assert.deepEqual(calls.filter(call=>call[0]==='SetNodeCoordinator').map(call=>call[1]),[{nodeId:'local',expectedRevision:'catalog-1'},{nodeId:'',expectedRevision:'catalog-1'}]);
});

test('live roaming coordinator lock leaves viewed-node selection available and makes no roaming mutation',async()=>{
 const calls=[],call=async(method,...args)=>{calls.push([method,...args]);return method==='NodeRoamingState'?roamingState({enabled:true,state:'waiting'}):defaultInvoke(method,...args);};
 await mount(async(method,...args)=>method==='NodeCatalog'?coordinatorCatalog():config(...args),call);
 assert.equal(container.querySelector('#node-coordinator').disabled,true);assert.equal(select('Node').disabled,false);
 await choose(select('Node'),'other');assert.equal(select('Node').value,'other');assert.equal(container.querySelector('#node-coordinator').value,'other');
 assert.ok(calls.every(call=>!['SetNodeCoordinator','EnableNodeRoaming','DisableNodeRoaming','ActivateRuntime'].includes(call[0])));
});


test('a coordinator draft stays cancellable when native roaming becomes enabled before save',async()=>{
 const calls=[];let native=roamingState();const call=async(method,...args)=>{calls.push([method,...args]);return native;};
 const owner=await mountCoordinator(call);await choose(container.querySelector('#node-coordinator'),'local');
 native=roamingState({enabled:true,state:'waiting'});await act(async()=>owner.read());
 assert.equal(buttons('Save')[0].disabled,true);assert.equal(container.querySelector('#node-coordinator').disabled,true);
 assert.equal(buttons('Cancel')[0].disabled,false);await click(buttons('Cancel')[0]);
 assert.equal(container.querySelector('#node-coordinator').value,'other');assert.equal(buttons('Stop automatic roaming')[0].disabled,false);
 assert.equal(calls.filter(call=>call[0]==='SetNodeCoordinator').length,0);
});

// Synthetic DOM and native DTO fixtures only; these checks do not claim GUI,
// native authentication, installed Runtime or remote process acceptance.
const flow=(stage='complete',sequence=1)=>({id:'original-flow',revision:`flow-${sequence}`,sequence,stage,title:'Node connection',message:'',installation:null,authorization:null,launchers:[],methods:[],models:[]});
const enter=async(el,value)=>{assert.ok(el);await act(async()=>{Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype,'value').set.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}));});};
const coldCatalog=()=>{const value=catalog();value.selectedNodeId='other';value.nodes[1].runtimes[0]={...runtime(),health:'unavailable',authentication:'required'};return value;};
const coldConfig=(nodeId,backend)=>{const value=config(nodeId,backend);return {...value,configurationAvailable:false,configuration:{...value.configuration,main:null,models:[],connections:[],team:{...value.configuration.team,roles:null,sets:null,models:null}}};};
const nativeRef=(guard,operationId)=>({nodeId:guard.nodeId,backend:guard.backend,operationId});

test('cold Node wizard forwards manual auth through one pinned native reference and rejects late scope callbacks',async()=>{
 const calls=[],oldWait=deferred();
 const invoke=async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return coldCatalog();
  if(method==='NodeRuntimeConfiguration')return coldConfig(...args);
  if(method==='BeginNodeRuntimeConnection')return nativeRef(...args);
  if(method==='NodeRuntimeConnectionCatalog')return {choices:[{id:'provider',name:'Fixture provider',description:'',custom:false}],unavailable:''};
  if(method==='NodeRuntimeSetupCatalog')return args[1]==='endpoints'?[{value:'https://fixture.invalid/v1',label:'Endpoint',noAuth:false}]:[{value:'fixture-model',label:'Model',noAuth:false}];
  if(method==='StartNodeRuntimeConnection')return {...flow('authorization'),authorization:{url:'https://fixture.invalid/auth',inputLabel:'Authorization code',canSubmit:true}};
  if(method==='WaitNodeRuntimeConnection')return oldWait.promise;
  if(method==='AdvanceNodeRuntimeConnection')return flow('complete',2);
  if(method==='CloseNodeRuntimeConnection'||method==='OpenMessageLink')return;
  throw new Error('Unexpected fixture method');
 };
 await mount(invoke);
 assert.equal(calls.filter(c=>c[0].includes('NodeRuntimeConnection')).length,0,'passive viewing started setup');
 assert.match(container.textContent,/Authentication|credentials|Set up/i);
 const add=buttons('Add connection')[0];assert.equal(add.disabled,false);
 await act(async()=>{add.dispatchEvent(new MouseEvent('click',{bubbles:true}));add.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 assert.ok(container.querySelector('[role="dialog"]'));
 const begin=calls.find(c=>c[0]==='BeginNodeRuntimeConnection');
 assert.deepEqual(begin[1],{nodeId:'other',backend:'caelis',revision:'guard-other-caelis'});
 assert.equal(calls.filter(c=>c[0]==='BeginNodeRuntimeConnection').length,1);
 const ref=nativeRef(begin[1],begin[2]);
 await click(buttons('API Key')[0]);await click(buttons('Fixture provider')[0]??[...container.querySelectorAll('button')].find(b=>b.textContent.includes('Fixture provider')));
 const password=container.querySelector('input[type="password"]');assert.equal(password.autocomplete,'off');
 await enter(container.querySelector('input[list]:not([type="url"])'),'fixture-model');
 await enter(password,'manual-fixture-key');
 await choose(select('Node'),'local');assert.equal(select('Node').value,'other');assert.equal(password.value,'manual-fixture-key');
 const connect=buttons('Connect')[0];assert.equal(connect.disabled,false);
 await act(async()=>{connect.dispatchEvent(new MouseEvent('click',{bubbles:true}));connect.dispatchEvent(new MouseEvent('click',{bubbles:true}));});
 const start=calls.find(c=>c[0]==='StartNodeRuntimeConnection');
 assert.equal(calls.filter(c=>c[0]==='StartNodeRuntimeConnection').length,1);
 assert.deepEqual(start[1],ref);assert.equal(start[2].settings,null);assert.equal(start[2].apiKey,'manual-fixture-key');
 assert.equal(container.querySelector('input[type="password"]').value,'');
 await click(buttons('Open login page')[0]);assert.deepEqual(calls.find(c=>c[0]==='OpenMessageLink'),['OpenMessageLink','https://fixture.invalid/auth']);
 await enter(container.querySelector('input[type="password"]'),'manual-fixture-code');
 await click(buttons('Submit authorization information')[0]??container.querySelector('[role="dialog"] form button'));
 const advance=calls.find(c=>c[0]==='AdvanceNodeRuntimeConnection');
 assert.deepEqual(advance[1],ref);assert.equal(advance[2].id,'original-flow');assert.equal(advance[2].input.code,'manual-fixture-code');
 await click(buttons('Done')[0]);
 assert.equal(container.querySelector('[role="dialog"]'),null);
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,1);
 assert.deepEqual(calls.find(c=>c[0]==='CloseNodeRuntimeConnection')[1],ref);
 await choose(select('Node'),'local');
 await act(async()=>oldWait.resolve({...flow('authorization',99),title:'Late stale authorization'}));
 assert.doesNotMatch(container.textContent,/Late stale authorization|manual-fixture-key|manual-fixture-code/);
 assert.ok(calls.filter(c=>['NodeRuntimeConnectionCatalog','NodeRuntimeSetupCatalog','WaitNodeRuntimeConnection'].includes(c[0])).every(c=>JSON.stringify(c[1])===JSON.stringify(ref)));
});

test('dismissal before Start closes the original Node setup exactly once',async()=>{
 const calls=[];
 await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return coldCatalog();if(method==='NodeRuntimeConfiguration')return coldConfig(...args);if(method==='BeginNodeRuntimeConnection')return nativeRef(...args);if(method==='NodeRuntimeConnectionCatalog')return {choices:[],unavailable:''};if(method==='CloseNodeRuntimeConnection')return;throw new Error('Unexpected fixture method');});
 await click(buttons('Add connection')[0]);
 await click(container.querySelector('button.runtime-close'));
 assert.equal(container.querySelector('[role="dialog"]'),null);
 assert.equal(calls.filter(c=>c[0]==='StartNodeRuntimeConnection').length,0);
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,1);
});

test('late Begin after unmount closes its original owner and never mounts a stale wizard',async()=>{
 const begin=deferred(),calls=[];
 await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return coldCatalog();if(method==='NodeRuntimeConfiguration')return coldConfig(...args);if(method==='BeginNodeRuntimeConnection')return begin.promise;if(method==='CloseNodeRuntimeConnection')return;throw new Error('Unexpected fixture method');});
 await click(buttons('Add connection')[0]);
 await choose(select('Node'),'local');assert.equal(select('Node').value,'other');
 await act(async()=>root.unmount());root=null;
 const original=calls.find(c=>c[0]==='BeginNodeRuntimeConnection');
 await act(async()=>begin.resolve(nativeRef(original[1],original[2])));
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,1);
 assert.deepEqual(calls.find(c=>c[0]==='CloseNodeRuntimeConnection')[1],nativeRef(original[1],original[2]));
 assert.equal(calls.filter(c=>c[0]==='NodeRuntimeConnectionCatalog').length,0);
});

test('lost Begin and cleanup keep the original scope blocked without leaking SDK errors or restarting',async()=>{
 const calls=[];
 await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return coldCatalog();if(method==='NodeRuntimeConfiguration')return coldConfig(...args);if(['BeginNodeRuntimeConnection','CloseNodeRuntimeConnection'].includes(method))throw new Error('RAW-SDK-BODY-MUST-STAY-PRIVATE');throw new Error('Unexpected fixture method');});
 await click(buttons('Add connection')[0]);await click(buttons('Add connection')[0]);
 assert.equal(calls.filter(c=>c[0]==='BeginNodeRuntimeConnection').length,1);
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,1);
 assert.equal(container.querySelector('[role="dialog"]'),null);
 assert.doesNotMatch(container.textContent,/RAW-SDK-BODY-MUST-STAY-PRIVATE/);
});

test('Node cancel and unknown Start preserve original native flow without duplicate actions',async()=>{
 const calls=[],delivery=deferred();
 const owner=createNodeSettingsClient(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeRuntimeConfiguration')return coldConfig(...args);if(method==='BeginNodeRuntimeConnection')return nativeRef(...args);if(method==='StartNodeRuntimeConnection')return delivery.promise;if(method==='CancelNodeRuntimeConnection'||method==='CloseNodeRuntimeConnection')return;throw new Error('Unexpected fixture method');});
 const view=createNodeRuntimeClient(owner,{nodeId:'other',backend:'caelis',revision:'catalog-revision'});await view.read();
 const client=await view.beginConnection(),signal=new AbortController().signal;
 const first=client.startConnection({kind:'account',choice:'fixture'},signal,()=>{});first.catch(()=>{});
 const again=client.startConnection({kind:'account',choice:'other'},signal,()=>{});again.catch(()=>{});
 delivery.reject(new Error('private-native-response-loss'));
 await assert.rejects(first,e=>e.unknown);await assert.rejects(again,e=>e.unknown);
 await assert.rejects(client.startConnection({kind:'account',choice:'fresh'},signal,()=>{}),e=>e.unknown);
 assert.equal(calls.filter(c=>c[0]==='StartNodeRuntimeConnection').length,1);
 await Promise.all([client.cancelConnection(flow('unknown')),client.cancelConnection(flow('unknown'))]);
 assert.equal(calls.filter(c=>c[0]==='CancelNodeRuntimeConnection').length,1);
 await Promise.all([client.closeConnection(),client.closeConnection()]);
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,1);
});

test('ordinary active local Runtime keeps original configuration, edits and connection wizard without a Node agent',async()=>{
 const calls=[],profile={runtime:'caelis',cliPath:'/fixture/source-caelis',caelisStore:'/fixture/source-store'};
 const shared={...config('local','caelis').configuration,revision:'source-revision',main:{model:'source-store-model',effort:'high',serviceTier:''},models:[model('source-store-model'),model('source-store-other')]};
 await mount(async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return {...catalog(),activeBotNodeId:'local'};
  if(method==='NodeRuntimeConfiguration')return {...config(...args),configuration:{...config(...args).configuration,main:{model:'private-node-slot',effort:'high',serviceTier:''}}};
  if(method==='RuntimeSettings')return profile;
  if(method==='SetupOverview')return {active:'caelis',pending:''};
  if(method==='InspectSetup')return {settings:profile,state:'ready',message:'',installation:{installed:true,path:'',version:'1',latestVersion:'1',updateState:'',message:''},models:[],selectedModel:'',accountType:''};
  if(method==='RuntimeConfiguration')return shared;
  if(['ExecutionSettings','WorkExecutionSettings'].includes(method))return {model:'source-store-model',effort:'high',serviceTier:''};
  if(method==='Models')return shared.models;
  if(method==='ChangeRuntimeConfiguration')return {operationId:'local-original',outcome:'committed',message:''};
  if(method==='RuntimeConnectionCatalog')return {choices:[{id:'fixture',name:'Original local provider',description:'',custom:false}],unavailable:''};
  if(method==='StartRuntimeConnection')return flow();
  throw new Error('Unexpected fixture method');
 });
 assert.match(container.textContent,/source-store-model/);assert.doesNotMatch(container.textContent,/private-node-slot/);
 await click(container.querySelectorAll('button.runtime-model-summary')[1]);
 await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);await click(buttons('Save')[0]);
 assert.equal(calls.filter(c=>c[0]==='ChangeRuntimeConfiguration').length,1);assert.equal(calls.filter(c=>c[0]==='ChangeNodeConfiguration').length,0);
 await click(buttons('Add connection')[0]);await click([...container.querySelectorAll('button')].find(b=>b.textContent.includes('Original local provider')));await click(buttons('Continue login')[0]);await click(buttons('Done')[0]);
 assert.equal(calls.filter(c=>c[0]==='StartRuntimeConnection').length,1);
 assert.deepEqual(calls.find(c=>c[0]==='StartRuntimeConnection')[1].settings,profile);
 assert.equal(calls.filter(c=>/NodeRuntimeConnection|NodeRuntimeSetupCatalog/.test(c[0])).length,0);
});

test('local model draft rejects a changed Bot owner before Save and preserves the selected value',async()=>{
 const calls=[],profile={runtime:'caelis',cliPath:'/fixture/source-caelis',caelisStore:'/fixture/source-store'};
 let activeBotNodeId='local';
 const shared={...config('local','caelis').configuration,revision:'source-revision',main:{model:'source-store-model',effort:'high',serviceTier:''},models:[model('source-store-model'),model('source-store-other')]};
 await mount(async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return {...catalog(),activeBotNodeId};
  if(method==='NodeRuntimeConfiguration')return config(...args);
  if(method==='RuntimeSettings')return profile;
  if(method==='SetupOverview')return {active:'caelis',pending:''};
  if(method==='InspectSetup')return {settings:profile,state:'ready',message:'',installation:{installed:true,path:'',version:'1',latestVersion:'1',updateState:'',message:''},models:[],selectedModel:'',accountType:''};
  if(method==='RuntimeConfiguration')return shared;
  if(['ExecutionSettings','WorkExecutionSettings'].includes(method))return {model:'source-store-model',effort:'high',serviceTier:''};
  if(method==='Models')return shared.models;
  throw new Error('Unexpected fixture method');
 });
 await click(container.querySelectorAll('button.runtime-model-summary')[1]);
 await click(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1]);
 activeBotNodeId='other';
 await click(buttons('Save')[0]);
 assert.ok(container.querySelector('[role="dialog"]'));
 assert.equal(container.querySelectorAll('[role="dialog"] input[type="radio"]')[1].checked,true);
 assert.ok(container.querySelector('[role="dialog"] [role="alert"]'));
 assert.equal(calls.filter(c=>['ChangeRuntimeConfiguration','ChangeNodeConfiguration','SaveExecutionSettings','SaveWorkExecutionSettings'].includes(c[0])).length,0);
});

test('local adapter rechecks backend and profile for every global mutation and late inspection',async()=>{
 const profile={runtime:'caelis',cliPath:'/fixture/source-caelis',caelisStore:'/fixture/source-store'};
 for(const changed of [{...profile,runtime:'codex'},{...profile,caelisStore:'/fixture/another-store'}]){
  let current=profile,mutations=0;
  const owner=createNodeSettingsClient(async(method)=>{
   if(method==='NodeCatalog')return {...catalog(),activeBotNodeId:'local'};
   if(method==='RuntimeSettings')return current;
   throw new Error('Unexpected fixture method');
  });
  owner.localRuntime=()=>({read:async()=>({profile,revision:'source-revision'}),saveModel:async()=>mutations++,changeTeam:async()=>mutations++,removeModel:async()=>mutations++,startConnection:async()=>mutations++});
  const adapter=createNodeRuntimeClient(owner,{nodeId:'local',backend:'caelis',revision:'catalog-revision'},{defaultLocal:true});
  await adapter.read();
  const captured=adapter.capture('source-revision');current=changed;
  await assert.rejects(()=>captured.saveModel('runtime',{model:'manual-model',effort:'',serviceTier:''},'source-revision'));
  await assert.rejects(()=>captured.changeTeam({action:'reset',id:'role'},'source-revision'));
  await assert.rejects(()=>captured.removeModel({id:'provider'}, {id:'model'},'source-revision'));
  await assert.rejects(()=>captured.startConnection({kind:'api-key',choice:'provider',apiKey:'manual-fixture'},new AbortController().signal,()=>{}));
  assert.equal(mutations,0);
 }
 const inspection=deferred();let activeBotNodeId='local';
 const owner=createNodeSettingsClient(async(method)=>{
  if(method==='NodeCatalog')return {...catalog(),activeBotNodeId};
  if(method==='RuntimeSettings')return profile;
  throw new Error('Unexpected fixture method');
 });
 owner.localRuntime=()=>({read:()=>inspection.promise});
 const adapter=createNodeRuntimeClient(owner,{nodeId:'local',backend:'caelis',revision:'catalog-revision'},{defaultLocal:true});
 const pending=adapter.read();await new Promise(resolve=>setImmediate(resolve));
 activeBotNodeId='other';inspection.resolve({profile,revision:'source-revision'});
 await assert.rejects(()=>pending);
});


test('StrictMode effect replay keeps explicitly opened Node owner until settings actually close',async()=>{
 const calls=[];
 await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog')return coldCatalog();if(method==='NodeRuntimeConfiguration')return coldConfig(...args);if(method==='BeginNodeRuntimeConnection')return nativeRef(...args);if(method==='NodeRuntimeConnectionCatalog')return {choices:[],unavailable:''};if(method==='CloseNodeRuntimeConnection')return;throw new Error('Unexpected fixture method');},async()=>({revision:1,nodes:[],issue:''}),true);
 await click(buttons('Add connection')[0]);
 assert.ok(container.querySelector('[role="dialog"]'));
 assert.equal(calls.filter(c=>c[0]==='BeginNodeRuntimeConnection').length,1);
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,0);
 await act(async()=>window.dispatchEvent(new Event('settings-close')));
 assert.equal(container.querySelector('[role="dialog"]'),null);
 assert.equal(calls.filter(c=>c[0]==='CloseNodeRuntimeConnection').length,1);
});

test('missing Node Runtime remains honest and cannot start authentication before installation',async()=>{
 const calls=[];
 await mount(async(method,...args)=>{calls.push([method,...args]);if(method==='NodeCatalog'){const value=coldCatalog();value.nodes[1].runtimes[0].health='missing';value.nodes[1].runtimes[0].authentication='unknown';return value;}const value=coldConfig(...args);value.installation.installed=false;return value;});
 assert.match(container.textContent,/Not installed/);
 assert.equal(buttons('Add connection')[0].disabled,true);
 await click(buttons('Add connection')[0]);
 assert.equal(calls.filter(c=>c[0]==='BeginNodeRuntimeConnection').length,0);
});

test('cold external Caelis executable enables explicit Node connection without a managed copy or authenticated account',async()=>{
 const calls=[];
 await mount(async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return coldCatalog();
  if(method==='NodeRuntimeConfiguration')return {...coldConfig(...args),configurationAvailable:false,installerAvailable:true,installation:{installed:false,version:'',latestVersion:'0.65.0'},executable:{installed:true,version:'0.65.0'},reviewedVersions:['0.65.0']};
  if(method==='BeginNodeRuntimeConnection')return nativeRef(...args);
  if(method==='NodeRuntimeConnectionCatalog')return {choices:[],unavailable:''};
  if(method==='CloseNodeRuntimeConnection')return;
  throw new Error('Unexpected fixture method');
 });
 assert.equal(buttons('Add connection')[0].disabled,false);
 assert.equal(calls.filter(call=>call[0]==='BeginNodeRuntimeConnection').length,0);
 assert.equal(calls.filter(call=>call[0]==='ChangeNodeConfiguration').length,0);
 assert.match(container.textContent,/Not connected|Not currently connected/i);
 await click(buttons('Add connection')[0]);
 assert.ok(container.querySelector('[role="dialog"]'));
 assert.equal(calls.filter(call=>call[0]==='BeginNodeRuntimeConnection').length,1);
 await act(async()=>window.dispatchEvent(new Event('settings-close')));
 assert.equal(calls.filter(call=>call[0]==='CloseNodeRuntimeConnection').length,1);
});

test('managed-copy metadata cannot conceal an explicitly missing executable',async()=>{
 const owner=createNodeSettingsClient(async(method,...args)=>{
  if(method==='NodeRuntimeConfiguration')return {...coldConfig(...args),installation:{installed:true,version:'0.65.0',latestVersion:'0.65.0'},executable:{installed:false,version:''}};
  throw new Error('Unexpected fixture method');
 });
 const client=createNodeRuntimeClient(owner,{nodeId:'other',backend:'caelis',revision:''});
 const view=await client.read();
 assert.equal(view.setup.installation.installed,false);
 assert.equal(view.setup.state,'installation');
});
