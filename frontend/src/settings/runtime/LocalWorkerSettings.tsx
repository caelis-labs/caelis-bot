import {useEffect, useRef, useState} from 'react';
import {backend} from '../../desktop';
import type {LocalWorkerSettings as WorkerSettings} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {ModelPicker, ModelSummary} from './ModelPicker';
import {executionMethodLabel} from './state';
import {serialLatest} from './worker-refresh';

type Request = {kind:'refresh'|'select'; runtime:string};

export function LocalWorkerSettings({active=true,refreshKey=0,onConnections,call=backend}:{active?:boolean;refreshKey?:number;onConnections?:(runtime?:string)=>void;onTeam?:()=>void;call?:typeof backend}) {
 const {t,locale}=useI18n();
 const [view,setView]=useState<WorkerSettings|null>(null),[selected,setSelected]=useState(''),[available,setAvailable]=useState<Record<string,boolean>>({}),[busy,setBusy]=useState(false),[error,setError]=useState(''),[editing,setEditing]=useState(false);
 const anchor=useRef<HTMLElement|null>(null),mounted=useRef(true);
 const execute=useRef<(request:Request)=>Promise<void>>(async()=>{});
 const queue=useRef<((request:Request)=>void)|null>(null);
 if(!queue.current)queue.current=serialLatest<Request>(request=>execute.current(request),value=>{if(mounted.current)setBusy(value);},e=>{if(mounted.current)setError(e instanceof Error?e.message:t('settings.workerSetupRequired'));});
 execute.current=async request=>{
    if(mounted.current)setError('');
    let inspected=request.runtime;
    try {
     const selectedRuntime=request.kind==='refresh'?await call<string>('SelectedLocalWorker').catch(()=>selected||'codex'):request.runtime;
     inspected=selectedRuntime;
     if(mounted.current&&request.kind==='refresh')setSelected(selectedRuntime);
     const next=await call<WorkerSettings>(request.kind==='select'?'SelectLocalWorker':'InspectWorkerRuntime',request.kind==='refresh'?selectedRuntime:request.runtime);
     if(mounted.current) {setView(next);if(request.kind==='select')setSelected(next.runtime);setAvailable(old=>({...old,[next.runtime]:next.ready}));}
     if(request.kind==='refresh') {
      const other=next.runtime==='caelis'?'codex':'caelis';
      try {const candidate=await call<WorkerSettings>('InspectWorkerRuntime',other);if(mounted.current)setAvailable(old=>({...old,[other]:candidate.ready}));}
      catch {if(mounted.current)setAvailable(old=>({...old,[other]:false}));}
     }
    } catch(e) {
     if(mounted.current){setError(e instanceof Error?e.message:t('settings.workerSetupRequired'));setAvailable(old=>({...old,[inspected]:false}));if(request.kind==='refresh')setView(null);}
     if(request.kind==='refresh') {
      const other=inspected==='caelis'?'codex':'caelis';
      try {const candidate=await call<WorkerSettings>('InspectWorkerRuntime',other);if(mounted.current)setAvailable(old=>({...old,[other]:candidate.ready}));}
      catch {if(mounted.current)setAvailable(old=>({...old,[other]:false}));}
     }
    }
 };
 const schedule=(request:Request)=>queue.current!(request);
 useEffect(()=>{mounted.current=true;if(active)schedule({kind:'refresh',runtime:''});return()=>{mounted.current=false;};},[active,refreshKey]);
 const label=locale==='zh-CN';
 return <div className="local-worker-settings">
  <div className="runtime-setting-row local-worker-runtime"><div><strong>{t('settings.machineRuntime')}</strong><p>{label?'仅影响新工作任务；已有任务保持原连接和回执。':'Applies to new work only. Existing tasks keep their original connection and receipts.'}</p></div><div className="runtime-scope-options" role="group" aria-label={t('settings.machineRuntime')}>{['caelis','codex'].map(id=><button key={id} disabled={busy||editing} aria-pressed={selected===id} onClick={()=>available[id]===false?onConnections?.(id):schedule({kind:'select',runtime:id})}><strong>{executionMethodLabel(id,t)}</strong><span>{available[id]===false?(selected===id?'✓ ':'')+(label?'需要先连接':'Set up connection'):selected===id?'✓ '+(label?'已选用于新任务':'Selected for new work'):label?'选择用于新任务':'Use for new work'}</span></button>)}</div></div>
  {view?.ready&&<div className="runtime-setting-row"><div><strong>{t('runtime.workModel')}</strong><p>{t('settings.localWorkerModelNote')} · {executionMethodLabel(view.runtime,t)}</p></div><ModelSummary value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.workModel')} disabled={busy} onClick={element=>{anchor.current=element;setEditing(true);}}/></div>}
  {(error||view&&!view.ready)&&<div className="runtime-worker-recovery"><p className="settings-note" role="status">{error||t('settings.workerSetupRequired')}</p><div className="runtime-worker-actions">{onConnections&&<button onClick={()=>onConnections(view?.runtime)}>{t('runtime.manageConnections')}</button>}<button disabled={busy} onClick={()=>schedule({kind:'refresh',runtime:''})}>{t('settings.machineRecheck')}</button></div></div>}
  {editing&&view&&<ModelPicker anchor={anchor.current} inherited title={`${t('runtime.workModel')} · ${executionMethodLabel(view.runtime,t)}`} value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} onClose={()=>setEditing(false)} onReload={()=>{schedule({kind:'refresh',runtime:''});return Promise.resolve();}} onSave={async value=>{const next=await call<WorkerSettings>('SaveLocalWorkerModel',value);setView(next);}}/>}
 </div>;
}
