import {useEffect, useRef, useState} from 'react';
import {backend} from '../../desktop';
import type {LocalWorkerSettings as WorkerSettings} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {ModelPicker, ModelSummary} from './ModelPicker';

export function LocalWorkerSettings({active=true,onConnections,call=backend}:{active?:boolean;onConnections?:()=>void;call?:typeof backend}) {
 const {t}=useI18n();
 const [view,setView]=useState<WorkerSettings|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[editing,setEditing]=useState(false);
 const anchor=useRef<HTMLElement|null>(null),pending=useRef(false),epoch=useRef(0);
 const refresh=async(runtime='')=>{
  if(pending.current)return;
  pending.current=true;const generation=++epoch.current;setBusy(true);setError('');
  try {const next=await call<WorkerSettings>('InspectLocalWorker',runtime);if(generation===epoch.current)setView(next);}
  catch {if(generation===epoch.current)setError(t('settings.workerSetupRequired'));}
  finally {pending.current=false;if(generation===epoch.current)setBusy(false);}
 };
 useEffect(()=>{if(active)void refresh();return()=>{epoch.current++;};},[active]);
 return <>
  <div className="runtime-setting-row local-worker-runtime"><div><strong>{t('settings.machineRuntime')}</strong><p>{t('settings.workerSwitchNote')}</p></div><div className="runtime-segments" role="group" aria-label={t('settings.machineRuntime')}>{['codex','caelis'].map(id=><button key={id} disabled={busy||editing} aria-pressed={view?.runtime===id} onClick={()=>void refresh(id)}>{id==='caelis'?'Caelis':'Codex'}</button>)}</div></div>
  {view?.ready&&<div className="runtime-setting-row"><div><strong>{t('runtime.workModel')}</strong><p>{t('settings.localWorkerModelNote')}</p></div><ModelSummary value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.workModel')} disabled={busy} onClick={element=>{anchor.current=element;setEditing(true);}}/></div>}
  {(error||view&&!view.ready)&&<div className="runtime-worker-recovery"><p className="settings-note" role="status">{error||t('settings.workerSetupRequired')}</p><div className="runtime-worker-actions">{onConnections&&<button onClick={onConnections}>{t('runtime.manageConnections')}</button>}<button className="text-action" disabled={busy} onClick={()=>void refresh()}>{t('settings.machineRecheck')}</button></div></div>}
  {editing&&view&&<ModelPicker anchor={anchor.current} inherited title={t('runtime.workModel')} value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} onClose={()=>setEditing(false)} onReload={()=>refresh()} onSave={async value=>{const next=await call<WorkerSettings>('SaveLocalWorkerModel',value);setView(next);}}/>}
 </>;
}
