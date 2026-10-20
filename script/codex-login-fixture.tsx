import React from 'react';
import {createRoot} from 'react-dom/client';
import {I18nProvider} from '../frontend/src/i18n';
import {RuntimePreparation} from '../frontend/src/RuntimeSettings';
import '../frontend/src/application.css';
const bridge={read:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),save:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),subscribe:()=>()=>{}};
const root=createRoot(document.getElementById('root')!);
const profile={runtime:'codex',cliPath:'',caelisStore:''};
const base={settings:profile,installation:{installed:true,version:'fixture',path:'PRIVATE_PATH'},state:'login',loginPending:false,models:[],selectedModel:''};
let state:any={...base},starts=0,cancels=0,reads=0,serial=0,copied='',opened='',lost=false;
const delay=(ms:number)=>new Promise(resolve=>setTimeout(resolve,ms));
const call:any=async(method:string,...args:any[])=>{
 if(method==='SetupOverview')return {active:'codex',hasRuntimeChoice:false,pending:'',onboarding:true};
 if(method==='RuntimeSettings'||method==='SetupProfile')return profile;
 if(method==='ComposerSnapshot')return {connection:'offline'};
 if(method==='OpenMessageLink'){opened=args[0];return;}
 if(method==='InspectSetup'){reads++;return structuredClone(state);}
 if(method==='ApplySetup'){
  const input=args[0];
  if(input.action==='login'){
   if(!state.loginPending){starts++;state={...base,loginPending:true,login:{id:`attempt-${starts}`,state:'pending',userCode:'TEST-1234',verificationUrl:'https://auth.openai.com/codex/device',expiresAt:new Date(Date.now()+900000).toISOString()}};}
   await delay(90);if(lost){lost=false;throw Error('PRIVATE_SENTINEL lost reply');}
  }
  if(input.action==='cancel-login'){if(input.loginId!==state.login.id)throw Error('wrong attempt');cancels++;state={...base,login:{id:state.login.id,state:'cancelled'}};}
  return structuredClone(state);
 }
 throw Error('Unexpected fixture method '+method);
};
const host:any=async(method:string,value:string)=>{if(method==='CopyText'){copied=value;return;}throw Error('Unexpected host call');};
const mount=()=>root.render(<I18nProvider bridge={bridge}><div className="settings-window" data-surface="settings"><div className="setup-flow"><RuntimePreparation key={++serial} onboarding initialRuntime="codex" call={call} host={host}/></div></div></I18nProvider>);
(window as any).codexFixture={
 mount, snapshot:()=>({starts,cancels,reads,copied,opened}),lost:()=>lost=true,
 terminal:(value:string)=>{state=value==='complete'?{...base,state:'ready',models:[{value:'fixture/model',label:'Fixture model',noAuth:false,current:true,parameters:{model:'fixture/model',name:'Fixture model',efforts:[],serviceTiers:[]}}],selectedModel:'fixture/model',login:{state:'complete'}}:{...base,login:{id:state.login?.id,state:value}}},
};
mount();
