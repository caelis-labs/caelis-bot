import {isWebSurface} from '../../transport';
import type {SetupState} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingGroup,SettingRow} from '../../SettingsUI';
import {SettingsChevron,CheckIcon} from '../../SettingsIcons';
export function BuiltinRuntimePanel({state,busy,checking,count,onAction,onConnect,onTeam,onDetect}:{state:SetupState;busy:boolean;checking:boolean;count?:number;onAction:(action:string)=>void;onConnect:()=>void;onTeam:()=>void;onDetect:()=>void}){
 const {t}=useI18n(),installed=state.installation.installed,connected=state.state==='ready',available=['ready','models'].includes(state.state);
 return <div className="codex-runtime-panel">
  <section className="codex-runtime-status"><div className="codex-runtime-symbol" aria-hidden="true">{available?<CheckIcon size={24}/>:<span>↓</span>}</div><div className="codex-runtime-copy"><h3>{t(!installed?'settings.builtinInstallTitle':connected?'settings.codexReadyTitle':available?'settings.builtinConnectTitle':'settings.builtinPrepareTitle')}</h3><p>{isWebSurface()&&!installed?'随包程序不可用，请在服务端重新解压完整发行包。':t(!installed?'settings.builtinInstallHint':connected?'settings.builtinReadyHint':available?'settings.builtinConnectHint':'settings.builtinPrepareHint')}</p></div><div className="codex-runtime-actions">
   {isWebSurface()&&!installed?<button className="primary" disabled={busy||checking} onClick={onDetect}>{t('runtime.recheck')}</button>:!installed?<button className="primary" disabled={busy||checking} onClick={()=>onAction('install')}>{t('runtime.installRuntime',{name:'built-in'})}</button>:!available?<button className="primary" disabled={busy||checking} onClick={()=>onAction(state.state==='service'?'start':'apply-update')}>{t('runtime.prepareConnection')}</button>:!connected?<button className="primary" disabled={busy} onClick={onConnect}>{t('connections.addConnection')}</button>:null}
  </div></section>
  {available&&<SettingGroup><button className="connection-navigation-row" onClick={onConnect}><span><strong>{t('runtime.modelConnections')}</strong>{count!==undefined&&<small>{t('runtime.modelCount',{count})}</small>}</span><SettingsChevron/></button></SettingGroup>}

  {available&&<SettingGroup title={t('settings.connectionAdvanced')}><button className="connection-navigation-row" onClick={onTeam}><strong>{t('runtime.teamSettings')}</strong><SettingsChevron/></button></SettingGroup>}
  {!available&&<button className="text-action" disabled={busy||checking} onClick={onDetect}>{t('runtime.recheck')}</button>}
  {installed&&!isWebSurface()&&<SettingGroup><SettingRow label={t('settings.codexVersion')} description={<>{state.installation.version}{state.installation.updateState==='current'&&<span> · {t('settings.codexUpToDate')}</span>}</>}>{state.installation.bundled?<span className="settings-note">{t('runtime.bundledUpdateHint')}</span>:<button disabled={busy||checking} onClick={()=>onAction('check-update')}>{t('runtime.checkUpdate')}</button>}</SettingRow>
   {!state.installation.bundled&&state.installation.updateState==='available'&&<SettingRow label={t('runtime.newVersionAvailable',{version:state.installation.latestVersion})}><button disabled={busy} onClick={()=>onAction('update')}>{t('runtime.update')}</button></SettingRow>}
   {state.serviceUpdateAvailable&&<SettingRow label={t('runtime.newVersionNotActivated')}><button disabled={busy} onClick={()=>onAction('apply-update')}>{t('runtime.applyInstalledVersion')}</button></SettingRow>}
  </SettingGroup>}
 </div>;
}
