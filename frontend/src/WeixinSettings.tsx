import {useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';

export type WeixinStatus={enabled:boolean;paired:boolean;phase:string;owner:string;bot:string;issue:string;qrImage:string;expiresAt:number};
const issueKeys=new Set(['network','pairing_failed','storage','credential','auth_expired','session_cooldown','remote_error','backlog','input_uncertain','delivery_retrying','delivery_uncertain','send_rejected','untrusted_api_host']);
const phases=new Set(['unconfigured','pairing','scanned','verify','blocked','expired','bound_elsewhere','confirm','connected','paused','attention']);
function stateKey(phase:string):MessageKey{return `settings.weixinState_${phases.has(phase)?phase:'attention'}` as MessageKey}
function issueKey(issue:string):MessageKey{return `settings.weixinIssue_${issueKeys.has(issue)?issue:'pairing_failed'}` as MessageKey}

export function WeixinSettings({onBack,active=true}:{onBack?:()=>void;active?:boolean}){
 const {t}=useI18n();
 const [status,setStatus]=useState<WeixinStatus|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[code,setCode]=useState(''),[forget,setForget]=useState(false);
 const acting=useRef(false),lastAuto=useRef(''),autoSuppressed=useRef(false),autoQRCount=useRef(0);
 useEffect(()=>{if(!active)return;let live=true;const read=()=>{if(acting.current)return;void desktop<WeixinStatus>('WeixinStatus').then(s=>{if(live)setStatus(s)}).catch(()=>{if(live)setError('unavailable')})};read();const timer=window.setInterval(read,1500);return()=>{live=false;clearInterval(timer)}},[active]);
 useEffect(()=>{if(!active){setCode('');setForget(false);autoSuppressed.current=false;lastAuto.current='';autoQRCount.current=0}},[active]);
 const act=async(method:string,...args:unknown[])=>{if(acting.current)return;acting.current=true;setBusy(true);setError('');try{setStatus(await desktop<WeixinStatus>(method,...args));if(method==='ForgetWeixin')autoSuppressed.current=true;setCode('');setForget(false)}catch{try{setStatus(await desktop<WeixinStatus>('WeixinStatus'))}catch{setError('unavailable')}}finally{acting.current=false;setBusy(false)}};
 useEffect(()=>{if(!active||!status||status.paired||autoSuppressed.current||!['unconfigured','expired'].includes(status.phase)||autoQRCount.current>=3)return;const key=`${status.phase}:${status.expiresAt}`;if(lastAuto.current===key)return;lastAuto.current=key;autoQRCount.current++;void act('StartWeixinPairing')},[active,status?.phase,status?.paired,status?.expiresAt]);
 const phase=status?.phase??'unconfigured';
 const issue=error||status?.issue||'';
 return <section className="telegram-page" aria-labelledby="weixin-title">
  {onBack&&<button className="text-action telegram-back" type="button" onClick={onBack}>‹ {t('settings.chatConnections')}</button>}
  <div className="telegram-heading"><div><h1 id="weixin-title">{t('settings.weixinTitle')}</h1><p>{t('settings.weixinDescription')}</p></div><span className={`telegram-state telegram-state-${phase==='connected'?'connected':phase==='paused'?'paused':phase==='unconfigured'?'unconfigured':'pairing'}`} role="status">{t(stateKey(phase))}</span></div>
  {phase==='unconfigured'&&<div className="telegram-step"><p>{t('settings.weixinStartHelp')}</p><button className="primary" disabled={busy} onClick={()=>void act('StartWeixinPairing')}>{t('settings.weixinStart')}</button></div>}
  {['pairing','scanned','verify'].includes(phase)&&<div className="telegram-step"><h3>{t('settings.weixinScanHeading')}</h3>{status?.qrImage&&<img className="weixin-qr" src={status.qrImage} width="280" height="280" alt={t('settings.weixinQRAlt')}/> }<p>{t(phase==='scanned'?'settings.weixinScannedHelp':'settings.weixinScanHelp')}</p>{phase==='verify'&&<><label htmlFor="weixin-verify">{t('settings.weixinVerifyLabel')}</label><input id="weixin-verify" type="text" value={code} autoComplete="off" maxLength={32} onChange={event=>setCode(event.target.value)}/><button className="primary" disabled={busy||code.trim().length<3} onClick={()=>void act('VerifyWeixinPairing',code)}>{t('settings.weixinVerify')}</button></>}</div>}
  {phase==='confirm'&&<div className="telegram-step"><h3>{t('settings.weixinConfirmHeading')}</h3><p>{t('settings.weixinConfirmHelp',{account:status?.owner??''})}</p><button className="primary" disabled={busy} onClick={()=>void act('ConfirmWeixinPairing')}>{t('settings.weixinConfirm')}</button></div>}
  {['expired','blocked','bound_elsewhere'].includes(phase)&&<div className="telegram-step"><p>{t(`settings.weixinHelp_${phase}` as MessageKey)}</p><button className="primary" disabled={busy} onClick={()=>void act('StartWeixinPairing')}>{t('settings.weixinNewQR')}</button></div>}
  {phase==='connected'&&<div className="telegram-step"><h3>{t('settings.weixinConnected')}</h3><p>{t('settings.weixinConnectedHelp')}</p><p>{t('settings.weixinOwner',{account:status?.owner??''})}</p></div>}
  {phase==='paused'&&<div className="telegram-step"><p>{t('settings.weixinPausedHelp')}</p><button className="primary" disabled={busy} onClick={()=>void act('ResumeWeixin')}>{t('settings.weixinResume')}</button></div>}
  {issue&&<div className="telegram-notice" role="alert"><p>{issue==='unavailable'?t('settings.weixinIssue_pairing_failed'):t(issueKey(issue))}</p>{!status?.paired&&!status?.qrImage&&<button type="button" disabled={busy} onClick={()=>void act('StartWeixinPairing')}>{t('settings.weixinNewQR')}</button>}</div>}
  {status?.paired&&<div className="telegram-manage">{status.enabled&&<button type="button" disabled={busy} onClick={()=>void act('PauseWeixin')}>{t('settings.weixinPause')}</button>}{!forget?<button type="button" className="text-action" disabled={busy} onClick={()=>setForget(true)}>{t('settings.weixinForget')}</button>:<div className="telegram-forget"><p>{t('settings.weixinForgetHelp')}</p><button type="button" disabled={busy} onClick={()=>void act('ForgetWeixin')}>{t('settings.weixinForgetConfirm')}</button><button type="button" disabled={busy} onClick={()=>setForget(false)}>{t('settings.telegramCancel')}</button></div>}</div>}
 </section>;
}
