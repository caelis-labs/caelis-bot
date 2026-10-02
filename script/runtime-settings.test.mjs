import assert from 'node:assert/strict';
import { test, after } from 'node:test';
import { createServer } from 'vite';
import { readFileSync } from 'node:fs';
const server = await createServer({server:{middlewareMode:true,ws:false},appType:'custom'});
after(()=>server.close());
const {createRuntimeSettingsClient} = await server.ssrLoadModule('/src/settings/runtime/client.ts');
const {chooseModel, validSelection, groupLegacyModels, acceptConnectionProgress, safeWebURL} = await server.ssrLoadModule('/src/settings/runtime/state.ts');
const {createPreviewClient} = await server.ssrLoadModule('/src/settings/runtime/preview/client.ts');
const model={model:'p/a',name:'A',description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[{id:'priority',name:'Fast',description:''}]};
const selection={model:'p/a',effort:'high',serviceTier:'priority'};

test('switching models drops incompatible effort and speed; stale options cannot save',()=>{
 const next={...model,model:'p/b',defaultEffort:'low',serviceTiers:[]};
 assert.deepEqual(chooseModel(next),{model:'p/b',effort:'low',serviceTier:''});
 assert.equal(validSelection(selection,[model]),true);
 assert.equal(validSelection({...selection,effort:'ultra'},[model]),false);
 assert.equal(validSelection({...selection,model:'p/b'},[next]),false);
 assert.equal(validSelection({model:'',effort:'high',serviceTier:''},[],true),false);
 assert.equal(validSelection({model:'',effort:'',serviceTier:''},[],true),true);
});
test('model write preserves fresh approval even when draft has stale extra fields',async()=>{
 const calls=[];
 const client=createRuntimeSettingsClient(async(method,...args)=>{calls.push([method,...args]);return {model:'p/old',effort:'low',serviceTier:'',approvalMode:'fresh'};});
 await client.saveModel('conversation',{...selection,approvalMode:'stale'});
 assert.deepEqual(calls,[['ExecutionSettings'],['SaveExecutionSettings',{...selection,approvalMode:'fresh'}]]);
});
test('shared model and Team retain revision and native profile identifiers',async()=>{
 const calls=[]; const client=createRuntimeSettingsClient(async(...args)=>{calls.push(args);return {operationId:'op',outcome:'committed',message:'Saved'};});
 await client.saveModel('runtime',selection,'9007199254740993');
 await client.changeTeam({action:'bind',id:'orbit',selection:{...selection,model:'native-profile',serviceTier:'fast'}},'9007199254740993');
 assert.equal(calls[0][1].expectedRevision,'9007199254740993');
 assert.equal(calls[1][1].selection.model,'native-profile');assert.equal(calls[1][1].selection.serviceTier,'fast');
});
test('unknown native receipt cannot be reported as success or automatically retried',async()=>{
 let calls=0;const client=createRuntimeSettingsClient(async()=>{calls++;return {operationId:'op',outcome:'unknown',message:'Refresh to reconcile'};});
 await assert.rejects(client.saveModel('runtime',selection,'1'),e=>e.unknown&&e.receipt.operationId==='op');assert.equal(calls,1);
});
test('unmounted preparation stops before mutation; deletion keeps exact identity and all-agent usage',async()=>{
 const calls=[];const client=createRuntimeSettingsClient(async(...args)=>{calls.push(args);return {operationId:'op',outcome:'committed',message:''};});
 const abort=new AbortController();abort.abort();
 await assert.rejects(client.startConnection({kind:'api-key',choice:'p'},abort.signal,()=>{}));assert.deepEqual(calls,[]);
 const [group]=groupLegacyModels([{value:'provider/vendor/model',label:'Model',current:false,noAuth:false}], '');
 await client.removeModel(group,group.models[0],'5');assert.equal(calls.at(-1)[1].id,'provider/vendor/model');assert.equal(calls.at(-1)[1].expectedRevision,'5');
 const count=calls.length;await assert.rejects(client.removeModel(group,{...group.models[0],uses:['orbit']}));
 await assert.rejects(client.removeModel({...group,kind:'agent',models:[group.models[0],{...group.models[0],uses:['orbit']}]},group.models[0]));assert.equal(calls.length,count);
 await client.removeModel({...group,id:'native-agent-id',kind:'agent'},group.models[0],'5');assert.equal(calls.at(-1)[1].id,'native-agent-id');assert.equal(calls.at(-1)[1].action,'disconnect-agent');
});
test('native auth progress is observed without redispatching connection',async()=>{
 const calls=[];let complete;
 const terminal=new Promise(resolve=>complete=resolve);
 const client=createRuntimeSettingsClient(async(method,...args)=>{calls.push([method,...args]);return {id:'f',revision:method==='StartRuntimeConnection'?'1':'2',sequence:method==='StartRuntimeConnection'?1:2,stage:method==='StartRuntimeConnection'?'authorization':'complete',title:'',message:'',installation:null,authorization:null};});
 await client.startConnection({kind:'account',choice:'grok'},new AbortController().signal,view=>complete(view));
 assert.equal((await terminal).stage,'complete');assert.equal(calls.filter(c=>c[0]==='StartRuntimeConnection').length,1);assert.equal(calls.filter(c=>c[0]==='WaitRuntimeConnection').length,1);
});
test('first Caelis setup pins the selected profile before activation',async()=>{
 const calls=[],profile={runtime:'caelis',cliPath:'/fixture/caelis',caelisStore:'/fixture/store'};
 const client=createRuntimeSettingsClient(async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='StartRuntimeConnection')return {id:'f',revision:'1',sequence:1,stage:'complete',title:'',message:'',installation:null,authorization:null};
  return [];
 },profile);
 await client.catalog('account');
 await client.apiKeyOptions('openai','https://example.invalid');
 await client.startConnection({kind:'account',choice:'grok'},new AbortController().signal,()=>{});
 assert.deepEqual(calls[0],['RuntimeConnectionCatalog','account',profile]);
 assert.deepEqual(calls.at(-1)[1].settings,profile);
 assert.ok(calls.filter(c=>c[0]==='SetupCatalog').every(c=>c[1].settings===profile));
 assert.equal(calls.filter(c=>c[0]==='RuntimeSettings').length,0);
});
test('OAuth progress cannot rewind completion or newer ordered status',()=>{
 const complete={id:'auth-1',revision:'opaque-a',sequence:3,stage:'complete',title:'',message:''};
 assert.equal(acceptConnectionProgress(complete,{...complete,sequence:2,stage:'authorization'}),complete);
 assert.equal(acceptConnectionProgress(complete,{...complete,sequence:4,stage:'authorization'}),complete);
 const models={...complete,stage:'models'};assert.equal(acceptConnectionProgress(models,{...models,sequence:2,stage:'authorization'}),models);
 assert.equal(safeWebURL('javascript:alert(1)'),false);assert.equal(safeWebURL('https://user:key@example.invalid'),false);assert.equal(safeWebURL('https://example.invalid/auth?state=example'),true);
});
test('preview covers built-in/custom, Antigravity preparation, terminal capability and Grok code auth',async()=>{
 const client=createPreviewClient(),signal=new AbortController().signal;
 const catalog=await client.catalog('agent');assert.ok(catalog.choices.some(c=>c.id==='custom'&&c.custom));assert.ok(catalog.choices.some(c=>c.id==='antigravity'&&!c.custom));
 let flow=await client.startConnection({kind:'agent',choice:'antigravity'},signal,()=>{});
 assert.equal(flow.stage,'installation');assert.ok(flow.installation.source);
 flow=await client.advanceConnection(flow,'check-installation',{},signal,()=>{});assert.equal(flow.methods.find(m=>m.id==='terminal').available,false);
 flow=await client.startConnection({kind:'account',choice:'grok'},signal,()=>{});assert.equal(flow.stage,'models');
 flow=await client.advanceConnection(flow,'connect',{model:flow.models[0].id},signal,()=>{});assert.ok(flow.authorization.inputLabel);
 flow=await client.advanceConnection(flow,'submit-code',{code:'fixture'},signal,()=>{});assert.equal(flow.stage,'complete');
 const view=await client.read();await client.changeTeam({action:'bind',id:'orbit',selection},view.team.revision);
 await assert.rejects(client.changeTeam({action:'bind',id:'orbit',selection},view.team.revision));
});
test('production entry has no preview import or fake success fallback',()=>{
 const main=readFileSync('frontend/src/main.tsx','utf8');const client=readFileSync('frontend/src/settings/runtime/client.ts','utf8');
 assert.doesNotMatch(main,/preview\//);assert.doesNotMatch(client,/createPreviewClient|preview\/client/);
});

test('same machine runtime actions retain exact target and reuse machine association',async()=>{
 const {callWorkerTarget,workerTargetKey}=await server.ssrLoadModule('/src/workerNodeTargets.ts');
 const legacy={id:'machine-a',label:'Build machine',ssh:'existing-alias',backend:'',helper:'',store:'/native/store',workspaceRoot:'/workspace'};
 const codex={...legacy,backend:'codex',store:'',socket:'/native/worker.sock'};
 const nodes=[{config:legacy},{config:codex}];
 const calls=[];
 const call=async(method,...args)=>{calls.push([method,...args]);return {nodes,revision:42};};
 await callWorkerTarget(call,'ConnectWorkerTarget',legacy,42);
 await callWorkerTarget(call,'DisconnectWorkerTarget',codex,42);
 assert.deepEqual(calls,[['ConnectWorkerTarget',{nodeId:'machine-a',backend:'caelis',role:'worker'},42],['DisconnectWorkerTarget',{nodeId:'machine-a',backend:'codex',role:'worker'},42]]);
 assert.notEqual(workerTargetKey(legacy),workerTargetKey(codex));
 const ui=readFileSync('frontend/src/WorkerNodeSettings.tsx','utf8');
 assert.doesNotMatch(ui,/worker-node-ssh|worker-node-store|worker-node-root/);
 assert.doesNotMatch(ui,/perform\('(?:Probe|Connect|Disconnect)WorkerNode'/);
});

test('remote model edits preserve inspected binding and strip protected settings',async()=>{
 const {createRemoteExecutionClient}=await server.ssrLoadModule('/src/settings/runtime/remoteExecutionClient.ts');
 let state={available:true,binding:'owned-binding',capabilities:{execution:true},pending:[]};
 const view={binding:'owned-binding',conversationDefault:false,conversation:{model:'p/a',effort:'high'},revision:'native-revision',models:[model]};
 const calls=[];let changed=0;
 const client=createRemoteExecutionClient(async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='RemoteRuntime')return state;
  if(method==='RemoteExecutionSettings')return view;
  return {id:args[0].id,outcome:'accepted'};
 },'owned-binding','Unknown original operation',()=>changed++);
 assert.deepEqual(await client.read(),view);
 await client.save('conversation',{...selection,approvalMode:'never'},view.revision);
 const command=calls.find(call=>call[0]==='ChangeRemoteExecutionSettings')[1];
 assert.deepEqual({...command,id:'stable-id'},{id:'stable-id',binding:'owned-binding',target:'conversation',expectedRevision:'native-revision',selection:{model:'p/a',effort:'high'}});
 assert.equal(changed,1);
 state={...state,pending:[{id:command.id,kind:'configure-execution'}]};
 await client.read(); // A matching model read cannot clear the original receipt.
 await assert.rejects(client.save('work',selection,view.revision),e=>e.unknown);
 assert.equal(calls.filter(call=>call[0]==='ChangeRemoteExecutionSettings').length,1);
 state={...state,binding:'new-binding',pending:[]};
 await assert.rejects(client.read());await assert.rejects(client.save('conversation',selection,view.revision));
 assert.equal(calls.filter(call=>call[0]==='ChangeRemoteExecutionSettings').length,1);
});

