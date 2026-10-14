import assert from 'node:assert/strict';
import { test, after } from 'node:test';
import { createServer } from 'vite';
import { readFileSync } from 'node:fs';
const server = await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});
after(()=>server.close());
const {createRuntimeSettingsClient} = await server.ssrLoadModule('/src/settings/runtime/client.ts');
const {canApplyInstalledService,setupStatusKey,botSessionStatusKey,nextOnboardingStep,setupFailureKind} = await server.ssrLoadModule('/src/settings/runtime/setup-status.ts');
const {defaultModel, selectionSummary, changeModelParameters, fastTier, chooseModel, validSelection, groupLegacyModels, acceptConnectionProgress, safeWebURL, executionMethodLabel} = await server.ssrLoadModule('/src/settings/runtime/state.ts');
const {displayConnectionMessage} = await server.ssrLoadModule('/src/connection-message.ts');
const {createPreviewClient} = await server.ssrLoadModule('/src/settings/runtime/preview/client.ts');
const {serialLatest} = await server.ssrLoadModule('/src/settings/runtime/worker-refresh.ts');
const {connectSelectedModels} = await server.ssrLoadModule('/src/settings/runtime/connection-sequence.ts');
const {readSelectedWorker} = await server.ssrLoadModule('/src/settings/runtime/worker-selection.ts');
const {createModeReadback} = await server.ssrLoadModule('/src/settings/runtime/mode-readback.ts');
const {createResourceReadback} = await server.ssrLoadModule('/src/settings/runtime/resource-readback.ts');
const {verifiedSetup,connectionReadiness,workerCardState,botCardState} = await server.ssrLoadModule('/src/settings/runtime/readiness.ts');
const {workerDisplayError} = await server.ssrLoadModule('/src/settings/runtime/worker-selection.ts');
const model={model:'p/a',name:'A',description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[{id:'priority',name:'Fast',description:''}]};
const selection={model:'p/a',effort:'high',serviceTier:'priority'};

test('saved Worker preference is distinct from available work in both directions',()=>{
 assert.equal(workerCardState('codex','codex',false),'saved-waiting');
 assert.equal(workerCardState('codex','codex',undefined),'saved-waiting');
 assert.equal(workerCardState('codex','codex',true),'selected-ready');
 assert.equal(workerCardState('caelis','codex',true),'ready');
 assert.equal(workerCardState('caelis','codex',false),'setup');
 assert.equal(botCardState('caelis','codex','caelis'),'pending');
 assert.equal(botCardState('codex','codex','caelis'),'current');
 assert.equal(botCardState('caelis','','caelis'),'pending');
 assert.equal(botCardState('codex','','caelis'),'other');
});

test('connected catalog models are not chat-ready without Core qualification',()=>{
 const base={state:'ready',installation:{installed:true},models:[]};
 const registered={value:'synthetic',noAuth:true,chatEligibility:'chat'};
 const unknown={...registered,noAuth:false,chatEligibility:'unknown'};
 const nonchat={...unknown,chatEligibility:'non-chat'};
 const chat={...unknown,chatEligibility:'chat'};
 for(const models of [[registered],[unknown],[nonchat],[{value:'legacy',noAuth:false}]]) {
  const state={...base,models};
  assert.equal(verifiedSetup('caelis',state).state,'models');
  assert.notEqual(connectionReadiness('caelis',state),'ready');
 }
 assert.equal(connectionReadiness('caelis',{...base,models:[registered]}),'auth_needed');
 assert.equal(connectionReadiness('caelis',{...base,models:[unknown]}),'chat_unverified');
 assert.equal(connectionReadiness('caelis',{...base,models:[nonchat]}),'no_chat_model');
 assert.equal(connectionReadiness('caelis',{...base,models:[]}), 'no_models');
 assert.equal(connectionReadiness('caelis',{...base,installation:{installed:false}}),'not_installed');
 assert.equal(connectionReadiness('caelis',null),'unknown');
 assert.equal(verifiedSetup('caelis',{...base,models:[chat]}).state,'ready');
 assert.equal(connectionReadiness('caelis',{...base,models:[chat]}),'ready');
 assert.equal(connectionReadiness('caelis',{...base,state:'models',models:[chat]}),'unavailable');
 assert.equal(setupStatusKey({...base,state:'models',models:[unknown]}),'runtime.chatUnverified');
 assert.equal(setupStatusKey({...base,state:'models',models:[nonchat]}),'runtime.noChatModel');
 assert.equal(connectionReadiness('codex',base),'ready');
 assert.equal(connectionReadiness('codex',{...base,state:'login'}),'unavailable');
});

