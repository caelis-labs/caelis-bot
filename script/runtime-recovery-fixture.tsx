import React from 'react';
import {createRoot} from 'react-dom/client';
import {I18nProvider} from '../frontend/src/i18n';
import {BotSetup} from '../frontend/src/BotSetup';
import {ArtifactPreviewWindow} from '../frontend/src/ArtifactPreviewWindow';
import {Recovery} from '../frontend/src/SurfaceBoundary';
import {ConnectionWizard} from '../frontend/src/settings/runtime/ConnectionWizard';
import '../frontend/src/application.css';
const root=createRoot(document.getElementById('root')!);
const bridge={read:async()=>({preference:'en' as const,locale:'en' as const,revision:1}),save:async()=>({preference:'en' as const,locale:'en' as const,revision:1}),subscribe:()=>()=>{}};
const delay=(ms:number)=>new Promise(r=>setTimeout(r,ms));
let phase='';
const wait=async(test:()=>boolean)=>{const end=Date.now()+6000;while(!test()){if(Date.now()>end)throw Error('fixture timeout: '+phase);await delay(20)}};
let serial=0;
const mount=(mode:string)=>{
 let reads=0,starts=0,backs=0,canRecover=false;
 const choice={id:'codex',name:'ChatGPT',description:'',custom:false};
 const client={catalog:async()=>{
  reads++;
  if(canRecover||mode==='transient'&&reads>1)return {choices:[choice],unavailable:''};
  if(mode==='empty')return {choices:[],unavailable:''};
  if(mode==='missing')return {choices:[{...choice,id:'other'}],unavailable:''};
  if(mode==='unavailable')return {choices:[],unavailable:'PRIVATE_SENTINEL socket rejected'};
  throw Error('PRIVATE_SENTINEL /private/user/token');
 },startConnection:async()=>{starts++;return {id:'original-flow',stage:'models',models:[{id:'model-a',name:'Model A'}],title:'Select model',message:''}}};
 root.render(<I18nProvider bridge={bridge}><ConnectionWizard key={++serial} inline onboarding initialKind="account" initialChoice="codex" client={client as any} onClose={()=>backs++} onConnected={async()=>{}}/></I18nProvider>);
 return {get reads(){return reads},get starts(){return starts},get backs(){return backs},recover:()=>canRecover=true};
};
const check=(ok:boolean,message:string)=>{if(!ok)throw Error(message)};
(window as any).runRecoveryRegression=async()=>{
 const evidence=[];
 for(const mode of ['reject','empty','missing','unavailable','transient']){
  phase=mode;const state=mount(mode);
  await wait(()=>state.reads>0);
  if(mode==='transient'){
   await wait(()=>state.starts===1);check(state.reads===2,'transient read budget');
   check(!document.querySelector('.operation-error'),'transient showed recovery');
  }else{
   await wait(()=>state.reads===3&&!!document.querySelector('.operation-error'));
   check(state.reads===3&&state.starts===0,'catalog failure dispatched auth or retried forever');
   check(!document.querySelector('.setup-loading'),'catalog exhausted but spinner remained');
   const back=[...document.querySelectorAll('button')].find(b=>b.textContent?.includes('Back'));
   check(!!back&&!back.disabled,'missing Back after failed catalog');
   back!.click();check(state.backs===1,'Back failed');
   state.recover();
   const retry=document.querySelector<HTMLButtonElement>('.operation-error button');
   check(!!retry&&!retry.disabled,'missing retry');retry!.click();
   await wait(()=>state.starts===1);check(state.reads===4,'retry did not load catalog once');
  }
  check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'private technical error leaked');
  evidence.push({mode,reads:state.reads,starts:state.starts,backs:state.backs});
 }
 const gatewayEvidence=[];
 for(const failure of ['reject','empty']){
  phase='gateway '+failure;
  let reads=0,available=false,started=0;
  const client={
   catalog:async()=>({choices:[{id:'magpie',name:'Magpie',custom:false}],unavailable:''}),
   apiKeyOptions:async()=>{
    reads++;
    if(!available&&failure==='reject')throw Error('PRIVATE_SENTINEL http://user:secret@private/?token=secret');
    return {endpoints:[],models:available?[{value:'demo/model',name:'Recovered model'}]:[]};
   },
   startConnection:async()=>{started++;throw Error('unexpected mutation');},
  };
  root.render(<I18nProvider bridge={bridge}><ConnectionWizard key={++serial} inline onboarding initialKind="api-key" initialChoice="magpie" client={client as any} onClose={()=>{}} onConnected={async()=>{}}/></I18nProvider>);
  await wait(()=>!!document.querySelector('.gateway-read-notice'));
  check(reads===(failure==='reject'?3:1),'gateway reads did not use bounded retry');
  check(!document.querySelector('.operation-error'),'gateway failure showed window recovery');
  const address=document.querySelector<HTMLInputElement>('input[type=url]')!;
  check(address.value==='http://127.0.0.1:3425/v1'&&!address.disabled,'gateway form was lost');
  check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'gateway leaked error');
  available=true;document.querySelector<HTMLButtonElement>('.gateway-read-notice button')!.click();
  await wait(()=>document.body.innerText.includes('Recovered model'));
  check(document.querySelector('input[type=url]')===address,'retry remounted form');
  check(!document.querySelector('.gateway-read-notice')&&started===0,'read retry mutated connection');
  gatewayEvidence.push({failure,reads,started,formPreserved:true});
 }
 phase='setup';let setupReads=0;
 (window as any).acceptanceNative=async(name:string)=>{if(name.endsWith('.FeatureGuidePending'))setupReads++;phase='setup reads='+setupReads;throw Error('PRIVATE_SENTINEL first-run read');};
 root.render(<I18nProvider bridge={bridge}><BotSetup onDone={()=>{throw Error('failed setup completed')}}/></I18nProvider>);
 await wait(()=>setupReads===3&&!!document.querySelector('.operation-error'));
 check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'setup exposed technical error');
 await delay(1600);check(setupReads===3,'setup read retried after exhaustion');
 phase='preview';let previewReads=0,previewReady=false;
 (window as any).acceptanceNative=async(name:string)=>{
  if(name.endsWith('.CurrentArtifactPreview')){previewReads++;if(!previewReady)throw Error('PRIVATE_SENTINEL artifact read');return {id:'fixture',name:'fixture.txt'}}
  if(name.endsWith('.PreviewArtifact'))return {text:'Recovered preview',truncated:false};
 };
 root.render(<I18nProvider bridge={bridge}><ArtifactPreviewWindow/></I18nProvider>);
 await wait(()=>previewReads===3&&!!document.querySelector('.operation-error'));
 check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'preview exposed technical error');
 previewReady=true;document.querySelector<HTMLButtonElement>('.operation-error button')!.click();
 await wait(()=>document.body.innerText.includes('Recovered preview'));
 check(previewReads===4,'preview retry did not reconcile once');
 phase='form';let retries=0,submits=0;
 root.render(<I18nProvider bridge={bridge}><form data-recovery-form onSubmit={event=>{event.preventDefault();submits++}}><Recovery message="This action needs another check." onRetry={()=>retries++}/></form></I18nProvider>);
 await wait(()=>!!document.querySelector('[data-recovery-form]'));
 document.querySelector<HTMLButtonElement>('[data-recovery-form] .operation-error button')!.click();
 check(retries===1&&submits===0,'recovery accidentally submitted enclosing form');
 check(document.querySelector('.operation-error')!.getBoundingClientRect().height<200,'local recovery took over the whole window');
 check(!document.body.innerText.includes('needs to reopen'),'local retry described window restart');
 return (window as any).recoveryResult={ok:true,evidence,gatewayEvidence,setupReads,previewReads,recoveryForm:{retries,submits}};
};
mount('reject');
const runButton=document.createElement('button');runButton.textContent='Run recovery regression';
const result=document.createElement('pre');result.setAttribute('data-testid','result');
runButton.onclick=()=>{runButton.disabled=true;void (window as any).runRecoveryRegression().then((value:unknown)=>{result.textContent=JSON.stringify(value,null,2)}).catch((error:Error)=>{result.textContent=JSON.stringify({ok:false,error:error.message})}).finally(()=>runButton.disabled=false)};
document.body.append(runButton,result);

