import assert from 'node:assert/strict';
import { test, after } from 'node:test';
import { createServer } from 'vite';
import { readFileSync } from 'node:fs';
const server = await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});
after(()=>server.close());
const {createRuntimeSettingsClient} = await server.ssrLoadModule('/src/settings/runtime/client.ts');
const {setupStatusKey,botSessionStatusKey,nextOnboardingStep,setupFailureKind} = await server.ssrLoadModule('/src/settings/runtime/setup-status.ts');
const {defaultModel, selectionSummary, changeModelParameters, fastTier, chooseModel, validSelection, groupLegacyModels, acceptConnectionProgress, safeWebURL, executionMethodLabel} = await server.ssrLoadModule('/src/settings/runtime/state.ts');
const {displayConnectionMessage} = await server.ssrLoadModule('/src/connection-message.ts');
const {createPreviewClient} = await server.ssrLoadModule('/src/settings/runtime/preview/client.ts');
const model={model:'p/a',name:'A',description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[{id:'priority',name:'Fast',description:''}]};
const selection={model:'p/a',effort:'high',serviceTier:'priority'};

test('execution labels are local copy only; native selectors and unknown IDs stay intact',()=>{
 const en=JSON.parse(readFileSync('internal/i18n/locales/en/runtime.json','utf8'));
 const zh=JSON.parse(readFileSync('internal/i18n/locales/zh-CN/runtime.json','utf8'));
 for(const [catalog, expected] of [[en,['Default','Use ChatGPT']],[zh,['默认','使用 ChatGPT']]]) {
  const label=key=>catalog[key.split('.')[1]];
  assert.deepEqual(['caelis','codex'].map(id=>executionMethodLabel(id,label)),expected);
  assert.equal(executionMethodLabel('unknown-provider',label),'unknown-provider');
 }
});

test('known native review errors have bilingual display copy while unknown Host detail remains exact',()=>{
 const original='Caelis Guardian 模型不可用，请在运行时连接设置中恢复该模型；不会转为免审批或人工放行';
 for(const [locale, expected] of [['zh-CN','审批模型不可用，请在连接与模型中恢复该模型；不会跳过审批。'],['en','The review model is unavailable. Restore it in Connections & models; reviews will not be skipped.']]) {
  const catalog=JSON.parse(readFileSync(`internal/i18n/locales/${locale}/chat.json`,'utf8'));
  assert.equal(displayConnectionMessage(original,key=>catalog[key.split('.')[1]]),expected);
 }
 const unknown='provider x returned 401 for model p/a';
 assert.equal(displayConnectionMessage(unknown,()=>''),unknown);
});

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
test('a live shared session remains usable when its CLI file disappears',async()=>{
 const setup={state:'missing',installation:{installed:false},models:[],selectedModel:''};
 const client=createRuntimeSettingsClient(async method=>{
  if(method==='RuntimeSettings')return {runtime:'codex',cliPath:'/isolated/removed-cli'};
  if(method==='SetupOverview')return {active:'codex',hasRuntimeChoice:true,pending:''};
  if(method==='ComposerSnapshot')return {connection:'ready',connectionIssue:''};
  if(method==='InspectSetup')return setup;
  if(method==='ExecutionSettings')return {model:'existing-model',effort:'',serviceTier:''};
  if(method==='WorkExecutionSettings')return {model:'existing-model',effort:'',serviceTier:''};
  if(method==='Models')return [];
  throw Error(`unexpected call ${method}`);
 });
 const view=await client.read();
 assert.equal(view.setup.installation.installed,false);
 assert.equal(view.conversation.model,'existing-model');
 assert.equal(botSessionStatusKey(view.setup,view.session,true),'runtime.connected');
 assert.equal(botSessionStatusKey(view.setup,{connection:'offline'},true),'runtime.notInstalled');
});
test('an acknowledged cancellation is not reported as a network failure',()=>{
 assert.equal(setupFailureKind('download connection interrupted',true),'cancelled');
 assert.equal(setupFailureKind('download connection interrupted',false),'failed');
 assert.equal(setupFailureKind('The operation has not been confirmed yet.',false),'unknown');
});
test('first run advances from missing CLI through service to a real model connection',()=>{
 const state={installation:{installed:false},state:'missing'};
 assert.equal(nextOnboardingStep(state,'caelis'),'install');
 assert.equal(nextOnboardingStep({...state,installation:{installed:true},state:'service'},'caelis'),'start');
 assert.equal(nextOnboardingStep({...state,installation:{installed:true},state:'models'},'caelis'),'model');
 assert.equal(nextOnboardingStep({...state,installation:{installed:true},state:'ready'},'caelis'),'');
 assert.equal(nextOnboardingStep({...state,installation:{installed:true},state:'login'},'codex'),'');
});
test('stop using a sole model sends one revision-fenced native transaction; uncertain result is not replayed',async()=>{
 const group={id:'provider',name:'Provider',kind:'provider',detail:'',models:[]};
 const model={id:'provider/a',name:'A',uses:['Caelis 主模型','Bot 对话','独立工作模型'],unavailable:false};
 group.models=[model];const calls=[];
 const client=createRuntimeSettingsClient(async(method,...args)=>{
  calls.push([method,...args]);
  return {operationId:'op',outcome:'committed',message:''};
 });
 await assert.rejects(client.removeModel(group,model,'9'));
 await client.removeModel(group,model,'9',true);
 assert.deepEqual(calls,[['ChangeRuntimeConfiguration',{action:'remove-model-and-stop',id:model.id,expectedRevision:'9'}]]);
 const failed=[];const partial=createRuntimeSettingsClient(async(method,...args)=>{failed.push([method,...args]);return {operationId:'op2',outcome:'unknown',message:'refresh before retry'};});
 await assert.rejects(partial.removeModel(group,model,'9',true),e=>e.unknown&&e.receipt.operationId==='op2');
 assert.equal(failed.length,1);
 const team={...model,uses:[...model.uses,'orbit']};
 await assert.rejects(client.removeModel({...group,models:[team]},team,'9',true),/connection_in_use/);
 assert.equal(calls.length,1);
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
test('gateway model catalog failure keeps Host endpoint and manual model fallback',async()=>{
 const profile={runtime:'caelis',cliPath:'/fixture/caelis',caelisStore:'/fixture/store'};
 const client=createRuntimeSettingsClient(async(method,request)=>{
  if(method==='SetupCatalog' && request.action==='endpoints')return [{value:'http://127.0.0.1:3425/v1',label:'Local Magpie',noAuth:true}];
  if(method==='SetupCatalog' && request.action==='models')throw Error('synthetic gateway offline');
  throw Error('unexpected call');
 },profile);
 const options=await client.apiKeyOptions('magpie','');
 assert.deepEqual(options.models,[]);assert.equal(options.endpoints[0].reuseAuth,true);
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
 assert.deepEqual(selectionSummary(inherited,[model],undefined,selection),{name:'A',detail:'默认 · 高 · Fast'});
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
