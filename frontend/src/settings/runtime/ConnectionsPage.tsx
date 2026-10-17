import {ProviderLogo} from './OnboardingProviders';
import {createPortal} from 'react-dom';
import {SettingsToolbarContext} from '../SettingsToolbar';
import {useContext,useEffect,useMemo,useRef,useState} from 'react';
import {backend} from '../../desktop';
import {useI18n} from '../../i18n';
import type {ModelOption,RuntimeSettings,RuntimeConfiguration,SetupState,SetupRequest,WorkExecutionSettings} from '../../backend/contract';
import {SettingGroup,SettingRow} from '../../SettingsUI';
import {SettingsChevron} from '../../SettingsIcons';
import {BuiltinRuntimePanel} from './BuiltinRuntimePanel';
import {CodexRuntimePanel} from './CodexRuntimePanel';
import {ModelPicker} from './ModelPicker';
import {SettingsPage} from './SettingsPage';
import {SettingsDialog} from './SettingsDialog';
import {modelUseLabel} from './state';
import {ConnectionWizard} from './ConnectionWizard';
import {TeamSettings} from './TeamSettings';
import {createRuntimeSettingsClient,ConfigurationError} from './client';
import {setupStatusKey} from './setup-status';
import {loadConnections,encodeModel as encode,followConversationModel,workerPickerValue} from './connections-loader';
import type {ConnectionKind,ConnectChoice} from './types';

