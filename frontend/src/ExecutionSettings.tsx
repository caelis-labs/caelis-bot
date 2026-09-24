import { useEffect, useRef, useState } from 'react';
import { backend } from './desktop';
import { WorkExecutionSettings, inheritedWork, effortLabels, tierLabel } from './WorkExecutionSettings';
import { SettingGroup, SettingRow } from './SettingsUI';
import { sameExecutionSettings, sameModelSettings, saveModelChanges } from './model-settings';
import type { ExecutionSettings as Preferences, WorkExecutionSettings as WorkPreferences, ModelOption, ExecutionOptions } from './backend/contract';


export function ExecutionSettings(){
 const [options,setOptions]=useState<ExecutionOptions>({defaultApprovalMode:'',approvalModes:[]});
 const [value,setValue]=useState<Preferences|null>(null),[saved,setSaved]=useState<Preferences|null>(null);
 const [work,setWork]=useState<WorkPreferences>(inheritedWork),[savedWork,setSavedWork]=useState<WorkPreferences>(inheritedWork);
 const [models,setModels]=useState<ModelOption[]>([]),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[error,setError]=useState(''),[loading,setLoading]=useState(true),[inherited,setInherited]=useState(false);
 const running=useRef(false);
 const load=async()=>{
  if(running.current)return;running.current=true;setLoading(true);setError('');setMessage('');
  try{
   const [prefs,list,opts,worker]=await Promise.all([backend<Preferences>('ExecutionSettings'),backend<ModelOption[]>('Models'),backend<ExecutionOptions>('ExecutionOptions'),backend<WorkPreferences>('WorkExecutionSettings')]);
   setOptions(opts);setModels(list);setInherited(!prefs.model);
   const model=list.find(m=>m.model===prefs.model)||(prefs.model?undefined:list.find(m=>m.default)??list[0]);
   const initial=prefs.model?prefs:{...prefs,model:model?.model??'',effort:model?.defaultEffort??'',approvalMode:prefs.approvalMode||opts.defaultApprovalMode};
   setValue(initial);setSaved(initial);setWork(worker);setSavedWork(worker);
  }catch(e){setError(e instanceof Error?e.message:'暂时无法读取模型设置');}
  finally{setLoading(false);running.current=false;}
 };
 useEffect(()=>{void load();},[]);
 const conversationDirty=!!value&&!!saved&&!sameExecutionSettings(value,saved);
 const workDirty=!sameModelSettings(work,savedWork),dirty=conversationDirty||workDirty;
 const change=(next:Preferences)=>{setValue(next);setMessage('');setError('');};
 const changeWork=(next:WorkPreferences)=>{setWork(next);setMessage('');setError('');};
 const model=models.find(m=>m.model===value?.model);
 const mode=options.approvalModes.find(m=>m.id===(value?.approvalMode||options.defaultApprovalMode));
 const save=async()=>{
  if(!value||!saved||running.current)return;running.current=true;setBusy(true);setMessage('');setError('');
  try{
   await saveModelChanges({conversation:value,work},{conversation:saved,work:savedWork},backend,scope=>{
    if(scope==='conversation'){setSaved(value);setInherited(false);}else setSavedWork(work);
   });
   setMessage('更改已保存。对话设置用于下次请求，工作模型用于新建任务。');
  }catch(e){setError(e instanceof Error?e.message:'设置未能保存');}
  finally{setBusy(false);running.current=false;}
 };
 return <section className="execution-settings"><h1>模型与权限</h1><p className="settings-intro">更改后统一保存，无需重启应用。</p>

 {value&&<fieldset disabled={busy||loading} className="execution-fields">
  <SettingGroup title="Bot 对话模型">
   <SettingRow label="使用模型" description={inherited?'当前沿用运行时的默认模型':'用于下一次请求'} htmlFor="execution-model"><select id="execution-model" value={value.model} onChange={e=>{const m=models.find(m=>m.model===e.target.value)!;change({...value,model:m.model,effort:m.defaultEffort,serviceTier:''});setMessage('');}}>
    {!model&&<option value={value.model}>{value.model||'没有可用模型'}</option>}{models.map(m=><option key={m.model} value={m.model}>{m.name||m.model}{m.default?' · 默认':''}</option>)}
   </select></SettingRow>
   <SettingRow label="推理强度" htmlFor="execution-effort"><select id="execution-effort" value={value.effort} onChange={e=>change({...value,effort:e.target.value})}>
    {!model?.efforts.includes(value.effort)&&<option value={value.effort}>{value.effort||'由模型决定'}</option>}{model?.efforts.map(e=><option key={e} value={e}>{effortLabels[e]??e}</option>)}
   </select></SettingRow>
   <SettingRow label="响应速度" htmlFor="execution-tier"><select id="execution-tier" value={value.serviceTier} onChange={e=>change({...value,serviceTier:e.target.value})}>
    <option value="">标准</option>{value.serviceTier&&!model?.serviceTiers.some(t=>t.id===value.serviceTier)&&<option value={value.serviceTier}>{value.serviceTier} · 当前不可用</option>}{model?.serviceTiers.filter(t=>t.id).map(t=><option key={t.id} value={t.id}>{tierLabel(t.id,t.name)}</option>)}
   </select></SettingRow>
  </SettingGroup>
  {options.approvalModes.length>0&&<SettingGroup title="权限"><SettingRow label="审批方式" description={<span className={mode?.dangerous?'permission-warning':undefined}>{mode?.description}</span>} htmlFor="execution-approval"><select id="execution-approval" value={value.approvalMode||options.defaultApprovalMode} onChange={e=>change({...value,approvalMode:e.target.value})}>{!mode&&<option value={value.approvalMode}>{value.approvalMode||'当前不可用'}</option>}{options.approvalModes.map(m=><option key={m.id} value={m.id}>{m.name}</option>)}</select></SettingRow>
  </SettingGroup>}
 <WorkExecutionSettings value={work} models={models} onChange={changeWork}/>
 </fieldset>}
 {error&&<p className="inline-error" role="alert">{error}</p>}
 {message&&<p className="settings-note" role="status">{message}</p>}
 <div className="settings-footer settings-save-bar">
  {dirty?<><span className="settings-note" role="status">有未保存的更改</span><button disabled={busy||loading} onClick={()=>{setValue(saved);setWork(savedWork);setError('');setMessage('');}}>撤销更改</button></>:<button disabled={busy||loading} onClick={()=>void load()}>{loading?'正在加载…':'重新加载'}</button>}
  <button className="primary" disabled={busy||loading||!dirty||(conversationDirty&&!model)||(workDirty&&!!work.model&&!models.some(m=>m.model===work.model))} onClick={()=>void save()}>{busy?'正在保存…':'保存更改'}</button>
 </div>
 </section>;
}
