import {useEffect, useRef, useState} from 'react';
import {backend} from '../../desktop';
import type {LocalWorkerSettings as WorkerSettings} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {ModelPicker, ModelSummary} from './ModelPicker';
import {executionMethodLabel} from './state';
import {serialLatest} from './worker-refresh';
import {readSelectedWorker,workerDisplayError} from './worker-selection';
import {workerCardState} from './readiness';

type Request = {kind:'refresh'|'select'; runtime:string};

export function LocalWorkerSettings({active=true,refreshKey=0,onConnections,call=backend}:{active?:boolean;refreshKey?:number;onConnections?:(runtime?:string)=>void;onTeam?:()=>void;call?:typeof backend}) {
 const {t,locale}=useI18n();
 const [view,setView]=useState<WorkerSettings|null>(null),[selected,setSelected]=useState(''),[available,setAvailable]=useState<Record<string,boolean>>({}),[busy,setBusy]=useState(false),[error,setError]=useState(''),[editing,setEditing]=useState(false);
 const anchor=useRef<HTMLElement|null>(null),mounted=useRef(true);
 const execute=useRef<(request:Request)=>Promise<void>>(async()=>{});
 const queue=useRef<((request:Request)=>void)|null>(null);
 if(!queue.current)queue.current=serialLatest<Request>(request=>execute.current(request),value=>{if(mounted.current)setBusy(value);},e=>{if(mounted.current)setError(workerDisplayError(e,t));});
 execute.current=async request=>{
    if(mounted.current)setError('');
    if(mounted.current)setAvailable({});
    let inspected='';
    try {
     const next=await readSelectedWorker(call,request.kind==='select'?request.runtime:undefined);
     inspected=next.runtime;
     if(mounted.current) {setSelected(next.runtime);setView(next);setAvailable(old=>({...old,[next.runtime]:next.ready}));}
    } catch(e) {
     // Inspection can fail while the native saved preference still exists.
     // Read that preference again; a failed selection read supplies no identity.
     let saved='';
     try {const value=await call<string>('SelectedLocalWorker');if(value==='caelis'||value==='codex')saved=value;}catch{}
     if(mounted.current){setSelected(saved);setView(null);setError(workerDisplayError(e,t));}
    }
    for(const runtime of ['caelis','codex']) {
     if(runtime===inspected)continue;
     try {const candidate=await call<WorkerSettings>('InspectWorkerRuntime',runtime);if(mounted.current)setAvailable(old=>({...old,[runtime]:candidate.ready}));}
     catch {if(mounted.current)setAvailable(old=>({...old,[runtime]:false}));}
    }
 };
 const schedule=(request:Request)=>queue.current!(request);
 useEffect(()=>{mounted.current=true;if(active)schedule({kind:'refresh',runtime:''});return()=>{mounted.current=false;};},[active,refreshKey]);
 const label=locale==='zh-CN';
 return <div className="local-worker-settings">
  <div className="runtime-setting-row local-worker-runtime"><div><strong>{t('settings.machineRuntime')}</strong><p>{label?'仅影响新工作任务；已有任务保持原连接和回执。':'Applies to new work only. Existing tasks keep their original connection and receipts.'}</p></div><div className="runtime-scope-options" role="group" aria-label={t('settings.machineRuntime')}>{['caelis','codex'].map(id=>{const status=workerCardState(id,selected,available[id]),ready=available[id]===true;return <button key={id} disabled={busy||editing} aria-pressed={status==='selected-ready'} onClick={()=>ready?schedule({kind:'select',runtime:id}):onConnections?.(id)}><strong>{executionMethodLabel(id,t)}</strong><span>{status==='saved-waiting'?t('settings.workerPreferenceWaiting'):status==='selected-ready'?'✓ '+t('settings.workerSelectedReady'):status==='ready'?t('settings.workerUseReady'):t('settings.workerNeedsConnection')}</span></button>;})}</div></div>
  {view?.ready&&<div className="runtime-setting-row"><div><strong>{t('runtime.workModel')}</strong><p>{t('settings.localWorkerModelNote')} · {executionMethodLabel(view.runtime,t)}</p></div><ModelSummary value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.workModel')} disabled={busy} onClick={element=>{anchor.current=element;setEditing(true);}}/></div>}
  {view?.ready&&<div className="runtime-setting-row"><div><strong>{t('settings.execution')}</strong><p>{label?'只用于新任务。已有任务保留创建时的执行权限。':'For new tasks only. Existing tasks keep their creation-time permissions.'}</p></div><div className="runtime-scope-options runtime-permission-options" role="group" aria-label={t('settings.execution')}>{([{id:'auto',label:t('settings.approvalAuto'),detail:label?'工作区沙箱与自动审查':'Workspace sandbox and automatic review'},{id:'full-access',label:t('settings.approvalFullAccess'),detail:label?'关闭命令沙箱，不再询问执行权限':'No command sandbox or execution prompts'}] as const).map(option=>{const chosen=(view.work.approvalMode||'auto')===option.id;const unavailable=option.id==='full-access'&&view.runtime==='caelis'&&!view.fullAccessAvailable;return <button key={option.id} className={option.id==='full-access'?'runtime-full-access':undefined} disabled={busy||unavailable} aria-pressed={chosen} onClick={async()=>{if(chosen)return;setBusy(true);setError('');try{setView(await call<WorkerSettings>('SaveLocalWorkerExecutionMode',option.id));}catch(e){setError(e instanceof Error?e.message:t('settings.executionSaveFailed'));}finally{setBusy(false);}}}><strong>{option.id==='full-access'?'⚠ ':''}{option.label}</strong><span>{chosen?'✓ ':''}{option.detail}{unavailable?' · '+(label?'需 Host 显式登记授权':'Requires explicit Host enrollment'):''}</span></button>;})}</div></div>}
  {(error||view&&!view.ready)&&<div className="runtime-worker-recovery"><p className="settings-note" role="status">{error||view?.executionModeIssue||t('settings.workerSetupRequired')}</p><div className="runtime-worker-actions">{onConnections&&<button onClick={()=>onConnections(view?.runtime)}>{t('runtime.manageConnections')}</button>}<button disabled={busy} onClick={()=>schedule({kind:'refresh',runtime:''})}>{t('settings.machineRecheck')}</button></div></div>}
  {editing&&view&&<ModelPicker anchor={anchor.current} inherited title={`${t('runtime.workModel')} · ${executionMethodLabel(view.runtime,t)}`} value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} onClose={()=>setEditing(false)} onReload={()=>{schedule({kind:'refresh',runtime:''});return Promise.resolve();}} onSave={async value=>{const next=await call<WorkerSettings>('SaveLocalWorkerModel',value);setView(next);}}/>}
 </div>;
}
