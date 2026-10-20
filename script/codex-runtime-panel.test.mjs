import assert from 'node:assert/strict';
import {test,after} from 'node:test';
import {createServer} from 'vite';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
const server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});
after(()=>server.close());
const {CodexRuntimePanel}=await server.ssrLoadModule('/src/settings/runtime/CodexRuntimePanel.tsx');
const {BuiltinRuntimePanel}=await server.ssrLoadModule('/src/settings/runtime/BuiltinRuntimePanel.tsx');
const {TeamSettings}=await server.ssrLoadModule('/src/settings/runtime/TeamSettings.tsx');
const {ConnectionsPage}=await server.ssrLoadModule('/src/settings/runtime/ConnectionsPage.tsx');
const {Recovery}=await server.ssrLoadModule('/src/SurfaceBoundary.tsx');

test('contextual errors preserve the actual failure and have only an explicitly supplied retry',()=>{
 const failed=renderToStaticMarkup(React.createElement(Recovery,{message:'Model was removed during selection'}));
 assert.match(failed,/Model was removed during selection/);
 assert.doesNotMatch(failed,/<button|surface-recovery|Copy feedback|Reopen window/);
 const retry=renderToStaticMarkup(React.createElement(Recovery,{message:'Could not read models',onRetry:()=>{}}));
 assert.equal((retry.match(/<button/g)||[]).length,1);
 assert.match(retry,/Try again/);
 assert.doesNotMatch(retry,/Copy feedback|Reopen window|message-row/);
});

test('renderer recovery retries once, then offers reopen without a generic feedback card',async(t)=>{
 const {SurfaceBoundary}=await server.ssrLoadModule('/src/SurfaceBoundary.tsx');
 t.mock.timers.enable({apis:['setTimeout']});
 const boundary=new SurfaceBoundary({children:'original surface'});
 boundary.setState=patch=>Object.assign(boundary.state,patch);
 Object.assign(boundary.state,SurfaceBoundary.getDerivedStateFromError());
 boundary.componentDidCatch(new Error('PRIVATE_SENTINEL'));
 assert.equal(boundary.render(),null);
 t.mock.timers.tick(300);
 assert.equal(boundary.render(),'original surface');
 Object.assign(boundary.state,SurfaceBoundary.getDerivedStateFromError());
 boundary.componentDidCatch(new Error('PRIVATE_SENTINEL'));
 const recovery=renderToStaticMarkup(boundary.render());
 assert.doesNotMatch(recovery,/Copy feedback/);
 assert.match(recovery,/Reopen window/);
 assert.doesNotMatch(recovery,/PRIVATE_SENTINEL|Reconnect|stack/i);
 t.mock.timers.tick(30000);
 assert.equal(boundary.state.fatal,true);
 boundary.componentWillUnmount();
});
test('model summaries start in an explicit loading state before any runtime facts arrive',()=>{
 const html=renderToStaticMarkup(React.createElement(ConnectionsPage,{active:false}));
 assert.equal((html.match(/aria-busy="true"/g)||[]).length,2);
 assert.equal((html.match(/<strong>Loading/g)||[]).length,2);
 assert.doesNotMatch(html,/Prepare a runtime first/);
});
const render=(state)=>renderToStaticMarkup(React.createElement(CodexRuntimePanel,{state,busy:false,checking:false,onAction:()=>{},onDetect:()=>{}}));
const base={state:'login',installation:{installed:true,version:'1.0',updateState:''},loginPending:false};
test('Codex authentication action failures remain local without a feedback card',()=>{
 const html=renderToStaticMarkup(React.createElement(CodexRuntimePanel,{state:base,busy:false,checking:false,error:true,onAction:()=>{},onDetect:()=>{}}));
 assert.match(html,/role="status"/);
 assert.doesNotMatch(html,/Copy feedback|SurfaceBoundary|recovery-actions/);
 assert.match(html,/Log in with ChatGPT/);
});
test('Codex offers installation only when absent, login only after handshake, and updates independently',()=>{
 const absent=render({...base,installation:{installed:false}});
 assert.match(absent,/Install Codex/);assert.doesNotMatch(absent,/Log in with ChatGPT|Check for updates/);
 const login=render(base);assert.match(login,/Log in with ChatGPT/);assert.match(login,/Check for updates/);
 const ready=render({...base,state:'ready'});assert.match(ready,/Ready to use/);assert.doesNotMatch(ready,/Log in with ChatGPT/);
 const incompatible=render({...base,state:'incompatible'});assert.match(incompatible,/Unable to connect/);assert.doesNotMatch(incompatible,/Log in with ChatGPT/);
 for(const html of [absent,login,ready,incompatible])assert.doesNotMatch(html,/Auto-Review|Program and paths/);
});

