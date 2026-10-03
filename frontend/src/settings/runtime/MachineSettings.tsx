import { useEffect, useRef, useState } from 'react';
import { backend, desktop } from '../../desktop';
import { useI18n } from '../../i18n';
import type { Machine, MachineInput, RuntimeMutationResult } from '../../backend/contract';
import type { RuntimeSettingsClient } from './types';
import { TeamSettings } from './TeamSettings';
import { runtimeSettingsClient, ConfigurationError } from './client';
import './machines.css';
import {ModelPicker, ModelSummary} from './ModelPicker';
import {SettingsChevron, DesktopTowerIcon, XIcon, ArrowClockwiseIcon, ArrowSquareOutIcon} from '../../SettingsIcons';

type Invoke = typeof backend;
const blank = (): MachineInput => ({id:'',name:'',address:'',port:22,user:'',authentication:'agent',privateKey:'',secret:'',remember:false,trustFingerprint:''});
export function MachineSettings({ call = backend, openTerminal = (id: string) => desktop('OpenMachineTerminal', id), active = true }: {call?:Invoke;openTerminal?:(id:string)=>Promise<void>;active?:boolean}) {
 const {t} = useI18n();
 const [machines,setMachines]=useState<Machine[]>([]),[draft,setDraft]=useState<MachineInput|null>(null),[selected,setSelected]=useState<Machine|null>(null);
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[step,setStep]=useState<'connection'|'work'>('connection');
 const [removeAsk,setRemoveAsk]=useState(false),[editingModel,setEditingModel]=useState(false),[sshOpen,setSSHOpen]=useState(false);
 const modelAnchor=useRef<HTMLElement|null>(null);
 const [advanced,setAdvanced]=useState(false),[advancedBusy,setAdvancedBusy]=useState(false),[terminalBusy,setTerminalBusy]=useState(false);
 const working=useRef(false),editor=useRef<HTMLDivElement>(null),advancedGeneration=useRef(0);
 const reload=async()=>setMachines(await call<Machine[]>('Machines'));
 useEffect(()=>{if(active)void reload().catch(()=>{});},[active,call]);
 useEffect(()=>{if(!active||draft)return;const timer=window.setInterval(()=>void reload().catch(()=>{}),4000);return()=>window.clearInterval(timer);},[active,!!draft,call]);
 useEffect(()=>{
  if(!draft)return;
  const prior=document.activeElement as HTMLElement|null;
  const section=editor.current?.closest('.machine-section'),workspace=section?.closest('.runtime-workspace');
  const siblings=[...workspace?.children??[],...section?.children??[],...document.querySelectorAll('.settings-window>aside')].filter(node=>node!==section&&!node.contains(editor.current!)) as HTMLElement[];
  const previous=siblings.map(node=>node.inert);siblings.forEach(node=>{node.inert=true;});editor.current?.focus();
  const guard=(e:Event)=>e.preventDefault();window.addEventListener('settings-navigate',guard);
  return()=>{siblings.forEach((node,i)=>{node.inert=previous[i];});window.removeEventListener('settings-navigate',guard);if(prior?.isConnected)prior.focus();};
 },[!!draft]);
 useEffect(()=>{if(selected?.state==='trust')editor.current?.querySelector('.machine-fingerprint')?.scrollIntoView({block:'nearest'});},[selected?.state,selected?.fingerprint]);
 const close=()=>{if(busy||terminalBusy)return;advancedGeneration.current++;setDraft(null);setSelected(null);setAdvanced(false);setSSHOpen(false);setEditingModel(false);setRemoveAsk(false);setError('');};
 const patch=(value:Partial<MachineInput>)=>{setDraft(d=>d?{...d,...value,trustFingerprint:''}:d);setSelected(v=>v&&v.state==='trust'?{...v,state:'offline',issue:''}:v);};
 const begin=(v?:Machine)=>{advancedGeneration.current++;setSelected(v??null);setDraft(v?{...blank(),...v,secret:'',trustFingerprint:''}:blank());setStep(v&&v.state!=='offline'?'work':'connection');setError('');setAdvanced(false);setSSHOpen(false);setEditingModel(false);setRemoveAsk(false);};
 const accept=(v:Machine)=>{setSelected(v);setDraft(d=>d?{...d,id:v.id||d.id,secret:['trust','offline'].includes(v.state)||v.issue?d.secret:'',port:v.port||d.port,address:v.address||d.address,user:v.user||d.user}:d);if(v.state!=='trust'&&v.state!=='offline'&&!v.issue)setStep('work');};
 const action=async(fn:()=>Promise<Machine>)=>{if(working.current)return;working.current=true;setBusy(true);setError('');try{accept(await fn());await reload();}catch(e){setError(e instanceof Error?e.message:String(e));}finally{working.current=false;setBusy(false);}};
 const connect=()=>{if(!draft)return;const input=selected?.state==='trust'?{...draft,trustFingerprint:selected.fingerprint}:draft;setDraft(input);void action(()=>call<Machine>('ConnectMachine',input));};
 const refresh=(runtime=selected?.runtime??'')=>{if(selected)void action(()=>call<Machine>('InspectMachine',selected.id,runtime));};
 const open=async()=>{if(!selected||terminalBusy)return;setTerminalBusy(true);setError('');try{await openTerminal(selected.id);}catch(e){setError(e instanceof Error?e.message:String(e));}finally{setTerminalBusy(false);}};
 const readAdvanced=async()=>{if(!selected)return;const id=selected.id,generation=++advancedGeneration.current;setAdvancedBusy(true);try{const v=await call<Machine>('ReadMachineAdvanced',id);if(generation===advancedGeneration.current)setSelected(v);}catch{if(generation===advancedGeneration.current)setSelected(v=>v?{...v,advancedIssue:t('settings.machineAdvancedFailed')}:v);}finally{if(generation===advancedGeneration.current)setAdvancedBusy(false);}};
 const teamClient:RuntimeSettingsClient={...runtimeSettingsClient,async changeTeam(change,revision){const v=await call<RuntimeMutationResult>('ChangeMachineTeam',{id:selected!.id,change:{...change,expectedRevision:revision}});if(v.outcome!=='committed')throw new ConfigurationError(v);}};
 const status=(v:Machine)=>t(v.state==='ready'?'settings.machineReady':v.state==='offline'?'settings.machineOffline':'settings.machineSetup');
 const issue=(value:string)=>{const key=`settings.machineError_${value}`;const translated=t(key as Parameters<typeof t>[0]);return translated===key?t('settings.machineActionFailed'):translated;};
 const terminalLabel=selected?.setup?.state==='missing'?t('settings.machineOpenTerminal'):t('settings.machineOpenTUI');
 return <section className="machine-section">
  <div className="runtime-section-title"><div><h2>{t('settings.machineTitle')}</h2><p className="settings-note">{t('settings.machineSubtitle')}</p></div><button onClick={()=>begin()}>{t('settings.machineAdd')}</button></div>
  {!machines.length?<p className="machine-empty">{t('settings.machineEmpty')}</p>:<ul className="machine-list">{machines.map(v=><li key={v.id}><button className="machine-row" onClick={()=>begin(v)}><span className="machine-icon" aria-hidden="true"><DesktopTowerIcon size={20}/></span><span><strong>{v.name}</strong><small>{v.runtime?v.runtime==='caelis'?'Caelis':'Codex':v.address}</small></span><span className={`machine-status ${v.state==='ready'?'ready':''}`}>{status(v)}</span><SettingsChevron/></button></li>)}</ul>}
  {draft&&<div className="machine-editor-layer" onPointerDown={e=>{if(e.target===e.currentTarget)close();}}><div ref={editor} className="machine-editor" role="dialog" aria-modal="true" aria-labelledby="machine-editor-title" tabIndex={-1} onKeyDown={e=>{
   if((e.target as HTMLElement).closest('.runtime-dialog'))return;
   if(e.key==='Escape'){e.preventDefault();e.stopPropagation();close();}
   if(e.key==='Tab'){const nodes=[...editor.current!.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),summary,a[href]')].filter(n=>n.getClientRects().length);const first=nodes[0],last=nodes.at(-1);if(e.shiftKey&&(document.activeElement===first||document.activeElement===editor.current)){e.preventDefault();last?.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}}
  }}>
   <header><div><h2 id="machine-editor-title">{selected?.name||t('settings.machineAdd')}</h2><p>{step==='connection'?t('settings.machineConnectStep'):t('settings.machineWorkStep')}</p></div><button disabled={busy||terminalBusy} aria-label={t('common.close')} onClick={close}><XIcon size={16} aria-hidden="true"/></button></header>
   <div className="machine-editor-body">
   {error&&<p role="alert" className="inline-error">{issue(error)}</p>}
   {step==='connection'?<form id="machine-connection-form" onSubmit={e=>{e.preventDefault();connect();}}>
    {selected?.issue&&selected.issue!=='confirm_fingerprint'&&<p role="alert" className="inline-error">{issue(selected.issue)}</p>}
    <label>{t('settings.machineAddress')}<input autoFocus required autoComplete="off" placeholder="192.168.1.20" value={draft.address} disabled={busy} onChange={e=>patch({address:e.target.value})}/></label>
    <div className="machine-fields"><label>{t('settings.machineUser')}<input autoComplete="username" value={draft.user} disabled={busy} onChange={e=>patch({user:e.target.value})}/></label><label>{t('settings.machinePort')}<input type="number" min="1" max="65535" required value={draft.port} disabled={busy} onChange={e=>patch({port:Number(e.target.value)})}/></label></div>
    <label>{t('settings.machineAuth')}<select value={draft.authentication} disabled={busy} onChange={e=>patch({authentication:e.target.value,secret:'',remember:false})}><option value="agent">{t('settings.machineAuthAgent')}</option><option value="key">{t('settings.machineAuthKey')}</option><option value="password">{t('settings.machineAuthPassword')}</option></select></label>
    {draft.authentication==='key'&&<label>{t('settings.machineKey')}<div className="machine-key-input"><input aria-label={t('settings.machineKey')} required value={draft.privateKey} disabled={busy} placeholder="/Users/…/.ssh/id_ed25519" onChange={e=>patch({privateKey:e.target.value})}/><button type="button" disabled={busy} onClick={()=>void desktop<string>('PickSSHKey').then(path=>{if(path)patch({privateKey:path});}).catch(()=>setError('invalid_key'))}>{t('runtime.chooseFile')}</button></div></label>}
    {draft.authentication!=='agent'&&<><label>{t(draft.authentication==='password'?'settings.machinePassword':'settings.machinePassphrase')}<input type="password" autoComplete="new-password" value={draft.secret} disabled={busy} onChange={e=>patch({secret:e.target.value})}/></label><label className="machine-remember"><input type="checkbox" checked={draft.remember} disabled={busy} onChange={e=>patch({remember:e.target.checked})}/>{t('settings.machineRemember')}</label></>}
    <details className="machine-extra"><summary><SettingsChevron/>{t('settings.machineOptional')}</summary><label>{t('settings.machineName')}<input value={draft.name} maxLength={80} disabled={busy} onChange={e=>patch({name:e.target.value})}/></label><p className="settings-note">{t('settings.machineAliasHint')}</p></details>
    {selected?.state==='trust'&&<div className="machine-fingerprint" role="status"><strong>{t('settings.machineTrustTitle')}</strong><p>{t('settings.machineTrustNote')}</p><code>{selected.fingerprint}</code></div>}

   </form>:<>
    <div className="machine-status-actions"><p className={`machine-ready-state ${selected?.state==='ready'?'ready':''}`} role="status">{selected&&status(selected)}</p><button className="text-action" disabled={busy||terminalBusy} onClick={()=>refresh()}><ArrowClockwiseIcon size={14} aria-hidden="true"/>{t('settings.machineRecheck')}</button></div>
    <div className="machine-runtime-row"><strong>{t('settings.machineRuntime')}</strong><div className="runtime-segments" role="group" aria-label={t('settings.machineRuntime')}>{(selected?.available?.length?selected.available:['codex','caelis']).map(id=><button key={id} disabled={busy} aria-pressed={selected?.runtime===id} onClick={()=>refresh(id)}>{id==='caelis'?'Caelis':'Codex'}</button>)}</div></div>
    {selected?.runtime&&<>
     {['ready','models'].includes(selected.setup?.state)?<>
      <div className="machine-account"><span>{t('settings.machineAccount')}</span><strong>{t('settings.machineLoggedIn')}</strong></div>
      <div className="runtime-setting-row machine-model-row"><strong>{t('settings.machineWorkModel')}</strong><ModelSummary value={selected.work} models={selected.models??[]} runtimeDefault={selected.runtimeDefault} label={t('settings.machineWorkModel')} disabled={busy} onClick={anchor=>{modelAnchor.current=anchor;setEditingModel(true);}}/></div>
      {!selected.models?.length && <p className="settings-note">{t('settings.machineModelsUnavailable')}</p>}
      <p className="settings-note">{t('settings.machineIndependent')}</p>
     </>:<div className="machine-guide"><strong>{t(selected.setup?.state==='missing'?'settings.machineInstallTitle':selected.setup?.state==='service'?'settings.machineStartServiceTitle':'settings.machineLoginTitle')}</strong><p>{t(selected.setup?.state==='missing'?'settings.machineInstallNote':selected.runtime==='codex'?'settings.machineCodexLoginNote':'settings.machineCaelisLoginNote')}</p><div className="machine-actions"><button disabled={terminalBusy||busy} onClick={()=>void open()}>{terminalBusy?t('settings.machineTerminalOpening'):terminalLabel}</button>{selected.setup?.state==='missing'&&<button disabled={busy||terminalBusy} onClick={()=>void call('OpenMessageLink',selected.runtime==='caelis'?'https://caelis.dev':'https://learn.chatgpt.com/docs/codex/cli').catch(()=>setError('guide_unavailable'))}>{t('runtime.viewOfficialGuide')}</button>}</div></div>}
     {selected.runtime==='caelis'&&<details className="machine-advanced" open={advanced} onToggle={e=>{const next=e.currentTarget.open;setAdvanced(next);if(next){setSSHOpen(false);if(!selected.configuration&&!advancedBusy)void readAdvanced();}}}><summary><SettingsChevron/>{t('settings.machineAdvanced')}</summary>{advancedBusy?<p role="status" className="settings-note">{t('runtime.loadingRuntime')}</p>:selected.configuration?<TeamSettings team={selected.configuration.team} models={selected.configuration.team.models} client={teamClient} onRefresh={readAdvanced}/>:selected.advancedIssue&&<p role="status" className="settings-note">{t('settings.machineAdvancedFailed')}</p>}<div className="machine-other-settings"><strong>{t('runtime.otherSettings')}</strong><p className="settings-note">{t('settings.machineAdvancedTUI')}</p><button className="text-action" disabled={terminalBusy||busy} onClick={()=>void open()}><ArrowSquareOutIcon aria-hidden="true" size={14}/>{terminalLabel}</button>{selected.advancedIssue&&<button className="text-action" disabled={advancedBusy||busy} onClick={()=>void readAdvanced()}>{t('settings.machineRecheck')}</button>}</div></details>}
    </>}
    <details className="machine-extra" open={sshOpen} onToggle={e=>{setSSHOpen(e.currentTarget.open);if(e.currentTarget.open)setAdvanced(false);}}><summary><SettingsChevron/>{t('settings.machineConnectionDetails')}<span className="machine-ssh-summary">{draft.address}</span></summary><dl className="machine-connection-values"><div><dt>{t('settings.machineAddress')}</dt><dd>{draft.address}</dd></div><div><dt>{t('settings.machineUser')}</dt><dd>{draft.user||'—'}</dd></div><div><dt>{t('settings.machinePort')}</dt><dd>{draft.port}</dd></div><div><dt>{t('settings.machineAuth')}</dt><dd>{t(draft.authentication==='agent'?'settings.machineAuthAgent':draft.authentication==='key'?'settings.machineAuthKey':'settings.machineAuthPassword')}</dd></div></dl><div className="machine-actions"><button disabled={busy} onClick={()=>setStep('connection')}>{t('settings.machineEditConnection')}</button><button className="text-action" disabled={busy||terminalBusy} onClick={()=>void open()}>{t('settings.machineOpenTerminal')}</button></div><button className="text-action machine-remove" disabled={busy||terminalBusy} onClick={()=>{if(!removeAsk){setRemoveAsk(true);return;}void (async()=>{setBusy(true);setError('');try{await call('RemoveMachine',selected!.id);await reload();setBusy(false);setDraft(null);setSelected(null);}catch(e){setError(e instanceof Error?e.message:String(e));}finally{setBusy(false);}})();}}>{t(removeAsk?'settings.machineRemoveConfirm':'settings.machineRemove')}</button></details>
    {selected?.issue&&<p role="alert" className="inline-error">{issue(selected.issue)}</p>}
   </>}
   </div>
   <footer><span>{busy?t('settings.machineChecking'):step==='connection'?'':selected?.state==='ready'?t('settings.machineCanStart'):t('settings.machineCanContinueLater')}</span><div className="machine-actions"><button disabled={busy||terminalBusy} onClick={close}>{step==='connection'?t('common.cancel'):selected?.state==='ready'?t('settings.machineDone'):t('settings.machineLater')}</button>{step==='connection'&&<button type="submit" form="machine-connection-form" className="primary" disabled={busy||selected?.issue==='host_key_changed'}>{busy?t('settings.machineConnecting'):selected?.state==='trust'?t('settings.machineTrust'):t('settings.machineConnect')}</button>}</div></footer>
  </div></div>}
  {editingModel && selected && <ModelPicker anchor={modelAnchor.current} inherited title={t('settings.machineWorkModel')} value={selected.work} models={selected.models??[]} runtimeDefault={selected.runtimeDefault} onClose={()=>setEditingModel(false)} onReload={async()=>accept(await call<Machine>('InspectMachine',selected.id,selected.runtime))} onSave={async selection=>{accept(await call<Machine>('SaveMachineModel',{id:selected.id,selection}));await reload().catch(()=>{});}}/>}
 </section>;
}
