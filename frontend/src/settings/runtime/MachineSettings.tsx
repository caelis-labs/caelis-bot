import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import {useI18n} from '../../i18n';
import type {Machine,WorkerSharing} from '../../backend/contract';
import {BotConnectionDialog,MachineRequestsDialog} from './BotConnections';
import {remoteMachineStatus} from './machine-status';
import {DotsThreeIcon} from '@phosphor-icons/react/dist/csr/DotsThree';
import {SettingsChevron,DesktopTowerIcon} from '../../SettingsIcons';

export function MachineSettings({standalone=false,call=backend,active=true}:{standalone?:boolean;call?:typeof backend;active?:boolean}){
 const {t}=useI18n();
 const [machines,setMachines]=useState<Machine[]>([]),[loaded,setLoaded]=useState(false),[listError,setListError]=useState(false);
 const [sharing,setSharing]=useState<WorkerSharing|null>(null),[editor,setEditor]=useState<Machine|true|null>(null),[requests,setRequests]=useState(false);
 const [revoking,setRevoking]=useState(''),[sourceError,setSourceError]=useState(false);
 const generation=useRef(0),sourceGeneration=useRef(0),sourceMutation=useRef(false);
 const reload=async(check=false)=>{
  const epoch=++generation.current;
  try{
   const values=await call<Machine[]>('Machines');
   if(generation.current===epoch){setMachines(values);setLoaded(true);setListError(false)}
   // One lazy check per page entry, with at most three concurrent reads. Never
   // wait for a human or start a background pairing/health polling loop.
   if(check&&generation.current===epoch){let next=0;await Promise.all(Array.from({length:Math.min(3,values.length)},async()=>{while(next<values.length&&generation.current===epoch){const v=values[next++];if(v.archived)continue;try{const fresh=await call<Machine>('InspectMachine',v.id,'');if(generation.current===epoch)setMachines(old=>old.map(item=>item.id===fresh.id?fresh:item))}catch{/* Preserve the existing local view and proof. */}}}))}
  }catch{if(generation.current===epoch){setLoaded(true);setListError(true)}}
 };
 const readSharing=async()=>{if(sourceMutation.current)return;const epoch=sourceGeneration.current;try{const value=await call<WorkerSharing>('WorkerSharing');if(sourceGeneration.current===epoch){setSharing(value);setSourceError(false)}}catch{if(sourceGeneration.current===epoch)setSourceError(true)}};
 useEffect(()=>{if(active){void reload(true);void readSharing()}return()=>{generation.current++;sourceGeneration.current++}},[active,call]);
 const revoke=async(id:string)=>{if(sourceMutation.current)return;sourceMutation.current=true;sourceGeneration.current++;setRevoking(id);setSourceError(false);try{await call('RevokeWorkerPeer',id);setSharing(old=>old?{...old,sources:old.sources.filter(v=>v.id!==id)}:old)}catch{setSourceError(true)}finally{sourceGeneration.current++;sourceMutation.current=false;setRevoking('')}};
 return <section className={`machine-section ${standalone?'machine-page':''}`}>
  <div className="remote-toolbar">{standalone?<h1>{t('settings.machines')}</h1>:<h2>{t('settings.machines')}</h2>}<div className="remote-primary-actions"><button onClick={()=>{setRequests(true);void readSharing()}}>{sharing?.requests?.length?t('settings.remoteRequests',{count:sharing.requests.length}):t('settings.remoteIncoming')}</button><button className="remote-add" onClick={()=>setEditor(true)}>{t('settings.remoteAdd')}</button></div></div>
  {!loaded?<p className="remote-empty" role="status">{t('settings.remoteLoading')}</p>:machines.filter(v=>!v.archived).length===0?!listError&&<p className="remote-empty">{t('settings.remoteEmpty')}</p>:<ul className="machine-list remote-machine-list">{machines.filter(v=>!v.archived).map(v=>{
   const state=remoteMachineStatus(v);
   return <li key={v.id}><button className="machine-row" onClick={()=>setEditor(v)}><span className="machine-icon" aria-hidden="true"><DesktopTowerIcon size={20}/></span><span><strong>{v.name}</strong><small>{v.address}</small></span><span className={`machine-status ${state==='online'?'ready':''}`}>{t(state==='online'?'settings.remoteOnline':state==='authorization'?'settings.remoteNeedsAuthorization':state==='setup'?'settings.remoteNeedsSetup':state==='unchecked'?'settings.remoteUnchecked':'settings.remoteOffline')}</span></button><button className="machine-more" aria-haspopup="dialog" aria-label={t('settings.remoteManage',{name:v.name})} onClick={()=>setEditor(v)}><DotsThreeIcon size={20}/></button></li>
  })}</ul>}
  {listError&&<p role="status" className="settings-note">{t('settings.remoteListUnavailable')}</p>}
  <div className="remote-secondary"><button className="text-action" onClick={()=>{void reload(true);void readSharing()}}>{t('settings.machineRecheck')}</button></div>
  {!!sharing?.sources?.length&&<details className="remote-authorizations"><summary><SettingsChevron/>{t('settings.botConnectedSources')}</summary><ul className="remote-source-list">{sharing.sources.map(source=><li key={source.id}><span><strong>{source.name||t('settings.botConnectedSource')}</strong><small>{source.id.slice(-8)}</small></span><button disabled={!!revoking} onClick={()=>void revoke(source.id)}>{t('settings.botRevoke')}</button></li>)}</ul><p className="settings-note">{t('settings.botRevokeDirection')}</p>{sourceError&&<p role="alert" className="inline-error">{t('settings.botReadFailed')}</p>}</details>}
  {editor&&<BotConnectionDialog machines={machines} initial={editor===true?undefined:editor} call={call} onClose={()=>setEditor(null)} onSaved={()=>{setEditor(null);void reload()}} onChanged={()=>reload()}/>}
  {requests&&<MachineRequestsDialog value={sharing} call={call} onClose={()=>{setRequests(false);void readSharing()}} onChanged={async()=>{await readSharing()}}/>}
 </section>;
}
