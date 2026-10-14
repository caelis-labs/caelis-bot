import {SettingsDialog} from './SettingsDialog';
import {useI18n} from '../../i18n';

export type FullAccessScope = 'bot' | 'worker';

// This is a separate, explicit user action. Native code validates the Host
// principal, changes only this connection's requested ceiling, and reads it
// back before the caller may save a future execution preference.
export function FullAccessConfirmation({scope,busy,error,onCancel,onConfirm}:{scope:FullAccessScope;busy:boolean;error:string;onCancel:()=>void;onConfirm:()=>void}) {
 const {t,locale}=useI18n();
 const zh=locale==='zh-CN';
 const target=scope==='bot'?(zh?'下一段 Bot 对话':'future Bot conversations'):(zh?'新 Worker 任务':'new Worker tasks');
 return <SettingsDialog title={zh?'授权完全访问':'Authorize full access'} busy={busy} onClose={onCancel}>
  <p className="settings-note">{zh?`仅为${target}向当前 Caelis Host 请求完全访问授权；现有会话、任务、回执和另一项授权保持原样。`:`Request full access from the current Caelis Host for ${target} only. Existing sessions, tasks, receipts, and the other grant stay unchanged.`}</p>
  <p className="settings-note">{scope==='bot'?(zh?'新对话将关闭命令工作区沙箱，已获产品授权的执行跳过 Runtime 审查。账号、后台任务、Computer Use 和 macOS 权限仍分别授权。':'New conversations will run without the command workspace sandbox. Execution already authorized by the product skips Runtime review. Account, background work, Computer Use, and macOS permissions remain separate.'):(zh?'新任务将使用无工作区沙箱的权限。账号、工具、Computer Use 和 macOS 权限仍分别授权。':'New tasks will use permissions without a workspace sandbox. Account, tool, Computer Use, and macOS permissions remain separate.')}</p>
  {error&&<p role="alert" className="inline-error">{error}</p>}
  <div className="setup-end"><button disabled={busy} onClick={onCancel}>{t('common.cancel')}</button><button className="primary runtime-full-access" disabled={busy} onClick={onConfirm}>{busy?(zh?'正在核对 Host 授权…':'Checking Host authorization…'):(zh?'确认授权并选择完全访问':'Authorize and select full access')}</button></div>
 </SettingsDialog>;
}
