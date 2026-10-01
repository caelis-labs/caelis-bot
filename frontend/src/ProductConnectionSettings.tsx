import {useEffect,useRef,useState} from 'react';
import {backend,desktop} from './desktop';
import {SettingGroup,SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
import type {ProductPairing,ProductConnectionState} from './backend/contract';

const empty:ProductPairing={mode:'local',label:'',ssh:'',helper:'',endpoint:'',authFile:'',nodeId:'',botId:''};

export function ProductConnectionSettings() {
 const {t}=useI18n();
 const [state,setState]=useState<ProductConnectionState|null>(null),[draft,setDraft]=useState<ProductPairing>(empty);
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const pending=useRef(false),alive=useRef(true),edited=useRef(false);
 useEffect(()=>{
  alive.current=true;
  const refresh=()=>{void backend<ProductConnectionState>('ProductConnection').then(next=>{if(alive.current){setState(next);if(!edited.current)setDraft(next.pairing);}}).catch(()=>{if(alive.current)setError(t('settings.productLoadFailed'));});};
  refresh();const timer=window.setInterval(refresh,2000);
  return()=>{alive.current=false;clearInterval(timer);};
 },[t]);
 const change=(key:keyof ProductPairing,value:string)=>{edited.current=true;setDraft({...draft,[key]:value});};
 const run=async(action:'save'|'reconnect'|'disconnect'|'restart')=>{
  if(pending.current||!state)return;
  pending.current=true;setBusy(true);setError('');
  try{
   if(action==='save'){const next=await backend<ProductConnectionState>('SaveProductPairing',draft,state.revision);if(alive.current){setState(next);setDraft(next.pairing);edited.current=false;}}
   else if(action==='restart')await desktop('RestartForRuntime');
   else await backend(action==='reconnect'?'ReconnectProduct':'DisconnectProduct');
  }catch{if(alive.current)setError(t('settings.productActionFailed'));}
  finally{pending.current=false;if(alive.current)setBusy(false);}
 };
 const field=(key:keyof ProductPairing,label:string,help?:string)=><SettingRow key={key} label={label} htmlFor={`product-${key}`} description={help}><input id={`product-${key}`} value={draft[key]} disabled={busy||!state} autoComplete="off" spellCheck={false} onChange={e=>change(key,e.target.value)}/></SettingRow>;
 return <SettingGroup title={t('settings.productConnection')}>
  <p className="settings-note">{t('settings.productConnectionHelp')}</p>
  <p className="settings-note" role="status">{state?.activeMode==='remote'?`${state.pairing.label} · ${t(state.state==='ready'?'settings.productConnected':state.state==='connecting'?'settings.productConnecting':'settings.productOffline')}`:t('settings.productLocal')}</p>
  {state?.activeMode==='remote'&&<>
   <p className="settings-note">{t(state.issue==='outcome_unknown'?'settings.productUnknownReceipt':'settings.productDetachHelp')}</p>
   <button disabled={busy} onClick={()=>void run(state.state==='ready'||state.state==='connecting'?'disconnect':'reconnect')}>{t(state.state==='ready'||state.state==='connecting'?'settings.productDisconnect':'settings.productReconnect')}</button>
  </>}
  <details className="worker-node-form"><summary>{t('settings.productConfigure')}</summary>
   <SettingRow label={t('settings.productMode')} htmlFor="product-mode"><select id="product-mode" value={draft.mode} disabled={busy||!state} onChange={e=>change('mode',e.target.value)}><option value="local">{t('settings.productModeLocal')}</option><option value="remote">{t('settings.productModeRemote')}</option></select></SettingRow>
   {draft.mode==='remote'&&<>
    <p className="settings-note">{t('settings.productTargetPreparation')}</p>
    {field('label',t('settings.workerNodeLabel'))}
    {field('ssh',t('settings.workerNodeSSH'),t('settings.workerNodeSSHHelp'))}
    <details className="worker-node-advanced"><summary>{t('settings.productAdvancedPairing')}</summary>
     {field('endpoint',t('settings.productEndpoint'),t('settings.productEndpointHelp'))}
     {field('authFile',t('settings.productAuthFile'),t('settings.productAuthFileHelp'))}
     {field('helper',t('settings.productHelper'))}
     {field('nodeId',t('settings.productNodePin'))}
     {field('botId',t('settings.productBotPin'))}
    </details>
   </>}
   <button disabled={busy||!state} onClick={()=>void run('save')}>{t('settings.productSave')}</button>
  </details>
  {state?.restartRequired&&<SettingRow label={t('settings.productRestartRequired')}><button disabled={busy} onClick={()=>void run('restart')}>{t('settings.productRestart')}</button></SettingRow>}
  {(error||busy)&&<p className={error?'inline-error':'settings-note'} role={error?'alert':'status'}>{error||t('settings.workerNodeWorking')}</p>}
 </SettingGroup>;
}
