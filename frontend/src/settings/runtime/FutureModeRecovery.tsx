import {useEffect, useState} from 'react';
import {backend} from '../../desktop';
import {useI18n} from '../../i18n';

// The target Runtime's saved choice is separate from the active conversation.
// This control changes only future Caelis bindings and requires a native readback.
export function FutureModeRecovery({active=true,call=backend,onRecovered}:{active?:boolean;call?:typeof backend;onRecovered?:()=>void}) {
 const {locale}=useI18n();
 const [mode,setMode]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(false);
 useEffect(()=>{if(!active)return;let live=true;setMode('');setError('');void call<string>('BotExecutionModeForRuntime','caelis').then(value=>{if(live)setMode(value);}).catch(e=>{if(live)setError(e instanceof Error?e.message:String(e));});return()=>{live=false;};},[active,call]);
 if(!active)return null;
 return <div className="runtime-worker-recovery">
  <p className="settings-note">{locale==='zh-CN'?'Caelis 下一段 Bot 对话的偏好：':'Caelis preference for the next Bot conversation: '}{mode==='auto'?(locale==='zh-CN'?'自动审查':'Automatic review'):mode==='full-access'?(locale==='zh-CN'?'完全访问':'Full access'):(locale==='zh-CN'?'正在读取':'Reading')}</p>
  {mode==='full-access'&&<><p className="settings-note">{locale==='zh-CN'?'若当前 Host 未完成完全访问授权，可明确改为自动审查后重试连接。已存在会话权限不会改变。':'If this Host has no full-access grant, choose automatic review and retry. Existing session permissions stay unchanged.'}</p><button disabled={busy} onClick={async()=>{setBusy(true);setError('');try{await call('SaveBotExecutionModeForRuntime','caelis','auto');const confirmed=await call<string>('BotExecutionModeForRuntime','caelis');if(confirmed!=='auto')throw new Error(locale==='zh-CN'?'模式回读与保存不一致':'Mode readback did not match the save');setMode(confirmed);onRecovered?.();}catch(e){setError(e instanceof Error?e.message:String(e));}finally{setBusy(false);}}}>{locale==='zh-CN'?'改为自动审查并重试':'Use automatic review and retry'}</button></>}
  {error&&<p className="inline-error" role="alert">{error}</p>}
 </div>;
}
