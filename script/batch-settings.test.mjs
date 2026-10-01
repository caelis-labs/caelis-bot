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
const server=await createServer({configLoader:'runner',cacheDir:'.cache/batch-test-vite',server:{middlewareMode:true,ws:false},appType:'custom'});
const {makeBatchTemplate,applyBatchTemplate,publicConnectionTemplate}=await server.ssrLoadModule('/src/settings/runtime/batch.ts');
const {createNodeSettingsClient}=await server.ssrLoadModule('/src/settings/runtime/nodeClient.ts');
const {NodeRuntimeSettings}=await server.ssrLoadModule('/src/NodeRuntimeSettings.tsx');
let root,container;
afterEach(async()=>{if(root)await act(async()=>root.unmount());container?.remove();root=null;});
after(async()=>{await server.close();dom.window.close();});
const selection={model:'provider/vendor/model',effort:'high',serviceTier:'priority'};
const model=(id=selection.model)=>({model:id,name:'Model',description:'',default:true,defaultEffort:'high',efforts:['high','low'],serviceTiers:[{id:id===selection.model?'priority':'fast',name:'Fast',description:''}]});
function view(id='source',backend='caelis'){
 return {profile:{runtime:backend,cliPath:`/machines/${id}/cli`,caelisStore:`/machines/${id}/store`},revision:'1',setup:{state:'ready',settings:{runtime:backend},installation:{installed:true,version:'1.0'},models:[],message:''},pending:'',models:[model()],conversation:{...selection,approvalMode:'auto_review'},work:{...selection},main:{...selection},canEditMain:true,connections:[],team:{available:true,revision:'1',roles:[{id:'orbit',description:'Plan',custom:false,system:false,selection:{model:`profile-${id}`,effort:'high',serviceTier:'fast'},inherited:false,modelIds:[`profile-${id}`]},{id:'reviewer',description:'Review changes',custom:true,system:false,selection:{model:'',effort:'',serviceTier:''},inherited:true,modelIds:[`profile-${id}`]}],sets:[],activeSet:'',models:[model(`profile-${id}`)],modelBindings:[{profileId:`profile-${id}`,selector:selection.model}]}};
}
const options={conversation:true,work:true,main:true,team:true,connection:false,teamSet:'Shared'};
function fixture({backend='caelis',authRequired=[],mutate,customize,connectionAuth=true}={}){
 const nodes=['source','one','two','unready'].map(id=>({id,label:id,join:'outgoing',os:'linux',runtimes:[{backend,version:'1.0',health:'healthy',authentication:authRequired.includes(id)?'required':'authenticated',roles:[]}]}));
 const catalog={revision:'registry',nodes,selectedNodeId:'source',activeBotNodeId:'source',pendingOperations:[]};
 const views=new Map(nodes.map(node=>[node.id,view(node.id,backend)])),calls=[],changes=[];
 customize?.(views);
 const invoke=async(method,...args)=>{
  calls.push([method,...args]);
  if(method==='NodeCatalog')return structuredClone(catalog);
  if(method==='NodeRuntimeConfiguration'){
   const [id,runtime]=args,v=views.get(id);
   return {guard:{nodeId:id,backend:runtime,revision:`guard-${id}-${v.revision}`},configurationAvailable:true,installation:{installed:true,version:'1.0',latestVersion:'1.0'},conversation:v.conversation,worker:v.work,configuration:{revision:v.revision,models:v.models,main:v.main,team:v.team,connections:[]}};
  }
  if(method==='ChangeNodeConfiguration'){
   const request=args[0],v=views.get(request.guard.nodeId);changes.push(request);
   const overridden=await mutate?.(request,v);if(overridden)return overridden;
   assert.equal(request.guard.revision,`guard-${request.guard.nodeId}-${v.revision}`);
   assert.equal(request.change.expectedRevision,v.revision);
   const change=request.change;
   if(change.action==='create-role')v.team.roles.push({id:change.id,description:change.description,custom:true,system:false,selection:{model:'',effort:'',serviceTier:''},inherited:true,modelIds:[`profile-${request.guard.nodeId}`]});
   v.revision=String(Number(v.revision)+1);v.team.revision=v.revision;
   return {ref:request.ref,outcome:'committed',message:'',revision:v.revision};
  }
  if(method==='BeginNodeRuntimeConnection')return {nodeId:args[0].nodeId,backend:args[0].backend,operationId:args[1]};
  if(method==='NodeRuntimeConnectionCatalog')return {choices:[{id:'vendor',name:'Vendor',description:'',custom:false}],unavailable:''};
  if(method==='NodeRuntimeSetupCatalog')return args[1]==='endpoints'?[{value:'https://example.com/v1',label:'Vendor',noAuth:connectionAuth}]:[{value:'model',label:'Model'}];
  if(method==='StartNodeRuntimeConnection')return {id:`connection-${args[0].nodeId}`,revision:'1',sequence:1,stage:'complete',title:'',message:''};
  if(method==='CloseNodeRuntimeConnection')return;
  if(method==='ReconcileNodeOperation')return {ref:args[0],outcome:'committed',message:'',revision:'2'};
  // Existing node setup read-only observations are unrelated to configuration.
  if(method==='NodeRoamingSettings')return {revision:1,nodes:[],issue:''};
  return {};
 };
 return {owner:createNodeSettingsClient(invoke),calls,changes,views,catalog};
}
const targets=(f,...ids)=>f.catalog.nodes.filter(node=>ids.includes(node.id));
const collect=()=>{const results=new Map();return {results,report:result=>results.set(result.nodeId,result)};};
test('one snapshot applies to two targets with target revisions and semantic Team profiles, retaining machine paths',async()=>{
 const f=fixture({authRequired:['unready'],customize:views=>{views.get('one').team.roles.pop();}}),c=collect();
 const template=makeBatchTemplate(view(),options);
 assert.equal(JSON.stringify(template).includes('profile-source'),false);
 assert.equal(JSON.stringify(template).includes('/machines/'),false);
 await applyBatchTemplate(f.owner,template,targets(f,'one','two','unready'),c.report);
 for(const id of ['one','two']){
  assert.equal(c.results.get(id).outcome,'committed');
  const bind=f.changes.find(request=>request.guard.nodeId===id&&request.change.action==='bind');
  assert.equal(bind.change.selection.model,`profile-${id}`);
  assert.equal(f.views.get(id).profile.cliPath,`/machines/${id}/cli`);
  assert.equal(f.views.get(id).profile.caelisStore,`/machines/${id}/store`);
  assert.equal(f.changes.filter(request=>request.guard.nodeId===id).at(-1).change.action,'save-set');
 }
 assert.equal(c.results.get('unready').reason,'authentication');
 assert.equal(c.results.get('unready').outcome,'needs-initialization');
 assert.equal(f.changes.some(request=>request.guard.nodeId==='unready'),false);
 assert.equal(f.calls.some(([method])=>/SaveRuntimeSettings|Activate|Install|StartBot|StartWorker/.test(method)),false);
 assert.equal(f.changes.some(request=>JSON.stringify(request).includes('/machines/')),false);
});
test('Codex copies only Bot and Worker preferences, including effort and tier, to at least two targets',async()=>{
 const f=fixture({backend:'codex'}),c=collect();
 f.catalog.nodes.find(node=>node.id==='two').join='local';f.catalog.activeBotNodeId='two';
 await applyBatchTemplate(f.owner,makeBatchTemplate(view('source','codex'),{...options,team:false}),targets(f,'one','two'),c.report);
 assert.deepEqual([...c.results.values()].map(result=>result.outcome),['committed','committed']);
 assert.deepEqual(f.changes.map(request=>request.change.action),['conversation-model','worker-model','conversation-model','worker-model']);
 for(const request of f.changes)assert.deepEqual(request.change.selection,selection);
});
test('ambiguous semantic mapping or unsupported model capability fails target preflight without writes',async()=>{
 const f=fixture({customize:views=>{views.get('one').team.modelBindings.push({profileId:'another-profile',selector:selection.model});views.get('two').models[0].efforts=['low'];}}),c=collect();
 await applyBatchTemplate(f.owner,makeBatchTemplate(view(),options),targets(f,'one','two'),c.report);
 assert.equal(c.results.get('one').reason,'mapping');assert.equal(c.results.get('two').reason,'model');assert.equal(f.changes.length,0);
});
test('unknown write keeps the original receipt, stops this target, and allows the next target; no retry',async()=>{
 const f=fixture({backend:'codex',mutate:request=>{if(request.guard.nodeId==='one'&&request.change.action==='worker-model')throw new Error('lost reply');}}),c=collect();
 await applyBatchTemplate(f.owner,makeBatchTemplate(view('source','codex'),{...options,team:false}),targets(f,'one','two'),c.report);
 assert.equal(c.results.get('one').outcome,'unknown');assert.deepEqual(c.results.get('one').applied,['conversation']);assert.equal(c.results.get('two').outcome,'committed');
 assert.equal(f.changes.filter(request=>request.guard.nodeId==='one').length,2);
 const ref=f.owner.pending('one','codex');assert.ok(ref);
 await f.owner.reconcile('one','codex');assert.deepEqual(f.calls.at(-1),['ReconcileNodeOperation',ref]);
});
test('rejected later writes report partial success and retain earlier writes',async()=>{
 const f=fixture({backend:'codex',mutate:request=>request.change.action==='worker-model'?{ref:request.ref,outcome:'rejected',message:''}:undefined}),c=collect();
 await applyBatchTemplate(f.owner,makeBatchTemplate(view('source','codex'),{...options,team:false}),targets(f,'one'),c.report);
 assert.equal(c.results.get('one').outcome,'partial');assert.deepEqual(c.results.get('one').applied,['conversation']);assert.equal(f.changes.length,2);
});
test('connection templates contain only nonsecret fields and reject any URL userinfo, query or fragment',()=>{
 const input={kind:'api-key',choice:'vendor',baseUrl:'https://example.com/v1',model:'model',apiKey:'secret-sentinel',command:'private-command',settings:{cliPath:'private-path'},reasoningLevels:['high'],imageInput:true,contextWindowTokens:8192,maxOutputTokens:1024};
 const template=publicConnectionTemplate(input);
 assert.ok(template);assert.equal(JSON.stringify(template).includes('secret'),false);assert.equal('settings' in template,false);assert.equal('command' in template,false);
 for(const baseUrl of ['https://user:pass@example.com/v1','https://example.com/v1?key=secret','https://example.com/v1#secret','file:///private/path'])assert.equal(publicConnectionTemplate({...input,baseUrl}),undefined);
});
test('connection application reuses each target auth, lists missing auth without start, and keeps ready targets',async()=>{
 const f=fixture(),c=collect();
 const template=makeBatchTemplate(view(),{...options,conversation:false,work:false,main:false,team:false,connection:true},publicConnectionTemplate({kind:'api-key',choice:'vendor',baseUrl:'https://example.com/v1',model:'model'}));
 await applyBatchTemplate(f.owner,template,targets(f,'one','two'),c.report);
 assert.deepEqual([...c.results.values()].map(result=>result.outcome),['committed','committed']);
 const starts=f.calls.filter(([method])=>method==='StartNodeRuntimeConnection');assert.equal(starts.length,2);
 for(const [,ref,input] of starts){assert.ok(['one','two'].includes(ref.nodeId));assert.equal('apiKey' in input,false);assert.equal(input.settings,null);assert.equal('command' in input,false);}
 const missing=fixture({connectionAuth:false}),other=collect();await applyBatchTemplate(missing.owner,template,targets(missing,'one'),other.report);
 assert.equal(other.results.get('one').reason,'authentication');assert.equal(missing.calls.some(([method])=>method==='StartNodeRuntimeConnection'),false);
});
test('existing page exposes node selection and per-node partial results through a restrained dialog',async()=>{
 const f=fixture({authRequired:['unready']});
 container=document.createElement('div');document.body.append(container);root=createRoot(container);
 await act(async()=>root.render(React.createElement(NodeRuntimeSettings,{client:f.owner,call:async()=>({revision:1,nodes:[],issue:''})})));
 const button=()=>[...container.querySelectorAll('button')].find(value=>value.textContent==='Apply to selected nodes');
 assert.ok(button());await act(async()=>button().click());
 const dialog=container.querySelector('[role="dialog"]');assert.ok(dialog);
 const nodeInputs=[...dialog.querySelectorAll('fieldset')].at(-1).querySelectorAll('input');assert.equal(nodeInputs.length,3);
 for(const input of nodeInputs)await act(async()=>input.click());
 await act(async()=>[...dialog.querySelectorAll('button')].find(value=>value.textContent==='Apply to 3 nodes').click());
 for(let attempt=0;attempt<30&&!dialog.querySelector('.runtime-batch-results')?.textContent.includes('unready · Needs setup');attempt++)await act(async()=>{await new Promise(resolve=>setTimeout(resolve,10));});
 assert.match(dialog.textContent,/one · Applied/);assert.match(dialog.textContent,/two · Applied/);assert.match(dialog.textContent,/unready · Needs setup/);assert.match(dialog.textContent,/needs authentication/);
 assert.equal(f.changes.filter(request=>request.guard.nodeId==='source').length,0);
});
