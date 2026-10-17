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

test('renderer recovery retries once, then offers feedback and reopen without exception prose',async(t)=>{
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
 assert.match(recovery,/Copy feedback/);
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
 assert.match(ready,/<button class="primary">Continue<\/button>/);
 assert.doesNotMatch(ready,/No credentials|API Key/);
 for(const html of [renderExisting('unavailable'),renderExisting(''),renderExisting('custom/existing',true)])assert.match(html,/<button class="primary" disabled="">Continue<\/button>/);
});


test('bundled Caelis needs no installation or self-update, while Host activation remains available',()=>{
 const panel=state=>renderToStaticMarkup(React.createElement(BuiltinRuntimePanel,{state,busy:false,checking:false,onAction:()=>{},onConnect:()=>{},onTeam:()=>{},onDetect:()=>{}}));
 const bundled={...base,state:'models',installation:{installed:true,bundled:true,version:'0.72.2',updateState:'available',latestVersion:'99'}};
 const html=panel(bundled);
 assert.match(html,/updated with the app/);assert.doesNotMatch(html,/Check for updates|Install|New version available/);
 assert.match(panel({...bundled,serviceUpdateAvailable:true}),/Enable installed version/);
});
