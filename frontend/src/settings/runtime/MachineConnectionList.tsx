import type {Machine,WorkerPeerSource,WorkerPairingRequest} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {DesktopTowerIcon,SettingsChevron} from '../../SettingsIcons';
import {DotsThreeIcon} from '@phosphor-icons/react/dist/csr/DotsThree';
import {remoteMachineStatus,remoteMachineLabel} from './machine-status';
import {peerSourceLabel} from './peer-source-label';

function ConnectionRow({name,address,direction,status,online=false,attention=false,onOpen,onMenu}:{name:string;address?:string;direction:string;status:string;online?:boolean;attention?:boolean;onOpen:()=>void;onMenu?:(anchor:HTMLElement)=>void}){
 const {t}=useI18n();
 return <li><button className="machine-row" onClick={onOpen}><span className="machine-icon" aria-hidden="true"><DesktopTowerIcon size={20}/></span><span className="machine-row-copy"><strong>{name}</strong><span className="machine-row-detail"><span className="machine-direction">{direction}</span>{address&&<small title={address}>{address}</small>}</span></span><span className={`machine-status ${online?'ready':''} ${attention?'attention':''}`}><span className="machine-status-dot" aria-hidden="true"/>{status}</span>{!onMenu&&<SettingsChevron/>}</button>{onMenu&&<button className="machine-more" aria-haspopup="dialog" aria-label={t('settings.remoteManage',{name})} onClick={event=>onMenu(event.currentTarget)}><DotsThreeIcon size={20}/></button>}</li>;
}

export function MachineConnectionList({machines,sources,requests,unread,sourcesUnread,onMachine,onMachineMenu,onSource,onSourceMenu,onRequest}:{machines:Machine[];sources:WorkerPeerSource[];requests:WorkerPairingRequest[];unread:Record<string,boolean>;sourcesUnread:boolean;onMachine:(machine:Machine)=>void;onMachineMenu:(machine:Machine,anchor:HTMLElement)=>void;onSource:(source:WorkerPeerSource)=>void;onSourceMenu:(source:WorkerPeerSource,anchor:HTMLElement)=>void;onRequest:(request:WorkerPairingRequest)=>void}){
 const {t}=useI18n();
 // A name or address is not identity proof. Keep each original direction and
 // request/grant ID; never merge unrelated authorities by their display label.
 return <ul className="machine-list remote-machine-list">
  {requests.map(request=><ConnectionRow key={`request:${request.id}`} name={request.name||t('settings.botConnectedSource')} address={request.address} direction={t('settings.remoteIncomingDirection')} status={t('settings.remoteNeedsDecision')} attention onOpen={()=>onRequest(request)}/>)}
  {machines.map(machine=><ConnectionRow key={`outgoing:${machine.id}`} name={machine.name} address={machine.address.startsWith('ssh://')?'SSH · '+machine.address.slice(6).replace(/:9317$/,''):machine.address.replace(/^https?:\/\//,'')} direction={t('settings.remoteOutgoingDirection')} status={t(unread[machine.id]?'settings.remoteCheckUnreadable':remoteMachineLabel(machine))} online={remoteMachineStatus(machine)==='online'&&!unread[machine.id]} onOpen={()=>onMachine(machine)} onMenu={anchor=>onMachineMenu(machine,anchor)}/>)}
  {sources.map(source=><ConnectionRow key={`incoming:${source.id}`} name={peerSourceLabel(source,sources,t('settings.botConnectedSource'))} direction={t('settings.remoteAllowedDirection')} status={t(sourcesUnread?'settings.remoteCheckUnreadable':'settings.remoteAllowed')} onOpen={()=>onSource(source)} onMenu={anchor=>onSourceMenu(source,anchor)}/>)}
 </ul>;
}