test('known native model warning uses localized user-facing connection name',()=>{
 const raw=Error('连接 Codex 后可加载模型；请在对话窗口检查连接');
 const en=JSON.parse(readFileSync('internal/i18n/locales/en/settings.json','utf8'));
 const zh=JSON.parse(readFileSync('internal/i18n/locales/zh-CN/settings.json','utf8'));
 for(const catalog of [en,zh]) {
  const shown=workerDisplayError(raw,key=>catalog[key.split('.')[1]]);
  assert.match(shown,/ChatGPT/);
  assert.doesNotMatch(shown,/Codex/);
 }
 assert.equal(workerDisplayError(Error('opaque Host detail'),()=>''),'opaque Host detail');
});

test('slow Worker inspection coalesces refreshes and always clears busy',async()=>{
 let release;const delayed=new Promise(resolve=>{release=resolve;});
 const calls=[],busy=[];
 const schedule=serialLatest(async request=>{calls.push(request);if(request==='old')await delayed;},value=>busy.push(value));
 schedule('old');schedule('superseded');schedule('latest');
 assert.deepEqual(calls,['old']);assert.deepEqual(busy,[true]);
 release();await delayed;await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(calls,['old','latest']);assert.deepEqual(busy,[true,false]);
});
test('failed Worker inspection still runs queued refresh and releases busy',async()=>{
 const calls=[],errors=[],busy=[];
 let release;const delayed=new Promise(resolve=>{release=resolve;});
 const schedule=serialLatest(async request=>{calls.push(request);if(request==='old'){await delayed;throw Error('fixture failure');}},value=>busy.push(value),error=>errors.push(error.message));
 schedule('old');schedule('latest');release();await delayed;await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(calls,['old','latest']);assert.deepEqual(errors,['fixture failure']);assert.deepEqual(busy,[true,false]);
});

test('failed native Worker selection read never substitutes an inspectable Codex or cached value',async()=>{
 const calls=[];
 const call=async(method,...args)=>{calls.push([method,...args]);if(method==='SelectedLocalWorker')throw Error('native default unavailable');return {runtime:'codex',ready:true};};
 await assert.rejects(readSelectedWorker(call),/native default unavailable/);
 assert.deepEqual(calls,[['SelectedLocalWorker']]);
 const selected=async(method,...args)=>{calls.push([method,...args]);if(method==='SelectedLocalWorker')return 'caelis';return {runtime:'codex',ready:true};};
 await assert.rejects(readSelectedWorker(selected,'codex'),/did not commit/);
 assert.deepEqual(calls.slice(1),[['SelectLocalWorker','codex'],['SelectedLocalWorker']]);
});

test('Bot mode save supersedes an older refresh and accepts only the authoritative readback',async()=>{
 let releaseOld,releaseSave;
 const old=new Promise(resolve=>{releaseOld=resolve;}),write=new Promise(resolve=>{releaseSave=resolve;});
 const values=[],errors=[],gate=createModeReadback();
 const stale=gate.read(()=>old,value=>values.push(value),error=>errors.push(error));
 const saving=gate.save(()=>write,()=>Promise.resolve('full-access'),value=>values.push(value),error=>errors.push(error));
 const racing=gate.read(()=>old,value=>values.push(value),error=>errors.push(error));
 releaseSave();await saving;
 releaseOld('auto');await Promise.all([stale,racing]);
 assert.deepEqual(values,['full-access']);assert.deepEqual(errors,[]);
});

