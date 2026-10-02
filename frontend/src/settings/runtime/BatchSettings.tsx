import { useRef, useState } from 'react';
import type { NodeCatalog } from '../../backend/contract';
import type { RuntimeView } from './types';
import type { NodeSettingsClient } from './nodeClient';
import { applyBatchTemplate, BatchError, makeBatchTemplate, type BatchOptions, type BatchReason, type BatchResult, type ConnectionTemplate } from './batch';
import { ConfigurationError } from './client';
import { selectionSummary } from './state';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';
import type { MessageKey } from '../../i18n/catalogs';

const reasonKeys:Record<BatchReason,MessageKey>={authentication:'runtime.batchAuthentication',unavailable:'runtime.batchUnavailable',model:'runtime.batchModelUnavailable',mapping:'runtime.batchMappingUnavailable',team:'runtime.batchTeamUnavailable',connection:'runtime.batchConnectionUnavailable',pending:'settings.nodeOperationUnknown',failed:'runtime.batchFailed',unknown:'settings.nodeOperationUnknown'};
const outcomeKeys:Record<BatchResult['outcome'],MessageKey>={applying:'runtime.batchApplying',committed:'runtime.batchApplied',partial:'runtime.batchPartial',unknown:'runtime.batchUnknown',failed:'runtime.batchFailed', 'needs-initialization':'runtime.batchNeedsInitialization'};
export function BatchSettings({catalog,owner,sourceId,view,connection,onClose,onSelect}:{catalog:NodeCatalog;owner:NodeSettingsClient;sourceId:string;view:RuntimeView;connection?:ConnectionTemplate;onClose:()=>void;onSelect:(id:string)=>void}) {
 const {t}=useI18n();
 const backend=view.profile.runtime;
 const targets=catalog.nodes.filter(node=>node.id!==sourceId&&node.runtimes.some(runtime=>runtime.backend===backend));
 const [selected,setSelected]=useState<string[]>([]),[options,setOptions]=useState<BatchOptions>({conversation:!connection&&backend==='codex'&&!!view.conversation,work:!connection&&backend==='codex'&&!!view.work,main:!connection&&backend==='caelis'&&!!view.main,team:false,connection:!!connection,teamSet:''});
 const [results,setResults]=useState<BatchResult[]>([]),[error,setError]=useState<MessageKey|''>(''),[busy,setBusy]=useState(false),[started,setStarted]=useState(false);
 const working=useRef(false);
 const choices:[keyof Pick<BatchOptions,'conversation'|'work'|'main'|'team'|'connection'>,MessageKey,boolean][]=[['conversation','runtime.botConversationModel',backend==='codex'&&!!view.conversation],['work','runtime.workModel',backend==='codex'&&!!view.work],['main','runtime.caelisMainModel',backend==='caelis'&&!!view.main],['team','runtime.agentTeam',backend==='caelis'&&view.team.available],['connection','runtime.batchConnectionTemplate',!!connection]];
 const run=async()=>{
  if(working.current||started)return;
  let template;
  try{template=makeBatchTemplate(view,options,connection);}catch(error){setError(reasonKeys[error instanceof BatchError?error.reason:'failed']);return;}
  working.current=true;setBusy(true);setStarted(true);setError('');
  try {await applyBatchTemplate(owner,template,targets.filter(node=>selected.includes(node.id)),result=>setResults(previous=>[...previous.filter(value=>value.nodeId!==result.nodeId),result]));}
  finally{working.current=false;setBusy(false);}
 };
 const reconcile=async(result:BatchResult)=>{
  if(working.current)return;working.current=true;setBusy(true);
  try {
   const receipt=await owner.reconcile(result.nodeId,backend);
   // A reconciled individual operation does not complete remaining batch steps.
   if(receipt?.outcome==='committed')setResults(previous=>previous.map(value=>value.nodeId===result.nodeId?{...value,outcome:'partial',reason:'failed',applied:[...value.applied,'receipt']}:value));
  }catch(error){
   if(error instanceof ConfigurationError&&!error.unknown)setResults(previous=>previous.map(value=>value.nodeId===result.nodeId?{...value,outcome:value.applied.length?'partial':'failed',reason:'failed'}:value));
   else setError('settings.nodeOperationUnknown');
  }
  finally{working.current=false;setBusy(false);}
 };
 const summary=(key:string)=>{
  if(key==='connection')return connection?[connection.choice,connection.model,connection.baseUrl].join(' · '):'';
  if(key==='team')return view.team.roles.filter(role=>role.id!=='self').map(role=>`${role.id}: ${selectionSummary(role.selection,view.team.models,t).name}`).join(' · ');
  const value=key==='conversation'?view.conversation:key==='work'?view.work:view.main;
  if(!value)return '';
  const text=selectionSummary(value,view.models,t);return [text.name,text.detail].filter(Boolean).join(' · ');
 };
 const appliedLabel=(item:string)=>item.startsWith('role:')?t('runtime.batchRole',{id:item.slice(5)}):item.startsWith('create:')?t('runtime.batchRoleCreated',{id:item.slice(7)}):t(({conversation:'runtime.botConversationModel',work:'runtime.workModel',runtime:'runtime.caelisMainModel',connection:'runtime.batchConnectionTemplate',teamSet:'runtime.teamScheme',receipt:'runtime.batchReceiptConfirmed'} as Record<string,MessageKey>)[item]);
 return <SettingsDialog title={t('runtime.batchApply')} description={t('runtime.batchDescription')} busy={busy} onClose={onClose}>
  <fieldset disabled={busy||started} className="runtime-batch-options"><legend>{t('runtime.batchConfiguration')}</legend>{choices.filter(([, ,available])=>available).map(([key,label])=><label className="runtime-check" key={key}><input type="checkbox" checked={options[key]} onChange={event=>setOptions(value=>({...value,[key]:event.target.checked}))}/><span>{t(label)}<small>{summary(key)}</small></span></label>)}{options.team&&<label>{t('runtime.batchTeamSet')}<input value={options.teamSet} onChange={event=>setOptions(value=>({...value,teamSet:event.target.value}))}/></label>}</fieldset>
  <p className="settings-note">{t('runtime.batchPathNote')}</p>
  <fieldset disabled={busy||started} className="runtime-batch-options"><legend>{t('runtime.batchNodes')}</legend>{targets.map(node=>{const status=node.runtimes.find(value=>value.backend===backend)!;return <label className="runtime-check" key={node.id}><input type="checkbox" checked={selected.includes(node.id)} onChange={event=>setSelected(value=>event.target.checked?[...value,node.id]:value.filter(id=>id!==node.id))}/><span>{node.label}{status.authentication!=='authenticated'&&<small> · {t('runtime.batchNeedsInitialization')}</small>}</span></label>;})}{!targets.length&&<p className="settings-note">{t('runtime.batchNoNodes')}</p>}</fieldset>
  {!!results.length&&<div aria-live="polite" className="runtime-batch-results">{results.map(result=><div key={result.nodeId} className="runtime-batch-result"><strong>{result.label} · {t(outcomeKeys[result.outcome])}</strong>{result.applied.length>0&&<p>{t('runtime.batchSaved',{items:result.applied.map(appliedLabel).join(' · ')})}</p>}{result.reason&&<p>{t(reasonKeys[result.reason])}</p>}{result.outcome==='unknown'&&owner.pending(result.nodeId,backend)&&<button disabled={busy} onClick={()=>void reconcile(result)}>{t('settings.productCheckOriginalReceipt')}</button>}{result.outcome!=='committed'&&result.outcome!=='applying'&&<button disabled={busy} className="text-action" onClick={()=>{onClose();onSelect(result.nodeId);}}>{t('runtime.batchOpenNode')}</button>}</div>)}</div>}
  {error&&<p role="alert" className="inline-error">{t(error)}</p>}
  <div className="setup-end"><button disabled={busy} onClick={onClose}>{t(started?'runtime.closeDialog':'common.cancel')}</button>{!started&&<button className="primary" disabled={busy||!selected.length||!choices.some(([key,,available])=>available&&options[key])} onClick={()=>void run()}>{t('runtime.batchApplyCount',{count:selected.length})}</button>}</div>
 </SettingsDialog>;
}
