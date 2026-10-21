import {useEffect,useState} from 'react';
import type {SetupRequest,SetupState} from '../../backend/contract';
import {desktop,backend} from '../../desktop';
import {useI18n} from '../../i18n';
import {SetupButton} from '../../SetupLoading';
import {DeviceCode} from './DeviceCode';
import type {MessageKey} from '../../i18n/catalogs';
import {isWebSurface} from '../../transport';
import './codex-login.css';

// Shared App/Web ceremony. Authentication and its lifetime belong to Codex on
// the service host. This component only reads, copies and opens the user challenge.
export function CodexLogin({state,busy=false,error=false,onAction,host=desktop,open=backend}:{state:SetupState;busy?:boolean;error?:boolean;onAction:(action:string,fields?:Partial<SetupRequest>)=>unknown;host?:typeof desktop;open?:typeof backend}) {
 const {t}=useI18n();
 const [keyForm,setKeyForm]=useState(false),[key,setKey]=useState(''),[localError,setLocalError]=useState<MessageKey|''>('');
 const login=state.login,status=login?.state||'',pending=state.loginPending;
 useEffect(()=>{setLocalError('');setKey('');if(pending)setKeyForm(false)},[login?.id,pending]);
 const perform=(action:string,fields?:Partial<SetupRequest>)=>{setLocalError('');void Promise.resolve(onAction(action,fields)).catch(()=>setLocalError('runtime.deviceActionFailed'))};
 const launch=()=>{if(login?.verificationUrl!=='https://auth.openai.com/codex/device')return;void open('OpenMessageLink',login.verificationUrl).catch(()=>setLocalError('runtime.deviceOpenFailed'))};
 const message:Record<string,MessageKey>={unsupported:'runtime.deviceUnsupported',unavailable:'runtime.deviceUnavailable',failed:'runtime.deviceFailed',expired:'runtime.deviceExpired',cancelled:'runtime.deviceCancelled',interrupted:'runtime.deviceInterrupted',unknown:'runtime.deviceUnknown','other-session':'runtime.deviceOtherSession'};
 return <div className="codex-login">
  {pending&&status==='pending'&&login?.userCode?<>
   <p className="settings-note">{t('runtime.deviceEnterCode')}</p>
   <DeviceCode key={login.id} code={login.userCode} host={host}/>
   <button type="button" className="primary" onClick={launch}>{t('runtime.deviceOpenVerification')}</button>
   <a className="codex-verification-url" href={login.verificationUrl} target="_blank" rel="noopener noreferrer">auth.openai.com/codex/device</a>
   <p className="settings-note" role="status">{t('runtime.deviceWaiting')}</p>
  </>:!pending&&!keyForm?<>
   <p className="settings-note">{t(isWebSurface()?'runtime.deviceStartWeb':'runtime.deviceStart')}</p>
   <SetupButton className="primary" type="button" busy={busy} disabled={busy} onClick={()=>perform('login')}>{t('runtime.loginWithChatGPT')}</SetupButton>
  </>:null}
  {message[status]&&<p className="settings-note" role="status">{t(message[status])}</p>}
  {pending&&login?.id&&<button type="button" className="text-action" disabled={busy} onClick={()=>perform('cancel-login',{loginId:login.id})}>{t('common.cancel')}</button>}
  {!pending&&!keyForm&&<button type="button" className="text-action" disabled={busy} onClick={()=>setKeyForm(true)}>{t('runtime.useOpenAIApiKey')}</button>}
  {!pending&&keyForm&&<form className="codex-key-form" data-settings-dirty={!!key||busy} onSubmit={event=>{event.preventDefault();const secret=key;setKey('');perform('api-key',{apiKey:secret})}}>
   <label>API Key<input type="password" required autoComplete="off" spellCheck={false} value={key} disabled={busy} onChange={event=>setKey(event.target.value)}/></label>
   <p className="settings-note">{t('runtime.apiKeySavedByCodex')}</p>
   <SetupButton className="primary" busy={busy} disabled={busy||!key.trim()}>{t('runtime.connect')}</SetupButton>
   <button type="button" className="text-action" disabled={busy} onClick={()=>{setKey('');setKeyForm(false)}}>{t('settings.setupBack')}</button>
  </form>}
  {(localError||error)&&<p className="settings-note" role="status">{t(localError||'runtime.deviceActionFailed')}</p>}
 </div>;
}
