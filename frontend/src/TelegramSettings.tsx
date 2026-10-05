import { useEffect, useState } from 'react';
import { desktop } from './desktop';
import { useI18n } from './i18n';
import type { MessageKey } from './i18n/catalogs';
import { SettingGroup, SettingRow } from './SettingsUI';

type TelegramStatus={enabled:boolean;bot:string;owner:string;paired:boolean;candidate:string;pairURL:string;issue:string};
export function TelegramSettings(){
 const {t}=useI18n();
 const [status,setStatus]=useState<TelegramStatus|null>(null),[token,setToken]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');
 useEffect(()=>{let alive=true;const load=()=>{void desktop<TelegramStatus>('TelegramStatus').then(s=>{if(alive)setStatus(s);}).catch(()=>{if(alive)setError('unavailable');});};load();const timer=setInterval(load,1500);return()=>{alive=false;clearInterval(timer);};},[]);
 const action=async(method:string,...args:unknown[])=>{if(busy)return;setBusy(true);setError('');try{setStatus(await desktop<TelegramStatus>(method,...args));if(method==='ConnectTelegram')setToken('');}catch{try{const s=await desktop<TelegramStatus>('TelegramStatus');setStatus(s);setError(s.issue||'setup_failed');}catch{setError('unavailable');}}finally{setBusy(false);}};
 const open=async(pair:boolean)=>{try{await desktop('OpenTelegramSetup',pair);}catch{setError('open_failed');}};
 const issue=error||status?.issue||'';
 return <SettingGroup title={t('settings.telegramTitle')}>
  <SettingRow label={t('settings.telegramSubtitle')} description={t('settings.telegramDescription')}><span className="connection-status">{status?.enabled&&status.paired&&!issue?t('settings.telegramConnected'):t('settings.telegramNotConnected')}</span></SettingRow>
  {(!status?.enabled||!status.paired)&&<div className="telegram-setup">
   {!status?.enabled&&<>
    <details className="settings-disclosure"><summary>{t('settings.telegramCreateTitle')}</summary><ol><li>{t('settings.telegramCreate1')}</li><li>{t('settings.telegramCreate2')}</li><li>{t('settings.telegramCreate3')}</li></ol><p>{t('settings.telegramExisting')}</p><button type="button" onClick={()=>void open(false)}>{t('settings.telegramBotFather')}</button></details>
    <label htmlFor="telegram-token">{t('settings.telegramToken')}</label>
    <div className="telegram-token-row"><input id="telegram-token" type="password" autoComplete="off" spellCheck={false} value={token} maxLength={512} placeholder={t('settings.telegramTokenPlaceholder')} onChange={e=>setToken(e.target.value)}/><button type="button" disabled={busy||!token.trim()} onClick={()=>void action('ConnectTelegram',token,false)}>{t('settings.telegramConnect')}</button></div>
    <p className="settings-description">{t('settings.telegramTokenPrivacy')}</p>
    {status?.bot&&status.paired&&<button type="button" disabled={busy} onClick={()=>void action('ConnectTelegram','',false)}>{t('settings.telegramResume')}</button>}
   </>}
   {status?.enabled&&!status.paired&&<>
    <p>{t('settings.telegramPairStep',{bot:status.bot})}</p>
    <button type="button" disabled={busy} onClick={()=>void open(true)}>{t('settings.telegramOpenBot')}</button> <button type="button" disabled={busy} onClick={()=>void action('ConnectTelegram','',false)}>{t('settings.telegramNewPairLink')}</button>
    {!status.candidate?<p className="settings-description">{t('settings.telegramPairWaiting')}</p>:<div className="telegram-confirm"><p>{t('settings.telegramConfirmAccount',{account:status.candidate})}</p><button type="button" disabled={busy} onClick={()=>void action('ConfirmTelegram')}>{t('settings.telegramConfirm')}</button></div>}
   </>}
  </div>}
  {status?.enabled&&status.paired&&<SettingRow label={`@${status.bot}`} description={t('settings.telegramOwner',{account:status.owner})}><button type="button" onClick={()=>void open(true)}>{t('settings.telegramOpenBot')}</button></SettingRow>}
  {issue&&<div className="telegram-notice" role="status"><p>{t(`settings.telegramIssue_${issue}` as MessageKey)}</p>
   {(issue==='keychain'||issue==='invalid_token')&&status?.enabled&&<><label htmlFor="telegram-token-recovery">{t('settings.telegramToken')}</label><div className="telegram-token-row"><input id="telegram-token-recovery" type="password" autoComplete="off" spellCheck={false} value={token} maxLength={512} placeholder={t('settings.telegramTokenPlaceholder')} onChange={e=>setToken(e.target.value)}/><button type="button" disabled={busy||!token.trim()} onClick={()=>void action('ConnectTelegram',token,false)}>{t('settings.telegramConnect')}</button></div><p className="settings-description">{t('settings.telegramTokenPrivacy')}</p></>}
   {issue==='webhook'&&<button type="button" disabled={busy} onClick={()=>void action('ConnectTelegram',token,true)}>{t('settings.telegramTakeOver')}</button>}
   {(issue==='occupied'||issue==='blocked'||issue==='pairing_failed'||issue==='pairing_expired'||(issue==='network'&&status?.enabled))&&<button type="button" disabled={busy} onClick={()=>void action('ConnectTelegram','',false)}>{t('settings.telegramRetry')}</button>}
  </div>}
  {status?.bot&&<SettingRow label={t('settings.telegramManage')} description={t('settings.telegramStayOnline')}><div className="telegram-actions">{status.enabled&&<button type="button" disabled={busy} onClick={()=>void action('DisconnectTelegram')}>{t('settings.telegramPause')}</button>}<button type="button" disabled={busy} onClick={()=>void action('ForgetTelegram')}>{t('settings.telegramForget')}</button></div></SettingRow>}
 </SettingGroup>;
}
