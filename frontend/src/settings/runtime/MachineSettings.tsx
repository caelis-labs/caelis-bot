import {useEffect,useRef,useState} from 'react';
import {backend,desktop} from '../../desktop';
import {useI18n} from '../../i18n';
import type {Machine,WorkerSharing} from '../../backend/contract';
import {SetupLoading,SetupButton} from '../../SetupLoading';
import {ArrowClockwiseIcon} from '@phosphor-icons/react/dist/csr/ArrowClockwise';
import {GearSixIcon} from '@phosphor-icons/react/dist/csr/GearSix';
import {SettingsActionMenu} from '../SettingsActionMenu';
import {SSHConfigDialog} from './SSHConnection';
import {BotConnectionDialog,MachineRequestsDialog} from './BotConnections';
import {incomingProblem} from './pairing-errors';
import {remoteMachineStatus,remoteMachineLabel} from './machine-status';
import {DotsThreeIcon} from '@phosphor-icons/react/dist/csr/DotsThree';
import {PlusIcon} from '@phosphor-icons/react/dist/csr/Plus';
import {BellIcon} from '@phosphor-icons/react/dist/csr/Bell';
import {SettingsChevron,DesktopTowerIcon} from '../../SettingsIcons';

export function MachineSettings({standalone=false,call=backend,active=true}:{standalone?:boolean;call?:typeof backend;active?:boolean}){
 const {t}=useI18n();
 const [machines,setMachines]=useState<Machine[]>([]),[loaded,setLoaded]=useState(false),[listError,setListError]=useState(false),[checking,setChecking]=useState(false);
 const [requestClock,setRequestClock]=useState(Date.now),[unread,setUnread]=useState<Record<string,boolean>>({});
 const [sharing,setSharing]=useState<WorkerSharing|null>(null),[editor,setEditor]=useState<Machine|true|null>(null),[requests,setRequests]=useState(false);
 const [sshConfig,setSSHConfig]=useState(false),[menu,setMenu]=useState<{machine:Machine;anchor:HTMLElement}>();
 const [revoking,setRevoking]=useState(''),[sourceError,setSourceError]=useState<''|'read'|'revoke'>('');
 const generation=useRef(0),sourceGeneration=useRef(0),sourceMutation=useRef(false),refreshing=useRef(false),refreshCycle=useRef(0);
 const inspections=useRef<Record<string,number>>({});
 const reload=async(check=false)=>{
  const epoch=++generation.current;
  try{
   const values=await call<Machine[]>('Machines');
   if(generation.current!==epoch)return;
   setMachines(values);setLoaded(true);setListError(false);
   // One page-entry/manual check with a fixed concurrency bound. Never poll
   // for a human; a failed read cannot replace the original request/proof.
   if(check){let next=0;await Promise.all(Array.from({length:Math.min(3,values.length)},async()=>{while(next<values.length&&generation.current===epoch){const v=values[next++];if(v.archived)continue;try{const fresh=await call<Machine>('InspectMachine',v.id,'');if(generation.current===epoch){setMachines(old=>old.map(item=>item.id===fresh.id?fresh:item));setUnread(old=>({...old,[v.id]:false}))}}catch{if(generation.current===epoch)setUnread(old=>({...old,[v.id]:true}))}}}))}
  }catch{if(generation.current===epoch){setLoaded(true);setListError(true)}}
 };
 const readSharing=async()=>{if(sourceMutation.current)return;const epoch=++sourceGeneration.current;try{const value=await call<WorkerSharing>('RefreshWorkerSharing');if(sourceGeneration.current===epoch){setSharing(value);setSourceError('')}}catch{if(sourceGeneration.current===epoch)setSourceError('read')}};
 const refresh=async()=>{if(refreshing.current)return;const cycle=++refreshCycle.current;refreshing.current=true;setChecking(true);try{await Promise.all([reload(true),readSharing()])}finally{if(refreshCycle.current===cycle){refreshing.current=false;setChecking(false)}}};
 const inspectOne=async(id:string)=>{const epoch=generation.current,ticket=(inspections.current[id]||0)+1;inspections.current[id]=ticket;const current=()=>generation.current===epoch&&inspections.current[id]===ticket;try{const fresh=await call<Machine>('InspectMachine',id,'');if(current()){setMachines(old=>old.map(v=>v.id===id?fresh:v));setUnread(old=>({...old,[id]:false}))}}catch{if(current())setUnread(old=>({...old,[id]:true}))}};
 useEffect(()=>{if(active)void refresh();return()=>{generation.current++;sourceGeneration.current++;refreshCycle.current++;refreshing.current=false}},[active,call]);
 useEffect(()=>{const focus=()=>{if(active&&!document.hidden)void refresh()};window.addEventListener('focus',focus);window.addEventListener('settings-focus',focus);return()=>{window.removeEventListener('focus',focus);window.removeEventListener('settings-focus',focus)}},[active,call]);
 const revoke=async(id:string)=>{if(sourceMutation.current)return;sourceMutation.current=true;sourceGeneration.current++;setRevoking(id);setSourceError('');try{await call('RevokeWorkerPeer',id);setSharing(old=>old?{...old,sources:old.sources.filter(v=>v.id!==id)}:old)}catch{setSourceError('revoke')}finally{sourceGeneration.current++;sourceMutation.current=false;setRevoking('')}};
 useEffect(()=>{const next=Math.min(...[...(sharing?.requests||[]).map(v=>v.expiresAt*1000),...machines.filter(v=>v.pairingStatus==='approved'&&v.state==='ready').map(v=>(v.checkedAt||0)+60000)].filter(v=>v>requestClock));if(!Number.isFinite(next))return;const timer=setTimeout(()=>setRequestClock(Date.now()),Math.max(0,next-Date.now()+20));return()=>clearTimeout(timer)},[sharing,machines,requestClock]);
 const visible=machines.filter(v=>!v.archived),pending=sharing?.requests?.filter(v=>v.expiresAt*1000>Date.now())||[];
 return <section className={`machine-section ${standalone?'machine-page':''}`} aria-label={t('settings.machines')}>
  <div className="remote-list-toolbar">
   {pending.length>0&&<button className="remote-inbox-action" onClick={()=>setRequests(true)} title={t('settings.remoteRequests',{count:pending.length})}><BellIcon size={17} aria-hidden="true"/>{t('settings.remoteRequests',{count:pending.length})}</button>}
   <div className="remote-primary-actions">
    <button className="remote-add" title={t('settings.remoteAdd')} aria-label={t('settings.remoteAdd')} onClick={()=>setEditor(true)}><PlusIcon size={18}/></button>
    <button className="remote-ssh-entry" title={t('settings.sshConfigTitle')} aria-label={t('settings.sshConfigTitle')} onClick={()=>setSSHConfig(true)}><GearSixIcon size={18}/></button>
    <SetupButton className="remote-refresh" busy={checking} disabled={!loaded} onClick={()=>void refresh()} aria-label={t('settings.machineRecheck')} title={t('settings.machineRecheck')}><ArrowClockwiseIcon size={18}/></SetupButton>
   </div>
  </div>
  {!loaded?<SetupLoading/>:listError&&visible.length===0?<div className="remote-inline-state" role="status"><span>{t('settings.remoteListUnavailable')}</span><button className="text-action" disabled={checking} onClick={()=>void refresh()}>{t('settings.remoteRetry')}</button></div>:visible.length===0?<div className="remote-empty"><DesktopTowerIcon size={32}/><h3>{t('settings.remoteEmptyTitle')}</h3><p>{t('settings.remoteEmptyHelp')}</p><button className="primary" onClick={()=>setEditor(true)}><PlusIcon size={16}/>{t('settings.remoteAdd')}</button></div>:<ul className="machine-list remote-machine-list">{visible.map(v=>{
   const state=remoteMachineStatus(v);
   return <li key={v.id}><button className="machine-row" onClick={()=>setEditor(v)}><span className="machine-icon" aria-hidden="true"><DesktopTowerIcon size={20}/></span><span className="machine-row-copy"><strong>{v.name}</strong><small>{v.address.startsWith('ssh://')?'SSH · '+v.address.slice(6).replace(/:9317$/,''):v.address.replace(/^https?:\/\//,'')}</small></span><span className={`machine-status ${state==='online'&&!unread[v.id]?'ready':''}`}><span className="machine-status-dot" aria-hidden="true"/>{t(unread[v.id]?'settings.remoteCheckUnreadable':remoteMachineLabel(v))}</span></button><button className="machine-more" aria-haspopup="dialog" aria-label={t('settings.remoteManage',{name:v.name})} onClick={event=>setMenu({machine:v,anchor:event.currentTarget})}><DotsThreeIcon size={20}/></button></li>
  })}</ul>}
  {listError&&visible.length>0&&<div role="status" className="remote-inline-state"><span>{t('settings.remoteListStale')}</span></div>}
  {(sourceError||sharing?.issue)&&<div role="status" className="remote-inline-state remote-receive-issue"><span>{t(sourceError==='revoke'?'settings.remoteRevokeUnconfirmed':sourceError==='read'?'settings.remoteInboxUnreadable':incomingProblem(sharing?.issue||''))}</span><button className="text-action" disabled={checking} onClick={()=>void refresh()}>{t('settings.remoteRetry')}</button>{sharing?.issue?.startsWith('worker_peer_state_')&&<button className="text-action" onClick={()=>void desktop('ExportDiagnostics')}>{t('settings.remoteSaveDiagnostics')}</button>}</div>}
  {!!sharing?.sources?.length&&<details className="remote-authorizations"><summary><SettingsChevron/>{t('settings.botConnectedSources')}</summary><ul className="remote-source-list">{sharing.sources.map(source=><li key={source.id}><span><strong>{source.name||t('settings.botConnectedSource')}</strong><small>{source.id.slice(-8)}</small></span><button disabled={!!revoking} onClick={()=>void revoke(source.id)}>{t('settings.botRevoke')}</button></li>)}</ul><p className="settings-note">{t('settings.botRevokeDirection')}</p></details>}
  {menu&&<SettingsActionMenu anchor={menu.anchor} title={t('settings.remoteManage',{name:menu.machine.name})} onClose={()=>setMenu(undefined)} items={[{label:t('settings.remoteManageTitle'),action:()=>setEditor(menu.machine)},{label:t('settings.machineRecheck'),action:()=>{void inspectOne(menu.machine.id)}}]}/>}
  {sshConfig&&<SSHConfigDialog call={call} onClose={()=>setSSHConfig(false)} onSaved={()=>{void refresh()}}/>}
  {editor&&<BotConnectionDialog machines={machines} initial={editor===true?undefined:editor} call={call} onClose={()=>setEditor(null)} onSaved={fresh=>{if(fresh){setMachines(old=>old.some(v=>v.id===fresh.id)?old.map(v=>v.id===fresh.id?fresh:v):[...old,fresh]);setEditor(fresh)}else{setEditor(null);void reload()}}} onChanged={async fresh=>{if(fresh){setMachines(old=>old.map(v=>v.id===fresh.id?fresh:v));setUnread(old=>({...old,[fresh.id]:false}))}else await reload()}}/>}
  {requests&&<MachineRequestsDialog value={sharing} readError={sourceError==='read'} call={call} onClose={()=>setRequests(false)} onChanged={readSharing}/>}
 </section>;
}
