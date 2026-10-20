import assert from 'node:assert/strict';
import { test, after } from 'node:test';
import { createServer } from 'vite';
import { readFileSync } from 'node:fs';
const server = await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});
after(()=>server.close());
const {createRuntimeSettingsClient} = await server.ssrLoadModule('/src/settings/runtime/client.ts');
const {setupStatusKey,botSessionStatusKey} = await server.ssrLoadModule('/src/settings/runtime/setup-status.ts');
const {connectModels}=await server.ssrLoadModule('/src/settings/runtime/connect-models.ts');
const {validCustomLimits,onboardingEffort,modelDescription,initialModelSelection,defaultModel, selectionSummary, changeModelParameters, fastTier, chooseModel, validSelection, groupLegacyModels, acceptConnectionProgress, safeWebURL} = await server.ssrLoadModule('/src/settings/runtime/state.ts');
const {createPreviewClient} = await server.ssrLoadModule('/src/settings/runtime/preview/client.ts');
// Finish module loading before registering tests; otherwise their after hook can
// close the SSR transport while a later top-level import is still in flight.
const {ModelSelectionDelivery}=await server.ssrLoadModule('/src/settings/runtime/model-selection.ts');
const model={model:'p/a',name:'A',description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[{id:'priority',name:'Fast',description:''}]};
const selection={model:'p/a',effort:'high',serviceTier:'priority'};

test('setup and session facts stay distinct when model or observation services fail',async()=>{
 const setup={state:'ready',installation:{installed:true},models:[],selectedModel:''};
 const client=createRuntimeSettingsClient(async method=>{
  if(method==='RuntimeSettings')return {runtime:'codex',cliPath:''};
  if(method==='SetupOverview')return {active:'codex',hasRuntimeChoice:true,pending:''};
  if(method==='InspectSetup')return setup;
  if(method==='ComposerSnapshot')return {connection:'offline',connectionIssue:'connection'};
  throw Error('service unavailable');
 });
 const view=await client.read();
 assert.equal(view.setup.state,'ready');
 assert.equal(view.hasRuntimeChoice,true);
 assert.deepEqual(view.session,{connection:'offline',issue:'connection'});
 assert.equal(view.conversation,null);assert.equal(view.work,null);assert.deepEqual(view.models,[]);
 assert.equal(setupStatusKey({...setup,state:'login'}),'runtime.loginCodex');
 assert.equal(setupStatusKey({...setup,state:'models'}),'runtime.noModelsConnected');
 assert.equal(setupStatusKey({...setup,installation:{installed:false}}),'runtime.notInstalled');
});

test('default Codex provider remains unselected when the session read fails',async()=>{
 const setup={state:'ready',installation:{installed:true},models:[],selectedModel:''};
 const calls=[];
 const client=createRuntimeSettingsClient(async method=>{
  calls.push(method);
  if(method==='RuntimeSettings')return {runtime:'codex',cliPath:''};
  if(method==='SetupOverview')return {active:'codex',hasRuntimeChoice:false,pending:'',onboarding:false};
  if(method==='InspectSetup')return setup;
  throw Error('synthetic service read failure');
 });
 const view=await client.read();
 assert.equal(view.activeRuntime,'codex');
 assert.equal(view.hasRuntimeChoice,false);
 assert.equal(view.session,null);
 assert.equal(botSessionStatusKey(view.setup,view.session,view.hasRuntimeChoice),'runtime.chooseConnection');
 assert.ok(calls.includes('ComposerSnapshot'));
 assert.equal(view.conversation,null);assert.equal(view.work,null);
});

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

// Runtime configuration is distinct from a catalog's recommended model.
test('default labels use actual configured identity; unreadable defaults stay generic',()=>{
 const inherited={model:'',effort:'',serviceTier:''};
 assert.equal(defaultModel([model]),undefined);
 assert.equal(selectionSummary(inherited,[model]).name,'默认');
 assert.deepEqual(selectionSummary(inherited,[model],undefined,selection),{name:'A',detail:'默认 · high · Fast'});
 assert.equal(selectionSummary(inherited,[model],undefined,{...selection,model:'missing'}).name,'默认');
});
test('editing inherited parameters pins the actual model and preserves other effective parameters',()=>{
 const inherited={model:'',effort:'',serviceTier:''};
 assert.deepEqual(changeModelParameters(inherited,model,{effort:'low'},selection),{model:'p/a',effort:'low',serviceTier:'priority'});
 assert.deepEqual(changeModelParameters(inherited,model,{serviceTier:''},selection),{model:'p/a',effort:'high',serviceTier:''});
 assert.equal(fastTier(model).id,'priority');
 assert.equal(fastTier({...model,serviceTiers:[]}),undefined);
 assert.deepEqual(changeModelParameters(selection,model,{effort:'low'}),{...selection,effort:'low'});
});

test('Magpie local URL UX accepts a changed port without exempting remote credentials',async()=>{
 const {localMagpie}=await server.ssrLoadModule('/src/settings/runtime/gateway.ts');
 for(const address of ['http://127.0.0.1:3425/v1','http://127.0.0.1:4888/v1','http://localhost:4888/v1','http://[::1]:4888/v1']) assert.equal(localMagpie('magpie',address),true,address);
 for(const address of ['https://example.com/v1','http://localhost.example.com/v1','http://127.0.0.1@example.com/v1','http://user@localhost/v1','file:///tmp/models','invalid']) assert.equal(localMagpie('magpie',address),false,address);
 assert.equal(localMagpie('custom','http://localhost:4888/v1'),false);
});

