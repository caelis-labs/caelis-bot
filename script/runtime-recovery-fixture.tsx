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
   check(!document.querySelector('.surface-recovery'),'transient showed recovery');
  }else{
   await wait(()=>state.reads===3&&!!document.querySelector('.surface-recovery'));
   check(state.reads===3&&state.starts===0,'catalog failure dispatched auth or retried forever');
   check(!document.querySelector('.setup-loading'),'catalog exhausted but spinner remained');
   const back=[...document.querySelectorAll('button')].find(b=>b.textContent?.includes('Back'));
   check(!!back&&!back.disabled,'missing Back after failed catalog');
   back!.click();check(state.backs===1,'Back failed');
   state.recover();
   const retry=document.querySelector<HTMLButtonElement>('.surface-recovery-primary');
   check(!!retry&&!retry.disabled,'missing retry');retry!.click();
   await wait(()=>state.starts===1);check(state.reads===4,'retry did not load catalog once');
  }
  check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'private technical error leaked');
  evidence.push({mode,reads:state.reads,starts:state.starts,backs:state.backs});
 }
 phase='setup';let setupReads=0;
 (window as any).acceptanceNative=async(name:string)=>{if(name.endsWith('.FeatureGuidePending'))setupReads++;phase='setup reads='+setupReads;throw Error('PRIVATE_SENTINEL first-run read');};
 root.render(<I18nProvider bridge={bridge}><BotSetup onDone={()=>{throw Error('failed setup completed')}}/></I18nProvider>);
 await wait(()=>setupReads===3&&!!document.querySelector('.surface-recovery'));
 check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'setup exposed technical error');
 await delay(1600);check(setupReads===3,'setup read retried after exhaustion');
 phase='preview';let previewReads=0,previewReady=false;
 (window as any).acceptanceNative=async(name:string)=>{
  if(name.endsWith('.CurrentArtifactPreview')){previewReads++;if(!previewReady)throw Error('PRIVATE_SENTINEL artifact read');return {id:'fixture',name:'fixture.txt'}}
  if(name.endsWith('.PreviewArtifact'))return {text:'Recovered preview',truncated:false};
 };
 root.render(<I18nProvider bridge={bridge}><ArtifactPreviewWindow/></I18nProvider>);
 await wait(()=>previewReads===3&&!!document.querySelector('.surface-recovery'));
 check(!document.body.innerText.includes('PRIVATE_SENTINEL'),'preview exposed technical error');
 previewReady=true;document.querySelector<HTMLButtonElement>('.surface-recovery-primary')!.click();
 await wait(()=>document.body.innerText.includes('Recovered preview'));
 check(previewReads===4,'preview retry did not reconcile once');
 phase='form';let retries=0,submits=0;
 root.render(<I18nProvider bridge={bridge}><form data-recovery-form onSubmit={event=>{event.preventDefault();submits++}}><Recovery onRetry={()=>retries++}/></form></I18nProvider>);
 await wait(()=>!!document.querySelector('[data-recovery-form]'));
 document.querySelector<HTMLButtonElement>('[data-recovery-form] .surface-recovery-primary')!.click();
 check(retries===1&&submits===0,'recovery accidentally submitted enclosing form');
 check(document.querySelector('.surface-recovery')!.getBoundingClientRect().height<200,'local recovery took over the whole window');
 check(!document.body.innerText.includes('needs to reopen'),'local retry described window restart');
 return (window as any).recoveryResult={ok:true,evidence,setupReads,previewReads,recoveryForm:{retries,submits}};
};
mount('reject');
const runButton=document.createElement('button');runButton.textContent='Run recovery regression';
const result=document.createElement('pre');result.setAttribute('data-testid','result');
runButton.onclick=()=>{runButton.disabled=true;void (window as any).runRecoveryRegression().then((value:unknown)=>{result.textContent=JSON.stringify(value,null,2)}).catch((error:Error)=>{result.textContent=JSON.stringify({ok:false,error:error.message})}).finally(()=>runButton.disabled=false)};
document.body.append(runButton,result);