type ID='caelis'|'codex';
type Route={page:'home'}|{page:'runtime'|'providers'|'team';id:ID}|{page:'connect';id:ID;kind:ConnectionKind;choice:string}|{page:'provider';id:ID;group:string};
type RuntimeView={profile:RuntimeSettings;state:SetupState;models:ModelOption[]};
const providerName=(id:string,label:string)=>({codex:'Codex',grok:'Grok',deepseek:'DeepSeek',xiaomi:'Xiaomi MiMo',magpie:'Magpie',ollama:'Ollama',custom:'Custom'}[id]||label||id);
const decode=(value:string):[ID,string]=>JSON.parse(value);
const emptyModel={model:'',effort:'',serviceTier:''};
export function ConnectionsPage({active=true,refreshKey=0}:{active?:boolean;refreshKey?:number}){
 const {t}=useI18n();
 const name=(id:string)=>id==='caelis'?t('runtime.builtinOnboardingTitle'):'Codex';
 const toolbar=useContext(SettingsToolbarContext);
 const [route,setRoute]=useState<Route>({page:'home'}),[views,setViews]=useState<Partial<Record<ID,RuntimeView>>>({}),[failures,setFailures]=useState<Partial<Record<ID,string>>>({});
 const [bot,setBot]=useState<WorkExecutionSettings>(emptyModel),[worker,setWorker]=useState<WorkExecutionSettings>(emptyModel),[botID,setBotID]=useState(''),[workerID,setWorkerID]=useState('');
 const [config,setConfig]=useState<RuntimeConfiguration|null>(null),[error,setError]=useState(''),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false);
 const [selectionLoaded,setSelectionLoaded]=useState(false),[selectionFailed,setSelectionFailed]=useState(false);
 const lastRefresh=useRef(0),refreshing=useRef<Promise<void>|null>(null);
 const [editing,setEditing]=useState<'conversation'|'work'|null>(null),[providerList,setProviderList]=useState<Array<ConnectChoice&{kind:ConnectionKind}>>([]),[providerError,setProviderError]=useState('');
 const [removing,setRemoving]=useState<{id:string;name:string;action:string}|null>(null),[removeError,setRemoveError]=useState('');
 const [notice,setNotice]=useState(''),[operation,setOperation]=useState(''),[operationError,setOperationError]=useState('');
 const generation=useRef(0),action=useRef(false),root=useRef<HTMLElement>(null),anchor=useRef<HTMLElement|null>(null);
 const id=route.page==='home'?'caelis':route.id,view=views[id];
 const client=useMemo(()=>createRuntimeSettingsClient(backend,view?.profile??{runtime:id,cliPath:'',caelisStore:''}),[view?.profile,id]);
 const textError=(e:unknown)=>e instanceof Error?e.message:t('runtime.actionFailed');
 const go=(next:Route)=>{if(action.current)return;setError('');setNotice('');setRoute(next);};
 const back=()=>go(route.page==='runtime'?{page:'home'}:route.page==='connect'||route.page==='provider'?{page:'providers',id}:{page:'runtime',id});
 const refresh=async(force=true)=>{
  if(refreshing.current)return refreshing.current;
  const inspect=force||Date.now()-lastRefresh.current>30000;
  const seq=++generation.current;setLoading(true);
  const pending=loadConnections(backend,current=>{
  if(seq!==generation.current)return;
  if(current[0].status==='fulfilled')setBotID(current[0].value.hasRuntimeChoice?current[0].value.active:'');
  if(current[1].status==='fulfilled')setBot(current[1].value);
  if(current[2].status==='fulfilled'){setWorkerID(current[2].value.runtime);setWorker(current[2].value.work);}
  setSelectionLoaded(true);setSelectionFailed(current.some(r=>r.status==='rejected'));
  },(runtime,result)=>{
   if(seq!==generation.current)return;
   if(result instanceof Error){setFailures(v=>({...v,[runtime]:textError(result)}));setViews(v=>({...v,[runtime]:undefined}));}
   else{setViews(v=>({...v,[runtime]:result}));setFailures(v=>({...v,[runtime]:undefined}));}
  },inspect).finally(()=>{if(seq===generation.current){setLoading(false);if(inspect)lastRefresh.current=Date.now();}refreshing.current=null;});
  refreshing.current=pending;return pending;
 };
 useEffect(()=>{let live=true;queueMicrotask(()=>{if(live&&active)void refresh(false);});return()=>{live=false}},[active,refreshKey]);
 useEffect(()=>{root.current?.closest('.settings-content')?.scrollTo(0,0)},[route.page]);
 useEffect(()=>{
  if(route.page==='home')return;
  let valid=true;setConfig(null);
  if(id==='caelis'&&view&&['ready','models'].includes(view.state.state))
   void backend<RuntimeConfiguration>('ProfileConfiguration',id).then(v=>{if(valid)setConfig(v)}).catch(e=>{if(valid)setError(textError(e))});
  return()=>{valid=false};
 },[route.page,id,view]);
 useEffect(()=>{if(route.page!=='providers')return;let valid=true;setProviderList([]);setProviderError('');void Promise.allSettled([client.catalog('account'),client.catalog('api-key')]).then(results=>{if(!valid)return;const list:Array<ConnectChoice&{kind:ConnectionKind}>=[];const errors:string[]=[];results.forEach((r,i)=>{if(r.status==='fulfilled'){if(r.value.unavailable)errors.push(r.value.unavailable);r.value.choices.forEach(c=>list.push({...c,kind:i===0?'account':'api-key'}));}else errors.push(textError(r.reason));});setProviderList(list);setProviderError(errors.join(' '));});return()=>{valid=false}},[route.page,client]);
 const run=async(fn:()=>Promise<void>)=>{if(action.current)return;action.current=true;setBusy(true);setError('');setNotice('');try{await fn()}catch(e){setError(textError(e))}finally{action.current=false;setBusy(false)}};
 const apply=async(act:string)=>run(async()=>{
  if(!view)return;
  setOperation(act);setOperationError('');
  try{
   const state=await backend<SetupState>('ApplySetup',{settings:view.profile,action:act,provider:'',model:'',baseUrl:'',apiKey:''} satisfies SetupRequest);
   setViews(v=>({...v,[id]:{...view,state}}));
   if(act!=='check-update')await refresh();
   setOperation('');
  }catch(e){
   if(['install','update'].includes(act)){setOperationError(textError(e));}else{setOperation('');throw e;}
  }
 });
 useEffect(()=>{if(!active||route.page!=='runtime'||!view?.state.loginPending)return;const timer=setInterval(()=>{if(!action.current)void refresh()},2500);return()=>clearInterval(timer)},[active,route.page,view?.state.loginPending]);
 const models=Object.entries(views).flatMap(([runtime,v])=>v?.state.state==='ready'?v.models.filter(m=>m.model.trim()).map(m=>({...m,name:m.name||m.model,model:encode(runtime,m.model),description:`${name(runtime)}${m.description?' · '+m.description:''}`})):[]);
 const followModel=followConversationModel;
 const pickerModels:ModelOption[]=editing==='work'?[{model:followModel,name:t('runtime.followConversationModel'),description:'',default:false,defaultEffort:'',efforts:[],serviceTiers:[],imageInput:null},...models]:models;
 const modelValue=(scope:'conversation'|'work')=>{if(scope==='work')return workerPickerValue(workerID,worker,views);const effective=bot.model||views[botID as ID]?.state.selectedModel;return {...bot,model:botID&&effective?encode(botID,effective):''}};
 const summary=(scope:'conversation'|'work')=>{const v=modelValue(scope),found=models.find(m=>m.model===v.model),runtime=scope==='conversation'?botID:workerID,saved=scope==='conversation'?bot:worker;return <button className="runtime-model-summary" onClick={event=>{anchor.current=event.currentTarget;setEditing(scope)}} disabled={loading||selectionFailed} aria-busy={!selectionLoaded}><span><strong>{!selectionLoaded?t('common.loading'):selectionFailed?t('runtime.loadRuntimeFailed'):(scope==='work'&&v.model===followModel?t('runtime.followConversationModel'):found?.name||saved.model||t(runtime?'runtime.default':'runtime.selectModel'))}</strong>{runtime==='codex'&&<small className="settings-source-badge">Codex</small>}{selectionLoaded&&runtime&&runtime!=='conversation'&&views[runtime as ID]&&views[runtime as ID]?.state.state!=='ready'&&<small>{t('runtime.notConnected')}</small>}{selectionLoaded&&!selectionFailed&&!runtime&&<small>{t('settings.connectionPrepareFirst')}</small>}</span><SettingsChevron/></button>};
 const stateText=(runtime:ID)=>views[runtime]?t(setupStatusKey(views[runtime]!.state)):failures[runtime]?t('runtime.loadRuntimeFailed'):t('runtime.detecting');
 const topError=<>{error&&<p className="inline-error" role="alert">{error}</p>}{notice&&<p className="settings-note" role="status">{notice}</p>}</>;
 return <section ref={root} className="connections-page">
  {toolbar&&['runtime','team','providers','provider','connect'].includes(route.page)&&createPortal(<div className="settings-subpage-crumb"><button className="text-action" disabled={busy} onClick={()=>go({page:'home'})}>{t('settings.connections')}</button><span aria-hidden="true">/</span>{route.page==='runtime'?<h1>{name(id)}</h1>:<><button className="text-action" disabled={busy} onClick={()=>go({page:'runtime',id})}>{name(id)}</button><span aria-hidden="true">/</span>{route.page==='connect'?<><button className="text-action" disabled={busy} onClick={()=>go({page:'providers',id})}>{t('runtime.modelConnections')}</button><span aria-hidden="true">/</span><h1>{providerName(route.choice,route.choice)}</h1></>:<h1>{t(route.page==='team'?'runtime.teamSettings':'runtime.modelConnections')}</h1>}</>}</div>,toolbar)}
  {route.page==='home'?<>
   <SettingGroup title={t('settings.connectionBasics')}><SettingRow label={t('settings.connectionBotModel')}>{summary('conversation')}</SettingRow><SettingRow label={t('settings.connectionWorkModel')}>{summary('work')}</SettingRow></SettingGroup>
   <SettingGroup title={t('settings.connectionRuntimes')}>{(['caelis','codex'] as const).map(runtime=><button className="connection-navigation-row" key={runtime} onClick={()=>go({page:'runtime',id:runtime})}><span><strong>{name(runtime)}</strong><small>{t(runtime==='caelis'?'settings.connectionBuiltinHint':'settings.connectionCodexHint')}</small></span><span className="connection-navigation-value">{stateText(runtime)}<SettingsChevron/></span></button>)}</SettingGroup>
   <div className="settings-footer"><button disabled={loading} onClick={()=>void refresh()}>{t('runtime.recheck')}</button></div>
  </>:route.page==='connect'?<ConnectionWizard inline onBusyChange={value=>{action.current=value;setBusy(value)}} hideHeader={!!toolbar} initialKind={route.kind} initialChoice={route.choice} client={client} active={active} onClose={()=>{action.current=false;setBusy(false);go({page:'providers',id})}} onConnected={async()=>{await refresh()}}/>:route.page==='team'&&config?<TeamSettings inline hideHeader={!!toolbar} onBack={back} team={config.team} models={config.team.models} client={{...client,changeTeam:async(change,revision)=>{const r=await backend<import('../../backend/contract').RuntimeMutationResult>('ChangeProfileConfiguration',id,{...change,expectedRevision:revision});if(r.outcome!=='committed')throw new ConfigurationError(r)}}} onRefresh={async()=>setConfig(await backend<RuntimeConfiguration>('ProfileConfiguration',id))}/>:<SettingsPage hideHeader={!!toolbar&&['runtime','team','providers','provider','connect'].includes(route.page)} title={route.page==='runtime'?name(id):t(route.page==='providers'?'runtime.modelConnections':route.page==='team'?'runtime.teamSettings':'settings.connectionModels')} busy={busy} onClose={back}>
   {topError}
   {route.page==='team'&&!config&&!error&&<p role="status" className="settings-note">{t('common.loading')}</p>}
   {route.page==='runtime'&&id==='codex'&&view&&<CodexRuntimePanel state={view.state} busy={busy} checking={loading||operation==='check-update'} onAction={act=>void apply(act)} onDetect={()=>void refresh()}/> }
   {route.page==='runtime'&&id==='codex'&&!view&&<p role="status" className="settings-note">{failures.codex||t('runtime.detecting')}</p>}
   {route.page==='runtime'&&id==='caelis'&&(view?<BuiltinRuntimePanel state={view.state} busy={busy} checking={loading} count={config?.models.length} onAction={act=>void apply(act)} onConnect={()=>go({page:'providers',id})} onTeam={()=>go({page:'team',id})} onDetect={()=>void refresh()}/>:<p role="status" className="settings-note">{failures.caelis||t('runtime.detecting')}</p>)}
   {route.page==='providers'&&<><div className="connection-page-heading"><p>{t('settings.connectionProviderHint')}</p></div>{config&&config.connections.length>0&&<SettingGroup title={t('settings.connectionAdded')}>{config.connections.map(g=><button className="connection-navigation-row" key={g.id} onClick={()=>go({page:'provider',id,group:g.id})}><span><strong>{g.name}</strong><small>{t('runtime.modelCount',{count:g.models.length})}</small></span><SettingsChevron/></button>)}</SettingGroup>}<SettingGroup title={t('connections.addConnection')}>{providerList.map(p=><button className="connection-navigation-row" key={p.kind+p.id} onClick={()=>go({page:'connect',id,kind:p.kind,choice:p.id})}><span className="provider-brand"><ProviderLogo id={p.id}/><strong>{providerName(p.id,p.name)}</strong></span><SettingsChevron/></button>)}</SettingGroup>{providerError&&<p role="alert" className="inline-error">{providerError}</p>}</>}
   {route.page==='provider'&&config&&<SettingGroup title={config.connections.find(g=>g.id===route.group)?.name}>{config.connections.find(g=>g.id===route.group)?.models.map(m=><SettingRow key={m.id} label={m.name} description={m.unavailable?t('runtime.reauthNeeded'):<span className="model-usage-tags">{m.uses.length>0&&<span>{t('settings.modelInUse')}</span>}{[...new Set(m.uses.map(modelUseLabel))].map(u=><span className="model-usage-tag" key={u}>{u}</span>)}</span>}><button disabled={!!m.uses.length||busy} onClick={()=>{const group=config.connections.find(g=>g.id===route.group)!;setRemoveError('');setRemoving({id:group.kind==='agent'?group.id:m.id,name:m.name,action:group.kind==='agent'?'disconnect-agent':'remove-model'})}}>{t('runtime.remove')}</button></SettingRow>)}</SettingGroup>}
   {busy&&!['install','update'].includes(operation)&&<p role="status" className="settings-note">{t('runtime.processing')}</p>}
  </SettingsPage>}
  {['install','update'].includes(operation)&&<SettingsDialog className="runtime-install-dialog" title={t(operationError?'settings.runtimeInstallFailed':operation==='install'?'settings.codexInstalling':'settings.codexUpdating',{name:name(id)})} busy={busy} onClose={()=>{if(!busy){setOperation('');setOperationError('')}}}>
   {operationError?<p className="inline-error" role="alert">{operationError}</p>:<><div className="runtime-install-progress" role="progressbar" aria-label={t(operation==='install'?'settings.codexInstalling':'settings.codexUpdating',{name:name(id)})}><span/></div><p className="settings-note" role="status">{t('settings.runtimeInstallWait')}</p></>}
   {operationError&&<div className="setup-end"><button onClick={()=>{setOperation('');setOperationError('')}}>{t('common.close')}</button><button className="primary" onClick={()=>void apply(operation)}>{t('settings.runtimeTryAgain')}</button></div>}
  </SettingsDialog>}
  {removing&&<SettingsDialog title={t('runtime.removeModelPrompt',{name:removing.name})} busy={busy} onClose={()=>setRemoving(null)}><p className="settings-note">{t('runtime.removeModelNote')}</p>{removeError&&<p role="alert" className="inline-error">{removeError}</p>}<div className="setup-end"><button disabled={busy} onClick={()=>setRemoving(null)}>{t('common.cancel')}</button><button disabled={busy||!!removeError} onClick={()=>void run(async()=>{try{const result=await backend<import('../../backend/contract').RuntimeMutationResult>('ChangeProfileConfiguration',id,{action:removing.action,id:removing.id,expectedRevision:config?.revision});if(result.outcome!=='committed')throw new ConfigurationError(result);setRemoving(null);await refresh()}catch(e){setRemoveError(textError(e))}})}>{t('runtime.remove')}</button></div></SettingsDialog>}
  {editing&&<ModelPicker description={!models.length?t('settings.connectionPrepareFirst'):undefined} showSources sourceLabels={Object.fromEntries(pickerModels.filter(m=>decode(m.model)[0]==='codex').map(m=>[m.model,'Codex']))} anchor={anchor.current} title={t(editing==='conversation'?'settings.connectionBotModel':'settings.connectionWorkModel')} value={modelValue(editing)} missingModelLabel={(editing==='work'?worker:bot).model||t('runtime.selectModel')} models={pickerModels} onReload={refresh} onClose={()=>setEditing(null)} onSave={async value=>{const [runtime,model]=decode(value.model);await backend('SaveSelectedModel',editing,runtime,{...value,model});await refresh()}}/>}
 </section>
}
