import { useEffect, useState } from 'react';
import { backend } from './desktop';
import { WorkExecutionSettings } from './WorkExecutionSettings';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { ExecutionSettings as Preferences, ModelOption, ExecutionOptions } from './backend/contract';


const effortLabels:Record<string,string>={none:'无',minimal:'最低',low:'低',medium:'中',high:'高',xhigh:'极高',max:'最高',ultra:'超高'};
export function ExecutionSettings(){
 const [options,setOptions]=useState<ExecutionOptions>({defaultApprovalMode:'',approvalModes:[]});
 const [value,setValue]=useState<Preferences|null>(null),[models,setModels]=useState<ModelOption[]>([]),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[loading,setLoading]=useState(true),[inherited,setInherited]=useState(false);
 const load=async()=>{setLoading(true);setMessage('');try{const [prefs,list,opts]=await Promise.all([backend<Preferences>('ExecutionSettings'),backend<ModelOption[]>('Models'),backend<ExecutionOptions>('ExecutionOptions')]);setOptions(opts);setModels(list);setInherited(!prefs.model);const model=list.find(m=>m.model===prefs.model)||(prefs.model?undefined:list.find(m=>m.default)??list[0]);setValue(prefs.model?prefs:{...prefs,model:model?.model??'',effort:model?.defaultEffort??'',approvalMode:prefs.approvalMode||opts.defaultApprovalMode});}catch(e){setMessage(e instanceof Error?e.message:'暂时无法读取模型设置');}finally{setLoading(false);}};
 useEffect(()=>{void load();},[]);
 const model=models.find(m=>m.model===value?.model);
 const mode=options.approvalModes.find(m=>m.id===(value?.approvalMode||options.defaultApprovalMode));
 const save=async()=>{if(!value)return;setBusy(true);setMessage('');try{await backend('SaveExecutionSettings',value);setInherited(false);setMessage('已保存，下次请求生效。');}catch(e){setMessage(e instanceof Error?e.message:'设置未能保存');}finally{setBusy(false);}};
 return <section className="execution-settings"><h1>模型与权限</h1>

 {value&&<fieldset disabled={busy||loading} className="execution-fields">
  <SettingGroup title="Bot 对话模型">
   <SettingRow label="使用模型" description={inherited?'当前沿用运行时的默认模型':'用于下一次请求'} htmlFor="execution-model"><select id="execution-model" value={value.model} onChange={e=>{const m=models.find(m=>m.model===e.target.value)!;setValue({...value,model:m.model,effort:m.defaultEffort,serviceTier:''});setMessage('');}}>
    {!model&&<option value={value.model}>{value.model||'没有可用模型'}</option>}{models.map(m=><option key={m.model} value={m.model}>{m.name||m.model}{m.default?' · 默认':''}</option>)}
   </select></SettingRow>
   <SettingRow label="推理强度" htmlFor="execution-effort"><select id="execution-effort" value={value.effort} onChange={e=>setValue({...value,effort:e.target.value})}>
    {!model?.efforts.includes(value.effort)&&<option value={value.effort}>{value.effort||'由模型决定'}</option>}{model?.efforts.map(e=><option key={e} value={e}>{effortLabels[e]??e}</option>)}
   </select></SettingRow>
   <SettingRow label="响应速度" htmlFor="execution-tier"><select id="execution-tier" value={value.serviceTier} onChange={e=>setValue({...value,serviceTier:e.target.value})}>
    <option value="">标准</option>{value.serviceTier&&!model?.serviceTiers.some(t=>t.id===value.serviceTier)&&<option value={value.serviceTier}>{value.serviceTier} · 当前不可用</option>}{model?.serviceTiers.filter(t=>t.id).map(t=><option key={t.id} value={t.id}>{t.name||t.id}</option>)}
   </select></SettingRow>
  </SettingGroup>
  {options.approvalModes.length>0&&<SettingGroup title="权限"><SettingRow label="审批方式" description={<span className={mode?.dangerous?'permission-warning':undefined}>{mode?.description}</span>} htmlFor="execution-approval"><select id="execution-approval" value={value.approvalMode||options.defaultApprovalMode} onChange={e=>setValue({...value,approvalMode:e.target.value})}>{!mode&&<option value={value.approvalMode}>{value.approvalMode||'当前不可用'}</option>}{options.approvalModes.map(m=><option key={m.id} value={m.id}>{m.name}</option>)}</select></SettingRow>
  </SettingGroup>}
 </fieldset>}
 {message&&<p className="settings-note" role="status">{message}</p>}
 <div className="settings-footer"><button disabled={busy||loading} onClick={()=>void load()}>{loading?'正在加载…':'刷新选项'}</button><button className="primary" disabled={busy||loading||!model} onClick={()=>void save()}>{busy?'正在保存…':'保存 Bot 设置'}</button></div>
 <WorkExecutionSettings/>
 </section>;
}
