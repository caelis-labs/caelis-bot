import {useEffect,useState} from 'react';
import {backend,desktop} from '../../desktop';
import type {NodeCatalog,NotebookSyncSettings as NotebookSyncPreferences,NotebookSyncState} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';

export function NotebookSyncSettings({catalog,call=backend,host=desktop,active=true}:{catalog:NodeCatalog;call?:typeof backend;host?:typeof desktop;active?:boolean}) {
 const {t}=useI18n();
 const [settings,setSettings]=useState<NotebookSyncPreferences|null>(null),[state,setState]=useState<NotebookSyncState|null>(null);
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[saved,setSaved]=useState(false),[switching,setSwitching]=useState('');
 const [unconfirmedSwitch,setUnconfirmedSwitch]=useState(false);
 const [dirty,setDirty]=useState(false);
 const source=catalog.activeBotNodeId||'local';
 const candidates=catalog.nodes.filter(node=>(node.join==='ssh'||node.join==='local')&&node.id!==source);
 const name=(id:string)=>catalog.nodes.find(node=>node.id===id)?.label||id;
 const reload=async()=>{
  try {
   const [preferences,next]=await Promise.all([call<NotebookSyncPreferences>('NotebookSyncSettings'),call<NotebookSyncState>('NotebookSyncState')]);
   if(!Array.isArray(preferences.targets)||!Array.isArray(next.targets))throw new Error("Notebook settings unavailable");
   setState(next);
   if(!dirty)setSettings({...preferences,targets:preferences.targets.filter(target=>target.nodeId!==source)});
  } catch { setError(t('runtime.notebookUnavailable')); }
 };
 useEffect(()=>{if(active)void reload();},[active,catalog.revision]);
 useEffect(()=>{if(!active)return;const timer=setInterval(()=>{void call<NotebookSyncState>('NotebookSyncState').then(next=>{if(Array.isArray(next.targets))setState(next);}).catch(()=>{});},10000);return()=>clearInterval(timer);},[active,call]);
 const edit=(next:NotebookSyncPreferences)=>{setSettings(next);setDirty(true);setSaved(false);};
 const action=async(kind:'save'|'sync'|'switch',id='')=>{
  if(busy||!settings)return;setBusy(true);setError('');setSaved(false);
  const previousOperation=state?.targets.find(target=>target.nodeId===id)?.operationId;
  try {
   if(kind==='save') {
    const preferences=await call<NotebookSyncPreferences>('SaveNotebookSyncSettings',settings);setSettings(preferences);setDirty(false);setSaved(true);
   } else { await call<NotebookSyncState>(kind==='sync'?'SyncNotebook':'SwitchNotebookNode',id);setSwitching(''); }
   const next=await call<NotebookSyncState>('NotebookSyncState');setState(next);
   if(kind==='switch'&&next.targets.some(target=>target.phase==='switched'||target.phase==='restart-required'))await host('RestartForRuntime');
  } catch {
   let blocked=false;
   try {
    const next=await call<NotebookSyncState>('NotebookSyncState');setState(next);
    const target=next.targets.find(target=>target.nodeId===id);
    // Only this attempt's confirmed pre-stop return to ready permits retry.
    // A stale ready observation cannot resolve a lost switch response.
    blocked=next.sourceNodeId===source&&target?.phase==='ready'&&!!target.operationId&&target.operationId!==previousOperation;
   } catch {/* Preserve uncertainty if the original outcome cannot be read. */}
   if(kind==='switch')setUnconfirmedSwitch(!blocked);
   setError(t(kind==='switch'?(blocked?'runtime.notebookSwitchBlocked':'runtime.notebookSwitchUnconfirmed'):'runtime.notebookActionFailed'));
  }
  finally { setBusy(false); }
 };
 const pending=state?.targets.some(target=>target.phase!=='ready');
 const moved=state?.targets.some(target=>(target.phase==='switched'||target.phase==='restart-required')&&target.nodeId!==source);
 const locked=unconfirmedSwitch||!!pending&&state?.sourceNodeId===source;
 return <details className="runtime-advanced notebook-sync-settings"><summary>{t('runtime.notebookTitle')}</summary>
  <p className="settings-note">{t('runtime.notebookDescription')}</p>
  {settings&&<>
   <label className="runtime-setting-row"><strong>{t('runtime.notebookEnabled')}</strong><input type="checkbox" disabled={busy||locked} checked={settings.enabled} onChange={event=>edit({...settings,enabled:event.target.checked})}/></label>
   <label className="runtime-setting-row"><strong>{t('runtime.notebookInterval')}</strong><input type="number" min="1" max="1440" disabled={busy||locked} value={settings.intervalMinutes} onChange={event=>edit({...settings,intervalMinutes:Number(event.target.value)})}/></label>
   {candidates.map(node=>{
    const selected=settings.targets.find(target=>target.nodeId===node.id);
    const status=state?.targets.find(target=>target.nodeId===node.id);
    return <div key={node.id} className="notebook-backup-row">
     <label><input type="checkbox" disabled={busy||locked} checked={!!selected} onChange={event=>edit({...settings,targets:event.target.checked?[...settings.targets,{nodeId:node.id,backend:node.runtimes[0]?.backend||'codex'}]:settings.targets.filter(target=>target.nodeId!==node.id)})}/><strong>{node.label}</strong></label>
     {selected&&<select aria-label={t('settings.workerNodeBackend')} value={selected.backend} disabled={busy||locked} onChange={event=>edit({...settings,targets:settings.targets.map(target=>target.nodeId===node.id?{...target,backend:event.target.value as 'codex'|'caelis'}:target)})}>{node.runtimes.map(runtime=><option key={runtime.backend} value={runtime.backend}>{runtime.backend==='caelis'?'Caelis':'Codex'}</option>)}</select>}
     <p className="settings-note">{status?.lastSuccess?t('runtime.notebookLastSuccess',{time:new Date(status.lastSuccess).toLocaleString()}):t('runtime.notebookNeverSynced')}</p>
     {status?.error&&<p role="status" className="settings-note">{t('runtime.notebookBackupFailed')}</p>}
     {selected&&settings.enabled&&status&&<div className="setup-end"><button disabled={busy||dirty||locked} onClick={()=>void action('sync',node.id)}>{t('runtime.notebookSyncNow')}</button><button disabled={busy||dirty||locked} onClick={()=>setSwitching(node.id)}>{t('runtime.notebookSwitch')}</button></div>}
    </div>;
   })}
   {!candidates.length&&<p className="settings-note">{t('runtime.notebookAddNode')}</p>}
   {moved&&<p role="status" className="runtime-callout">{t('runtime.notebookRestart')} <button disabled={busy} onClick={()=>void host('RestartForRuntime').catch(()=>setError(t('runtime.notebookActionFailed')))}>{t('runtime.switchAndRestart')}</button></p>}
   {locked&&!moved&&<p role="alert" className="runtime-callout">{t('runtime.notebookSwitchUnconfirmed')}</p>}
   <button disabled={busy||locked||settings.enabled&&(!settings.targets.length||!Number.isInteger(settings.intervalMinutes)||settings.intervalMinutes<1||settings.intervalMinutes>1440)} onClick={()=>void action('save')}>{busy?t('runtime.saving'):t('common.save')}</button>
   {saved&&<p role="status" className="settings-note">{t('runtime.notebookSaved')}</p>}
  </>}
  {error&&!switching&&<p role="alert" className="inline-error">{error}</p>}
  {switching&&<SettingsDialog title={t('runtime.notebookSwitchTitle',{name:name(switching)})} busy={busy} onClose={()=>setSwitching('')}><p className="settings-note">{t('runtime.notebookSwitchDescription')}</p>{error&&<p role="alert" className="inline-error">{error}</p>}<div className="setup-end"><button disabled={busy} onClick={()=>setSwitching('')}>{t('common.cancel')}</button><button disabled={busy||locked} onClick={()=>void action('switch',switching)}>{t('runtime.notebookSwitch')}</button></div></SettingsDialog>}
 </details>;
}
