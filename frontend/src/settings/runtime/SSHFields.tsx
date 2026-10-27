import {useEffect,useState} from 'react';
import {backend,desktop} from '../../desktop';
import type {SSHHosts,SSHIdentity,SSHTarget} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {NumericPortInput} from './PortInput';
import {SettingsSelect} from '../../SettingsSelect';
import {FolderOpenIcon} from '@phosphor-icons/react/dist/csr/FolderOpen';
import {isWebSurface} from '../../transport';
import {sshProblem} from './SSHConnection';

export function SSHFields({value,onChange,disabled=false,call=backend}:{value:SSHTarget;onChange:(v:SSHTarget)=>void;disabled?:boolean;call?:typeof backend}){
 const {t}=useI18n(),[hosts,setHosts]=useState<string[]>([]),[issue,setIssue]=useState(''),[keyName,setKeyName]=useState('');
 const manual=!!value.authentication;
 useEffect(()=>{let live=true;const read=async()=>{try{const v=await call<SSHHosts>('SSHHosts');if(live){setHosts(v.hosts);setIssue(v.issue||'')}}catch{if(live)setIssue('ssh_config_unreadable')}};void read();window.addEventListener('focus',read);return()=>{live=false;window.removeEventListener('focus',read)}},[call]);
 const update=(fields:Partial<SSHTarget>)=>onChange({...value,...fields});
 const chooseKey=async()=>{try{const key=await desktop<SSHIdentity>('PickSSHIdentity');if(key.id){update({identity:key.id});setKeyName(key.name);setIssue('')}}catch{setIssue('ssh_key_unavailable')}};
 const botPortField=<label>{t('settings.sshBotPort')}<NumericPortInput placeholder="9317" defaultValue={9317} value={value.port} disabled={disabled} onChange={port=>update({port})}/></label>;
 return <div className="remote-ssh-fields">
  <div className="remote-ssh-mode" role="group" aria-label={t('settings.sshDestination')}>
   <button type="button" className={!manual?'selected':''} aria-pressed={!manual} disabled={disabled} onClick={()=>onChange({host:'',port:value.port})}>{t('settings.sshExistingHost')}</button>
   <button type="button" className={manual?'selected':''} aria-pressed={manual} disabled={disabled} onClick={()=>onChange({host:'',port:value.port,sshPort:22,user:'',authentication:'password'})}>{t('settings.sshManual')}</button>
  </div>
  {manual?<>
   <div className="remote-endpoint-row"><label>{t('settings.remoteAddress')}<input required autoComplete="off" spellCheck={false} placeholder="192.168.1.10" value={value.host} disabled={disabled} onChange={e=>update({host:e.target.value})}/></label><label>{t('settings.sshLoginPort')}<NumericPortInput required value={value.sshPort} disabled={disabled} onChange={sshPort=>update({sshPort})}/></label></div>
   <label>{t('settings.sshUsername')}<input required autoComplete="off" spellCheck={false} placeholder="root" value={value.user||''} disabled={disabled} onChange={e=>update({user:e.target.value})}/></label>
   <div className="remote-auth-choice" role="group" aria-label={t('settings.sshAuthentication')}>
    {(['password','key'] as const).map(auth=><button key={auth} type="button" aria-pressed={value.authentication===auth} disabled={disabled} onClick={()=>update({authentication:auth,identity:undefined})}>{t(auth==='password'?'settings.sshPasswordLogin':'settings.sshKeyLogin')}</button>)}
   </div>
   {value.authentication==='key'&&<button type="button" className="remote-key-picker" disabled={disabled||isWebSurface()} onClick={()=>void chooseKey()}><FolderOpenIcon size={18}/><span>{keyName||(value.identity?t('settings.sshSelectedKey'):t(isWebSurface()?'settings.sshChooseKeyDesktop':'settings.sshChooseKey'))}</span></button>}
  </>:<div className="remote-endpoint-row"><label>{t('settings.sshHost')}<SettingsSelect aria-label={t('settings.sshHost')} value={value.host} disabled={disabled} placeholder={t('settings.sshHostPlaceholder')} searchable centered={false} searchLabel={t('settings.sshHostSearch')} onCreate={host=>update({host})} createLabel={name=>t('settings.sshUseAlias',{name})} onChange={e=>update({host:e.target.value})}>{hosts.map(host=><option key={host} value={host}>{host}</option>)}</SettingsSelect></label>{botPortField}</div>}
  {manual&&<div className="remote-bot-port">{botPortField}</div>}
  {issue&&(!manual||issue==='ssh_key_unavailable')&&<p role="alert" className="remote-dialog-error">{t(sshProblem(issue))}</p>}
 </div>
}