test('remote model response loss admits one original command and keeps unknown',async()=>{
 const {createRemoteExecutionClient}=await server.ssrLoadModule('/src/settings/runtime/remoteExecutionClient.ts');
 let calls=0,changed=0,pending=[];
 const client=createRemoteExecutionClient(async(method,...args)=>{
  if(method==='RemoteRuntime')return {available:true,binding:'binding',capabilities:{execution:true},pending};
  calls++;pending=[{id:args[0].id,kind:'configure-execution'}];throw new Error('response lost');
 },'binding','Unknown',()=>changed++);
 await assert.rejects(client.save('work',{model:'',effort:'',serviceTier:''},'revision'),e=>e.unknown);
 await assert.rejects(client.save('work',selection,'revision'),e=>e.unknown);
 assert.equal(calls,1);assert.equal(changed,1);
});

test('remote picker hides tier while local picker keeps its existing controls',async()=>{
 const React=await import('react');
 const {renderToStaticMarkup}=await import('react-dom/server');
 const {ModelPicker}=await server.ssrLoadModule('/src/settings/runtime/ModelPicker.tsx');
 const props={title:'Model',value:selection,models:[model],onSave:async()=>{},onClose:()=>{}};
 const local=renderToStaticMarkup(React.createElement(ModelPicker,props));
 const remote=renderToStaticMarkup(React.createElement(ModelPicker,{...props,value:{...selection,serviceTier:''},allowServiceTier:false,disabled:true}));
 assert.match(local,/aria-label="Response speed"/);
 assert.doesNotMatch(remote,/aria-label="Response speed"/);
 assert.match(remote,/aria-label="Reasoning effort"/);
 assert.match(remote,/class="primary" disabled=""/);
});