test('built-in separates installation, connection, and optional Team without exposing extra agents',()=>{
 const panel=state=>renderToStaticMarkup(React.createElement(BuiltinRuntimePanel,{state,busy:false,checking:false,onAction:()=>{},onConnect:()=>{},onTeam:()=>{},onDetect:()=>{}}));
 assert.doesNotMatch(panel({...base,installation:{installed:false}}),/Team|Model connections/);
 const installed=panel({...base,state:'models'});assert.match(installed,/Connect a model/);assert.match(installed,/Check for updates/);assert.ok(installed.indexOf('Agent Team')<installed.indexOf('Current version'));
 const role=id=>({id,selection:{model:id==='guardian-screen'?'classifier':id==='guardian'?'review-model':''},modelIds:[],inherited:true});
 const html=renderToStaticMarkup(React.createElement(TeamSettings,{inline:true,team:{available:true,roles:['self','breeze','orbit','zenith','tool-search','guardian','guardian-screen','reviewer','custom'].map(role)},models:[{model:'classifier',name:'Jev classifier'},{model:'review-model',name:'Luna reviewer'}],client:{},onRefresh:async()=>{}}));
 assert.equal((html.match(/class="team-agent-card"/g)||[]).length,5);
 assert.match(html,/Jev classifier/);assert.match(html,/Luna reviewer/);assert.match(html,/Classifier \(optional\)/);assert.doesNotMatch(html,/>guardian-screen<|>self<|>reviewer<|>custom</);
});

test('existing Caelis models can continue without a new provider connection',async()=>{
 globalThis.HTMLElement ??= class HTMLElement {};
 const {ExistingSetupModels}=await server.ssrLoadModule('/src/settings/runtime/ExistingSetupModels.tsx');
 const renderExisting=(selectedModel,disabled=false)=>renderToStaticMarkup(React.createElement(ExistingSetupModels,{models:[{value:'custom/existing',label:'Existing custom model',noAuth:false},{value:'unavailable',label:'No credentials',noAuth:true}],selectedModel,disabled,onSelect:()=>{},onContinue:()=>{}}));
 const ready=renderExisting('custom/existing');
 assert.match(ready,/Existing custom model/);
 assert.match(ready,/<button class="setup-button primary"><span class="setup-button-label">Next<\/span><\/button>/);
 assert.doesNotMatch(ready,/No credentials|API Key|Add connection/);
 for(const html of [renderExisting('unavailable'),renderExisting(''),renderExisting('custom/existing',true)])assert.match(html,/<button class="setup-button primary" disabled=""><span class="setup-button-label">Next<\/span><\/button>/);
});


test('bundled Caelis needs no installation or self-update, while Host activation remains available',()=>{
 const panel=state=>renderToStaticMarkup(React.createElement(BuiltinRuntimePanel,{state,busy:false,checking:false,onAction:()=>{},onConnect:()=>{},onTeam:()=>{},onDetect:()=>{}}));
 const bundled={...base,state:'models',installation:{installed:true,bundled:true,version:'0.72.2',updateState:'available',latestVersion:'99'}};
 const html=panel(bundled);
 assert.match(html,/updated with the app/);assert.doesNotMatch(html,/Check for updates|Install|New version available/);
 assert.match(panel({...bundled,serviceUpdateAvailable:true}),/Enable installed version/);
});

const {existingModelChoice,activateExistingModel}=await server.ssrLoadModule('/src/settings/runtime/onboarding-models.ts');
const modelSetup={state:'ready',selectedModel:'saved/one',models:[{value:'saved/one',label:'One',noAuth:false},{value:'saved/two',label:'Two',noAuth:false},{value:'locked',label:'Needs credentials',noAuth:true}]};
test('onboarding reuses authenticated models even before a default model is selected',()=>{
 assert.deepEqual(existingModelChoice(null),{models:[],selected:''});
 assert.equal(existingModelChoice({...modelSetup,state:'models',selectedModel:''}).selected,'saved/one');
 assert.equal(existingModelChoice(modelSetup,'saved/two').selected,'saved/two');
 assert.equal(existingModelChoice(modelSetup,'locked').selected,'saved/one');
 assert.equal(existingModelChoice({...modelSetup,models:[modelSetup.models[2]]}).models.length,0);
 assert.equal(existingModelChoice({...modelSetup,state:'service'}).models.length,0);
 assert.equal(existingModelChoice({...modelSetup,loginPending:true}).models.length,0);
 assert.equal(existingModelChoice({...modelSetup,state:'login'}).models.length,0);
});

test('Codex confirms its selected model before activation without repeating installation or login',async()=>{
 const calls=[];
 const profile={runtime:'codex',cliPath:'existing-codex.exe'};
 await activateExistingModel(async(method,arg)=>{
  calls.push([method,arg]);
  return {...modelSetup,selectedModel:'saved/two'};
 },profile,modelSetup,'saved/two');
 assert.deepEqual(calls.map(([method])=>method),['ApplySetup','ActivateRuntime']);
 assert.deepEqual(calls[0][1],{settings:profile,action:'use-model',model:'saved/two',provider:'',baseUrl:'',apiKey:''});
 assert.deepEqual(calls[1][1],profile);
 let activated=false;
 await assert.rejects(activateExistingModel(async()=>{activated=true},profile,{...modelSetup,state:'login'},'saved/one'));
 assert.equal(activated,false);
});

