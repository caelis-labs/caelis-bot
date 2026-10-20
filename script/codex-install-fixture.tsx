// Synthetic installation lifecycle; renders the production shared App/Web UI.
import React from 'react';
import {createRoot} from 'react-dom/client';
import {I18nProvider} from '../frontend/src/i18n';
import {RuntimePreparation} from '../frontend/src/RuntimeSettings';
import {installTransport} from '../frontend/src/transport';
import '../frontend/src/application.css';
const bridge={read:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),save:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),subscribe:()=>()=>{}};
const root=createRoot(document.getElementById('root')!);
const profile={runtime:'codex',cliPath:'',caelisStore:''};
let progress:any={phase:'',downloaded:0,total:0,bytesPerSecond:0},installed=false,starts=0,serial=0,reads=0;
const state=()=>({settings:profile,installation:{installed,version:installed?'fixture':'',path:''},state:progress.state==='running'?'installing':installed?'login':'missing',loginPending:false,models:[],selectedModel:''});
const call:any=async(method:string,...args:any[])=>{
 if(method==='SetupOverview')return {active:'codex',hasRuntimeChoice:false,pending:'',onboarding:true};
 if(method==='RuntimeSettings'||method==='SetupProfile')return profile;
 if(method==='ComposerSnapshot')return {connection:'offline'};
 if(method==='InspectSetup')return state();
 if(method==='SetupInstallProgress'){reads++;return {...progress};}
 if(method==='CancelSetupOperation'){progress={...progress,state:'cancelled'};return true;}
 if(method==='ApplySetup'){
  if(args[0].action==='install'&&progress.state!=='running'){starts++;progress={...progress,runtime:'codex',id:'install-'+starts,action:'install',state:'running',phase:'checking'};}
  return state();
 }
 throw Error('Unexpected fixture method '+method);
};
if(new URLSearchParams(location.search).get('surface')==='web')installTransport((_service,method,args)=>call(method,...args));
const mount=()=>root.render(<I18nProvider bridge={bridge}><div className="settings-window" data-surface="settings"><div className="setup-flow"><RuntimePreparation key={++serial} onboarding initialRuntime="codex" call={call}/></div></div></I18nProvider>);
// Existing native preview menu drives synthetic phase transitions only.
(window as any).fixtureNavigate=()=>{if((window as any).fixtureRuntimeFailure)progress={...progress,state:'failed'};else if((window as any).fixtureRefreshDelay)progress={...progress,phase:'downloading'};};
(window as any).installFixture={mount,snapshot:()=>({starts,reads,progress,installed}),phase:(phase:string)=>progress={...progress,phase},finish:(state:string)=>{progress={...progress,state};installed=state==='complete';}};
mount();