test('uncertain Bot mode write still reads authoritative state before reporting the error',async()=>{
 const values=[],gate=createModeReadback();
 await assert.rejects(gate.save(async()=>{throw Error('response lost');},async()=> 'auto',value=>values.push(value),()=>{}),/response lost/);
 assert.deepEqual(values,['auto']);
});

test('successful Bot full save with auto readback keeps the confirmation pending',async()=>{
 const values=[],errors=[],gate=createModeReadback(),mismatch=Error('full mode was not confirmed');
 let writes=0,reads=0;
 await assert.rejects(gate.save(async()=>{writes++;},async()=>{reads++;return 'auto';},value=>values.push(value),error=>errors.push(error),{mode:'full-access',error:mismatch}),error=>error===mismatch);
 assert.equal(writes,1);assert.equal(reads,1);
 assert.deepEqual(values,['auto']);assert.deepEqual(errors,[]);
});

test('successful Bot full save with failed readback cannot confirm the requested mode',async()=>{
 const values=[],errors=[],gate=createModeReadback(),readError=Error('readback unavailable');
 let writes=0,reads=0;
 await assert.rejects(gate.save(async()=>{writes++;},async()=>{reads++;throw readError;},value=>values.push(value),error=>errors.push(error),{mode:'full-access',error:Error('mismatch')}),error=>error===readError);
 assert.equal(writes,1);assert.equal(reads,1);
 assert.deepEqual(values,[]);assert.deepEqual(errors,[readError]);
});

test('resource read failure is an error, not an empty directory; a later empty result is authoritative',async()=>{
 const states=[],reader=createResourceReadback();
 await reader.load(async()=>{throw Error('Host unavailable');},state=>states.push(state));
 assert.equal(states.at(-1).status,'error');assert.equal(states.at(-1).value,null);
 await reader.load(async()=>({connections:[]}),state=>states.push(state));
 assert.equal(states.at(-1).status,'ready');assert.deepEqual(states.at(-1).value.connections,[]);
});

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
test('a schema-incompatible running Host requires an upgrade, not activation of the same installed binary',()=>{
 const installed={installed:true,updateState:'current'};
 const old={state:'incompatible',installation:installed,serviceUpdateAvailable:false};
 assert.equal(canApplyInstalledService(old),false);
 assert.equal(canApplyInstalledService({...old,serviceUpdateAvailable:true}),true);
 assert.equal(canApplyInstalledService({...old,state:'service'}),true);
 assert.equal(canApplyInstalledService({...old,installation:{installed:false}}),false);
 assert.equal(setupStatusKey(old),'runtime.incompatibleConnection');
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
test('selected provider models connect sequentially and stop on an unknown receipt',async()=>{
 const calls=[],completed=[];
 const client={startConnection:async(input,_signal,progress)=>{
  calls.push(input);
  const state=calls.length===2?'unknown':'complete';
  const flow={id:`flow-${calls.length}`,revision:'1',sequence:1,stage:state,title:'',message:''};
  progress(flow);return flow;
 }};
 const result=await connectSelectedModels(client,{kind:'api-key',choice:'fixture',baseUrl:'https://example.invalid/v1',apiKey:'private-sentinel'},['chat-a','chat-b','chat-c'],new AbortController().signal,()=>{},count=>completed.push(count));
 assert.equal(result.stage,'unknown');
 assert.deepEqual(calls.map(call=>call.model),['chat-a','chat-b']);
 assert.deepEqual(completed,[1]);
 assert.ok(calls.every(call=>call.apiKey==='private-sentinel'));
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