test('selector placeholders stay in the trigger, while real empty and unavailable choices remain in the menu',async()=>{
 globalThis.HTMLElement ??= class HTMLElement {};
 const {SettingsSelect}=await server.ssrLoadModule('/src/SettingsSelect.tsx');
 const option=(value,label,props={})=>React.createElement('option',{value,...props},label);
 const render=children=>renderToStaticMarkup(React.createElement(SettingsSelect,{value:'',placeholder:'Choose model',onChange:()=>{}},children));
 const placeholder=render([option('','Choose model',{disabled:true}),option('one','One')]);
 const menu=placeholder.slice(placeholder.indexOf('role="menu"'));
 assert.match(placeholder,/Choose model/);assert.doesNotMatch(menu,/Choose model/);
 assert.equal((menu.match(/role="menuitemradio"/g)||[]).length,1);
 const defaults=render([option('','Default'),option('unavailable','Unavailable',{disabled:true})]);
 assert.match(defaults,/role="menuitemradio" aria-label="Default" aria-checked="true"/);
 assert.match(defaults,/role="menuitemradio" aria-label="Unavailable" aria-checked="false" disabled/);
 const explicit=render([option('one','One')]);
 assert.match(explicit,/Choose model/);assert.equal((explicit.match(/Choose model/g)||[]).length,1);
});
test('Next reuses current authentication and awaits actual activation without another configuration write',async()=>{
 const calls=[];let release;
 const ready=new Promise(resolve=>{release=resolve});
 const result=activateExistingModel(async method=>{calls.push(method);await ready;},{runtime:'caelis'},modelSetup,'saved/one');
 let advanced=false;void result.then(()=>{advanced=true});
 await Promise.resolve();assert.equal(advanced,false);
 assert.deepEqual(calls,['ActivateRuntime']);release();await result;assert.equal(advanced,true);
});
test('a different model is confirmed before activation; failed or uncertain writes are never replayed',async()=>{
 const calls=[];
 await activateExistingModel(async(method,request)=>{calls.push([method,request]);return {...modelSetup,selectedModel:'saved/two'};},{runtime:'caelis'},modelSetup,'saved/two');
 assert.deepEqual(calls.map(c=>c[0]),['ApplySetup','ActivateRuntime']);assert.equal(calls[0][1].model,'saved/two');
 for(const response of [new Error('lost receipt'),modelSetup]){
  const methods=[];
  await assert.rejects(activateExistingModel(async method=>{methods.push(method);if(response instanceof Error)throw response;return response;},{runtime:'caelis'},modelSetup,'saved/two'));
  assert.deepEqual(methods,['ApplySetup']);
 }
 let called=false;
 await assert.rejects(activateExistingModel(async()=>{called=true;},{runtime:'caelis'},modelSetup,'locked'));
 assert.equal(called,false);
});
test('onboarding progress retains the action label and disables repeated submission',async()=>{
 const {SetupButton,SetupLoading}=await server.ssrLoadModule('/src/SetupLoading.tsx');
 const button=renderToStaticMarkup(React.createElement(SetupButton,{busy:true},'Next'));
 assert.match(button,/disabled="" aria-busy="true"/);assert.match(button,/>Next</);assert.match(button,/setup-spinner/);
 const page=renderToStaticMarkup(React.createElement(SetupLoading));
 assert.match(page,/role="status"/);assert.match(page,/aria-busy="true"/);
 assert.doesNotMatch(page,/Ready|Continue|Connecting/);
});

const {CodexLogin}=await server.ssrLoadModule('/src/settings/runtime/CodexLogin.tsx');
test('shared device ceremony has one account primary and one secondary key entry',()=>{
 const html=renderToStaticMarkup(React.createElement(CodexLogin,{state:base,onAction:()=>{}}));
 assert.equal((html.match(/>Log in with ChatGPT</g)||[]).length,1);
 assert.equal((html.match(/>Use OpenAI API Key</g)||[]).length,1);
 assert.doesNotMatch(html,/device-auth|Advanced options|Program and paths|Recheck|Reopen window/);
});
test('owned pending challenge exposes only official user instructions, never another start or API key',()=>{
 const state={...base,loginPending:true,login:{id:'attempt',state:'pending',userCode:'TEST-1234',verificationUrl:'https://auth.openai.com/codex/device',expiresAt:'2026-10-20T12:00:00Z'}};
 const html=renderToStaticMarkup(React.createElement(CodexLogin,{state,onAction:()=>{}}));
 assert.match(html,/TEST-1234/);assert.match(html,/Copy verification code/);assert.match(html,/Open verification page/);assert.match(html,/Waiting for your authorization/);
 assert.doesNotMatch(html,/>Log in with ChatGPT<|API Key|Use OpenAI|attempt/);
 const other=renderToStaticMarkup(React.createElement(CodexLogin,{state:{...state,login:{state:'other-session'}},onAction:()=>{}}));
 assert.doesNotMatch(other,/TEST-1234|verification page|Cancel/);
 assert.match(other,/browser that started/);
});