(window as any).runIntroductionRegression=async()=>{
 let ready=true,initializations=0,opened=0,done=0;
 (window as any).acceptanceNative=async(name:string)=>{
  switch(name.split('.').at(-1)){
   case 'FeatureGuidePending':return false;
   case 'SetupOverview':return {hasRuntimeChoice:true,active:'caelis',onboarding:true};
   case 'Snapshot':return {connection:ready?'ready':'offline',phase:'idle',canSend:ready};
   case 'BotInitialization':return {required:true,status:'required'};
   case 'InitializeBot':initializations++;return {required:false,status:'pending'};
   case 'OpenHistory':opened++;return;
   default:throw Error('unexpected method '+name);
  }
 };
 document.body.dataset.surface='settings';
 const previousName=document.querySelector('#bot-name');
 root.render(<I18nProvider bridge={bridge}><main className="settings-window setup-window"><div className="setup-page"><BotSetup key={++serial} onDone={()=>done++}/></div></main></I18nProvider>);
 await wait(()=>!!document.querySelector('#bot-name')&&document.querySelector('#bot-name')!==previousName);
 const name=document.querySelector<HTMLInputElement>('#bot-name')!;
 const setValue=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!;
 setValue.call(name,'Retained introduction');name.dispatchEvent(new Event('input',{bubbles:true}));
 await wait(()=>!(document.querySelector('.setup-end .primary') as HTMLButtonElement)?.disabled);
 ready=false;
 document.querySelector<HTMLButtonElement>('.setup-end .primary')!.click();
 await wait(()=>!!document.querySelector('.bot-introduction .operation-error'));
 check(initializations===0&&opened===0,'failed preflight dispatched introduction');
 check(name.value==='Retained introduction','failed preflight erased input');
 check(document.querySelector('.setup-end .primary')?.textContent==='Start chatting','primary label silently changed');
 check(!document.body.innerText.includes('Copy feedback'),'feedback mounted in introduction');
 (window as any).resumeIntroductionRegression=async()=>{
  ready=true;
  const recheck=[...document.querySelectorAll('button')].find(b=>b.textContent==='Check connection again')!;
  recheck.click();await wait(()=>!(document.querySelector('.setup-end .primary') as HTMLButtonElement).disabled);
  check(initializations===0,'read-only recheck dispatched work');
  document.querySelector<HTMLButtonElement>('.setup-end .primary')!.click();
  await wait(()=>done>0);
  check(initializations===1&&opened===1,'introduction duplicated or did not navigate');
  return {fixtureOnly:true,initializations,opened,inputRetained:true,readOnlyRecheck:true};
 };
 return {fixtureOnly:true,failedPreflight:true,initializations,opened,inputRetained:name.value==='Retained introduction'};
};