test('unbound role gets a local default draft without overwriting saved unavailable choices',()=>{
 const empty={model:'',effort:'',serviceTier:''};
 const model={model:'luna',defaultEffort:'medium',efforts:['medium'],serviceTiers:[{id:'fast',name:'Fast'}]};
 assert.deepEqual(initialModelSelection(empty,[model]),{model:'luna',effort:'medium',serviceTier:''});
 assert.equal(empty.model,'');
 assert.equal(initialModelSelection(empty,[]),empty);
 const saved={model:'offline',effort:'high',serviceTier:'fast'};
 assert.equal(initialModelSelection(saved,[model]),saved);
 assert.equal(fastTier(model).id,'fast');
});

test('multi-model connection waits for confirmed completion and stops on ambiguous result',async()=>{
 const calls=[],done=[];let finish;
 const client={startConnection:async(input,signal,progress)=>{calls.push(input.model);if(input.model==='a'){finish=()=>progress({id:'a',stage:'complete'});return {id:'a',stage:'preparing'};}return {id:'b',stage:'unknown'};}};
 const result=connectModels(client,{kind:'api-key',choice:'deepseek'},['a','b','c'],new AbortController().signal,()=>{},id=>done.push(id));
 await Promise.resolve();assert.deepEqual(calls,['a']);finish();
 assert.equal((await result).stage,'unknown');assert.deepEqual(calls,['a','b']);assert.deepEqual(done,['a']);
});
test('canceling multi-model connection never starts the next model',async()=>{
 const controller=new AbortController(),calls=[];
 const result=connectModels({startConnection:async(input)=>{calls.push(input.model);return {id:'a',stage:'preparing'};}},{kind:'api-key',choice:'deepseek'},['a','b'],controller.signal,()=>{},()=>{});
 controller.abort();await assert.rejects(result);assert.deepEqual(calls,['a']);
});

test('multi-model connection keeps confirmed items when a later dispatch rejects',async()=>{
 const calls=[],done=[];
 const client={startConnection:async(input)=>{calls.push(input.model);if(input.model==='b')throw Error('transport');return {id:'a',stage:'complete'};}};
 await assert.rejects(connectModels(client,{kind:'api-key',choice:'deepseek'},['a','b','c'],new AbortController().signal,()=>{},id=>done.push(id)));
 assert.deepEqual(calls,['a','b']);assert.deepEqual(done,['a']);
});

test('onboarding model capabilities use the selected provider endpoint and model without credentials',async()=>{
 const calls=[];
 const capability={...model,model:'xiaomi/mimo'};
 const profile={runtime:'caelis',cliPath:''};
 const client=createRuntimeSettingsClient(async(method,...args)=>{calls.push([method,...args]);return [{value:capability.model,parameters:capability}];},profile);
 assert.deepEqual(await client.modelParameters('xiaomi','https://token-plan.example/v1',capability.model),capability);
 assert.deepEqual(calls,[['SetupCatalog',{settings:profile,action:'model-parameters',provider:'xiaomi',baseUrl:'https://token-plan.example/v1',model:capability.model,apiKey:''}]]);
});

test('onboarding picks a supported middle effort, preferring medium',()=>{
 assert.equal(onboardingEffort(['ultra','low','medium','high']), 'medium');
 assert.equal(onboardingEffort(['none','low','high','max']), 'high');
 assert.equal(onboardingEffort(['none','high']), 'high');
 assert.equal(onboardingEffort(['low']), 'low');
 assert.equal(onboardingEffort([]), '');
 assert.equal(onboardingEffort(['','medium','medium']), 'medium');
});
test('model descriptions omit repeated names and preserve genuine descriptions',()=>{
 assert.equal(modelDescription('gpt-6-sol','GPT-6-sol'), undefined);
 assert.equal(modelDescription('', 'Model'), undefined);
 assert.equal(modelDescription('native preset · reasoning · tools', 'Model'), undefined);
 assert.equal(modelDescription('Supports image input', 'Model'), 'Supports image input');
});

test('custom limits require explicit positive safe token counts',()=>{
 assert.equal(validCustomLimits('128000','8192'),true);
 for(const value of ['', '0', '-1', '1.5', 'NaN', 'Infinity']) assert.equal(validCustomLimits(value,'8192'),false);
 assert.equal(validCustomLimits('128000',''),false);
});

test('model choice stays editable across a lost acknowledgement and newer choices win',async()=>{
 let release;const blocked=new Promise(resolve=>{release=resolve});
 const calls=[];let first=true;
 const delivery=new ModelSelectionDelivery(async(method,scope,runtime,selection)=>{
  calls.push([method,scope,runtime,selection.model]);
  if(first){first=false;await blocked;throw Error('lost transport response');}
 },async()=>{});
 delivery.save('conversation','codex',{model:'first',effort:'',serviceTier:''});
 delivery.save('conversation','caelis',{model:'latest',effort:'',serviceTier:''});
 assert.equal(delivery.snapshot()[0].selection.model,'latest');
 assert.equal(delivery.snapshot()[0].state,'saving');
 release();
 for(let i=0;i<20&&delivery.snapshot().length;i++)await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(calls.map(v=>v[3]),['first','latest']);
 assert.deepEqual(delivery.snapshot(),[]);
});

test('reads begun before a model choice or its acknowledgement cannot overwrite it',async()=>{
 let complete;const delivery=new ModelSelectionDelivery(()=>new Promise(resolve=>{complete=resolve}));
 const before=delivery.version();delivery.save('conversation','codex',{model:'new',effort:'',serviceTier:''});
 assert.notEqual(delivery.version(),before);const saving=delivery.version();
 assert.equal(delivery.snapshot()[0].selection.model,'new');complete();
 await new Promise(resolve=>setImmediate(resolve));assert.notEqual(delivery.version(),saving);assert.deepEqual(delivery.snapshot(),[]);
});
