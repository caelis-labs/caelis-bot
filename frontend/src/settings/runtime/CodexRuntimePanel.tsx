import {isWebSurface} from '../../transport';
import type {SetupState} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingGroup,SettingRow} from '../../SettingsUI';
import {CheckIcon} from '../../SettingsIcons';
import {setupStatusKey} from './setup-status';

// Installation, a working handshake, and authentication are separate states.
export function CodexRuntimePanel({state,busy,checking,onAction,onDetect}:{state:SetupState;busy:boolean;checking:boolean;onAction:(action:string)=>void;onDetect:()=>void}) {
 const {t}=useI18n();
 const installed=state.installation.installed,ready=state.state==='ready';
 const login=installed&&(state.state==='login'||state.loginPending);
 const available=state.installation.updateState==='available';
 if(isWebSurface()&&!ready)return <div className="codex-runtime-panel"><p className="settings-note">{!installed?'请在运行 CaelisBot 的服务端主机安装 Codex，再重新检测。':login?'在服务端主机运行 codex login --device-auth，完成后重新检测。':t(setupStatusKey(state))}</p><button disabled={busy||checking} onClick={onDetect}>{t('runtime.recheck')}</button></div>;
 return <div className="codex-runtime-panel">
  <section className={`codex-runtime-status${ready?' is-ready':''}`}>
   <div className="codex-runtime-symbol" aria-hidden="true">{ready?<CheckIcon size={24}/>:<span>⌘</span>}</div>
   <div className="codex-runtime-copy"><h3>{t(!installed?'settings.codexInstallTitle':ready?'settings.codexReadyTitle':state.loginPending?'runtime.waitingBrowserLogin':login?'settings.codexLoginTitle':'settings.codexUnavailableTitle')}</h3>
   <p>{t(!installed?'settings.codexInstallHint':ready?'settings.codexReadyHint':state.loginPending?'settings.codexLoginPendingHint':login?'settings.codexLoginHint':'settings.codexUnavailableHint')}</p></div>
   <div className="codex-runtime-actions">
    {!installed?<button className="primary" disabled={busy||checking} onClick={()=>onAction('install')}>{t('runtime.installRuntime',{name:'Codex'})}</button>:login?<><button className="primary" disabled={busy||checking} onClick={()=>onAction('login')}>{t(state.loginPending?'runtime.openLoginPage':'runtime.loginWithChatGPT')}</button>{state.loginPending&&<button disabled={busy} onClick={()=>onAction('cancel-login')}>{t('common.cancel')}</button>}</>:!ready?<button disabled={busy||checking} onClick={onDetect}>{t('runtime.recheck')}</button>:null}
   </div>
  </section>
  {installed&&!ready&&!login&&<p className="inline-error" role="alert">{t(setupStatusKey(state))}</p>}
  {installed&&<SettingGroup><SettingRow label={t('settings.codexVersion')} description={<>{state.installation.version}{state.installation.updateState==='current'&&<span className="codex-version-status" role="status"> · {t('settings.codexUpToDate')}</span>}</>}>
   <button disabled={busy||checking} onClick={()=>onAction('check-update')}>{t(checking?'settings.codexCheckingUpdate':'runtime.checkUpdate')}</button>
  </SettingRow>{available&&!isWebSurface()&&<SettingRow label={t('runtime.newVersionAvailable',{version:state.installation.latestVersion})}><button className="primary" disabled={busy||checking} onClick={()=>onAction('update')}>{t('runtime.update')}</button></SettingRow>}
</SettingGroup>}
  {!installed&&<button className="text-action" disabled={busy||checking} onClick={onDetect}>{t('settings.codexDetectExisting')}</button>}
 </div>;
}
