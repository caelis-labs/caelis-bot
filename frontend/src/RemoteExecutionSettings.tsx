import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {backend} from './desktop';
import type {RemoteExecutionView,RemoteRuntimeState} from './backend/contract';
import {ModelPicker,ModelSummary} from './settings/runtime/ModelPicker';
import {createRemoteExecutionClient} from './settings/runtime/remoteExecutionClient';
import {useI18n} from './i18n';

export function RemoteExecutionSettings({state,active,refreshKey,onChanged,call=backend}:{call?:typeof backend;state:RemoteRuntimeState;active:boolean;refreshKey:number;onChanged:()=>void}){
 const {t}=useI18n();
 const [view,setView]=useState<RemoteExecutionView|null>(null),[editing,setEditing]=useState<'conversation'|'work'|null>(null),[error,setError]=useState('');
 const epoch=useRef(0),editingRevision=useRef(''),editingSelection=useRef<RemoteExecutionView['conversation']|null>(null),editingClient=useRef<ReturnType<typeof createRemoteExecutionClient>|null>(null);
 const client=useMemo(()=>createRemoteExecutionClient(call,state.binding,t('settings.productManagementUnknown'),onChanged),[call,state.binding,t,onChanged]);
 const refresh=useCallback(async()=>{
  const generation=++epoch.current;
  const next=await client.read();
  if(generation===epoch.current){setView(next);setError('');return next;}
 },[client]);
 useEffect(()=>{if(!active)return;void refresh().catch(()=>setError(t('settings.productManagementUnavailable')));return()=>{epoch.current++;};},[active,refreshKey,refresh,t]);
 const models=(view?.models??[]).map(model=>({...model,efforts:model.efforts??[],serviceTiers:[]}));
 const blocked=!state.available||state.pending.length>0;
 const edit=(scope:'conversation'|'work')=>{editingRevision.current=view?.revision??'';editingSelection.current=(scope==='conversation'?view?.conversation:view?.work)??null;editingClient.current=client;setEditing(scope);};
 const selection=editingSelection.current;
 return <section className="runtime-model-section">
  {view&&<>
   <div className="runtime-setting-row"><strong>{t('runtime.botConversation')}</strong><ModelSummary value={{...view.conversation,serviceTier:''}} models={models} label={t('runtime.botConversationModel')} disabled={blocked} onClick={()=>edit('conversation')}/></div>
   {view.work&&<div className="runtime-setting-row"><strong>{t('runtime.dedicatedWorkModel')}</strong><ModelSummary value={{...view.work,serviceTier:''}} models={models} label={t('runtime.dedicatedWorkModel')} disabled={blocked} onClick={()=>edit('work')}/></div>}
  </>}
  {editing&&selection&&view&&<ModelPicker key={`${state.binding}:${editing}`} title={t(editing==='conversation'?'runtime.botConversationModel':'runtime.dedicatedWorkModel')} description={editing==='work'?t('runtime.dedicatedWorkModelDescription'):undefined} value={{...selection,serviceTier:''}} models={models} allowServiceTier={false} disabled={blocked} inherited={editing==='work'||view.conversationDefault} onSave={async selection=>{await editingClient.current!.save(editing,selection,editingRevision.current);await refresh();}} onReload={async()=>{const next=await refresh();if(next){editingRevision.current=next.revision;editingClient.current=client;}}} onClose={()=>setEditing(null)}/>}
  {error&&<p role="alert" className="inline-error">{error}</p>}
 </section>;
}
