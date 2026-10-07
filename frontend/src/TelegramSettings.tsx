import {useEffect,useRef,useState} from 'react';
import {flushSync} from 'react-dom';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import {telegramPhase,type TelegramStatus} from './telegram-state';

const issues=new Set(['invalid_token','network','occupied','webhook','keychain','storage','blocked','unavailable','setup_failed','open_failed','pairing_failed','pairing_expired','delivery_uncertain','file_delivery_uncertain','file_unavailable','rate_limited','telegram_error','sync_busy','approval_unavailable']);
function issueKey(issue:string):MessageKey {return `settings.telegramIssue_${issues.has(issue)?issue:'setup_failed'}` as MessageKey;}

export function TelegramSettings({onBack,active=true}:{onBack?:()=>void;active?:boolean}={}){
 const {t}=useI18n();
 const [status,setStatus]=useState<TelegramStatus|null>(null),[token,setToken]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [route,setRoute]=useState<'new'|'existing'>('new'),[confirmForget,setConfirmForget]=useState(false);
 const alive=useRef(false),revision=useRef(0),acting=useRef(false),tokenInput=useRef<HTMLInputElement|null>(null);
 const clearToken=()=>{if(tokenInput.current)tokenInput.current.value='';setToken('')};
 useEffect(()=>{if(!active){clearToken();setConfirmForget(false)}},[active]);
 useEffect(()=>{
  alive.current=true;
  const close=()=>flushSync(()=>{clearToken();setConfirmForget(false)});
  window.addEventListener('settings-close',close);
  const load=()=>{if(acting.current)return;const current=++revision.current;void desktop<TelegramStatus>('TelegramStatus').then(s=>{if(alive.current&&current===revision.current){setStatus(s);setError(previous=>previous==='unavailable'?'':previous)}}).catch(()=>{if(alive.current&&current===revision.current)setError(previous=>previous||'unavailable')});};
  load();const timer=window.setInterval(load,2000);
  return()=>{alive.current=false;revision.current++;clearInterval(timer);window.removeEventListener('settings-close',close)};
 },[]);
 const refresh=async()=>{const current=++revision.current;try{const s=await desktop<TelegramStatus>('TelegramStatus');if(alive.current&&current===revision.current){setStatus(s);setError('')}}catch{if(alive.current&&current===revision.current)setError('unavailable')}};
 const act=async(method:'ConnectTelegram'|'ConfirmTelegram'|'DisconnectTelegram'|'ForgetTelegram',...args:unknown[])=>{
  if(acting.current)return;acting.current=true;revision.current++;setBusy(true);setError('');
  if(method==='ConnectTelegram')clearToken();
  try{const s=await desktop<TelegramStatus>(method,...args);if(alive.current){setStatus(s);setConfirmForget(false)}}
  catch{if(alive.current){const current=++revision.current;try{const s=await desktop<TelegramStatus>('TelegramStatus');if(alive.current&&current===revision.current){setStatus(s);setError(s.issue||'setup_failed')}}catch{if(alive.current&&current===revision.current)setError('unavailable')}}}
  finally{acting.current=false;if(alive.current)setBusy(false)}
 };
 const open=async(pair:boolean)=>{try{await desktop('OpenTelegramSetup',pair);if(alive.current)setError('')}catch{if(alive.current)setError('open_failed')}};
 const issue=error||status?.issue||'';
 const phase=telegramPhase(status,busy,issue);
 const hasToken=token.trim().length>0;
 const tokenInvalid=issue==='invalid_token'&&!hasToken;
 const showToken=phase==='unconfigured'||issue==='invalid_token'||issue==='keychain'||issue==='webhook';
 return <section className="telegram-page" aria-labelledby="telegram-title">
  {onBack&&<button type="button" className="text-action telegram-back" onClick={()=>{clearToken();onBack()}}>‹ {t('settings.chatConnections')}</button>}
  <div className="telegram-heading"><div><h1 id="telegram-title">{t('settings.telegramTitle')}</h1><p>{t('settings.telegramDescription')}</p></div><span className={`telegram-state telegram-state-${phase}`} role="status">{t(`settings.telegramState_${phase}` as MessageKey)}</span></div>
  {phase==='loading'&&<p role="status">{t('common.loading')}</p>}
  {phase==='connecting'&&<p role="status" className="telegram-feedback">{t('settings.telegramConnectingHelp')}</p>}
  {phase==='unconfigured'&&<div className="telegram-step">
   <h3>{t('settings.telegramChooseBot')}</h3>
   <div className="telegram-route" role="group" aria-label={t('settings.telegramChooseBot')}><button type="button" aria-pressed={route==='new'} onClick={()=>setRoute('new')}>{t('settings.telegramNewBot')}</button><button type="button" aria-pressed={route==='existing'} onClick={()=>setRoute('existing')}>{t('settings.telegramExistingBot')}</button></div>
   {route==='new'?<p>{t('settings.telegramCreateSimple')}</p>:<p>{t('settings.telegramExisting')}</p>}
   <button type="button" className="text-action link-action" onClick={()=>void open(false)}>{t('settings.telegramBotFather')}</button>
  </div>}
  {showToken&&<div className="telegram-step"><h3>{t('settings.telegramTokenStep')}</h3>
   <label htmlFor="telegram-token">{t('settings.telegramToken')}</label>
   <input id="telegram-token" ref={tokenInput} type="password" autoComplete="off" spellCheck={false} value={token} maxLength={512} placeholder={t('settings.telegramTokenPlaceholder')} disabled={busy} aria-invalid={tokenInvalid} aria-describedby={tokenInvalid?'telegram-token-error':undefined} onChange={e=>{setToken(e.target.value);if(error==='invalid_token')setError('')}}/>
   {tokenInvalid&&<p id="telegram-token-error" className="inline-error" role="alert">{t('settings.telegramIssue_invalid_token')}</p>}
   <p className="settings-description">{t('settings.telegramTokenPrivacy')}</p>
   <button type="button" className="primary" disabled={busy||!hasToken} onClick={()=>void act('ConnectTelegram',token,issue==='webhook')}>{t(issue==='webhook'?'settings.telegramTakeOver':'settings.telegramConnect')}</button>
  </div>}
  {phase==='pairing'&&<div className="telegram-step"><h3>{t('settings.telegramPairHeading')}</h3><p>{t('settings.telegramPairStep',{bot:status?.bot??''})}</p><button type="button" className="primary" disabled={busy} onClick={()=>void open(true)}>{t('settings.telegramOpenBot')}</button><p className="settings-description">{t('settings.telegramPairWaiting')}</p><button type="button" className="text-action" disabled={busy} onClick={()=>void act('ConnectTelegram','',false)}>{t('settings.telegramNewPairLink')}</button></div>}
  {phase==='candidate'&&<div className="telegram-step"><h3>{t('settings.telegramConfirmHeading')}</h3><p>{t('settings.telegramConfirmAccount',{account:status?.candidate??''})}</p><button type="button" className="primary" disabled={busy} onClick={()=>void act('ConfirmTelegram')}>{t('settings.telegramConfirm')}</button><p className="settings-description">{t('settings.telegramConfirmHelp')}</p></div>}
  {phase==='connected'&&<div className="telegram-step"><h3>@{status?.bot}</h3><p>{t('settings.telegramOwner',{account:status?.owner??''})}</p><button type="button" className="primary" disabled={busy} onClick={()=>void open(true)}>{t('settings.telegramOpenBot')}</button><p className="settings-description">{t('settings.telegramStayOnline')}</p></div>}
  {phase==='paused'&&<div className="telegram-step"><h3>@{status?.bot}</h3><p>{t('settings.telegramPausedHelp')}</p><button type="button" className="primary" disabled={busy} onClick={()=>void act('ConnectTelegram','',false)}>{t('settings.telegramResume')}</button></div>}
  {issue&&issue!=='invalid_token'&&<div className="telegram-notice" role="alert"><p>{t(issueKey(issue))}</p>
   {(phase==='error'||phase==='reconnecting')&&!showToken&&<button type="button" className="primary" disabled={busy} onClick={()=>phase==='reconnecting'||issue==='unavailable'?void refresh():void act('ConnectTelegram','',false)}>{t(phase==='reconnecting'||issue==='unavailable'?'settings.telegramCheckAgain':'settings.telegramRetry')}</button>}
   {issue==='webhook'&&<p className="settings-description">{t('settings.telegramWebhookChoice')}</p>}
  </div>}
  {status?.bot&&phase!=='connecting'&&<div className="telegram-manage">
   {status.enabled&&<button type="button" disabled={busy} onClick={()=>void act('DisconnectTelegram')}>{t('settings.telegramPause')}</button>}
   {!confirmForget?<button type="button" className="text-action" disabled={busy} onClick={()=>setConfirmForget(true)}>{t('settings.telegramForget')}</button>:<div className="telegram-forget" role="group" aria-label={t('settings.telegramForgetTitle')} onKeyDown={event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();setConfirmForget(false)}}}><p>{t('settings.telegramForgetHelp')}</p><button type="button" disabled={busy} onClick={()=>void act('ForgetTelegram')}>{t('settings.telegramForgetConfirm')}</button><button type="button" autoFocus disabled={busy} onClick={()=>setConfirmForget(false)}>{t('settings.telegramCancel')}</button></div>}
  </div>}
 </section>;
}
