import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {backend} from './desktop';
import type {RemoteExecutionView,RemoteRuntimeState} from './backend/contract';
import {ModelPicker,ModelSummary} from './settings/runtime/ModelPicker';
import {createRemoteExecutionClient} from './settings/runtime/remoteExecutionClient';
import {useI18n} from './i18n';

export function RemoteExecutionSettings({state,active,refreshKey,onChanged}:{state:RemoteRuntimeState;active:boolean;refreshKey:number;onChanged:()=>void}){
 const {t}=useI18n();
 const [view,setView]=useState<RemoteExecutionView|null>(null),[editing,setEditing]=useState<'conversation'|'work'|null>(null),[error,setError]=useState('');
 const epoch=useRef(0);
 const client=useMemo(()=>createRemoteExecutionClient(backend,state.binding,t('settings.productManagementUnknown'),onChanged),[state.binding,t,onChanged]);
 const refresh=useCallback(async()=>{
  const generation=++epoch.current;
  const next=await client.read();
  if(generation===epoch.current){setView(next);setError('');}
 },[client]);
 useEffect(()=>{if(!active)return;void refresh().catch(()=>setError(t('settings.productManagementUnavailable')));return()=>{epoch.current++;};},[active,refreshKey,refresh,t]);
 const models=(view?.models??[]).map(model=>({...model,efforts:model.efforts??[],serviceTiers:[]}));
 const blocked=!state.available||state.pending.length>0;
 const selection=view&&(editing==='conversation'?view.conversation:editing==='work'?view.work:null);
 return <section className="runtime-model-section">
  {view&&<>
   <div className="runtime-setting-row"><strong>{t('runtime.botConversation')}</strong><ModelSummary value={{...view.conversation,serviceTier:''}} models={models} label={t('runtime.botConversationModel')} disabled={blocked} onClick={()=>setEditing('conversation')}/></div>
   {view.work&&<div className="runtime-setting-row"><strong>{t('runtime.dedicatedWorkModel')}</strong><ModelSummary value={{...view.work,serviceTier:''}} models={models} label={t('runtime.dedicatedWorkModel')} disabled={blocked} onClick={()=>setEditing('work')}/></div>}
  </>}
  {editing&&selection&&view&&<ModelPicker key={`${state.binding}:${editing}`} title={t(editing==='conversation'?'runtime.botConversationModel':'runtime.dedicatedWorkModel')} description={editing==='work'?t('runtime.dedicatedWorkModelDescription'):undefined} value={{...selection,serviceTier:''}} models={models} allowServiceTier={false} disabled={blocked} inherited={editing==='work'||view.conversationDefault} onSave={async selection=>{await client.save(editing,selection,view.revision);await refresh();}} onReload={refresh} onClose={()=>setEditing(null)}/>}
  {error&&<p role="alert" className="inline-error">{error}</p>}
 </section>;
}
