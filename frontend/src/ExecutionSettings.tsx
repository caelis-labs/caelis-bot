import { useEffect, useState } from 'react';
import { backend } from './desktop';
import { SettingGroup, SettingRow, SettingHelp } from './SettingsUI';
import type { ExecutionSettings as Preferences, ModelOption } from './backend/contract';

const modes=[['auto','自动审查','保持工作目录写入沙箱，由 Codex 自动审查需要额外权限的操作。'],['ask','由我确认','保持工作目录写入沙箱，需要额外权限时向你确认。'],['read-only','只读沙箱','本地命令使用只读沙箱，不申请提升权限；连接器和 Bot 工具仍遵循各自的授权。'],['full-access','完全访问','关闭命令沙箱并不再询问执行权限。只在你明确需要时使用。']] as const;
const effortLabels:Record<string,string>={none:'无',minimal:'最低',low:'低',medium:'中',high:'高',xhigh:'极高',max:'最高',ultra:'超高'};
export function ExecutionSettings(){
 const [value,setValue]=useState<Preferences|null>(null),[models,setModels]=useState<ModelOption[]>([]),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[loading,setLoading]=useState(true),[inherited,setInherited]=useState(false);
 const load=async()=>{setLoading(true);setMessage('');try{const [prefs,list]=await Promise.all([backend<Preferences>('ExecutionSettings'),backend<ModelOption[]>('Models')]);setModels(list);setInherited(!prefs.model);const model=list.find(m=>m.model===prefs.model)||(prefs.model?undefined:list.find(m=>m.default)??list[0]);setValue(prefs.model?prefs:{...prefs,model:model?.model??'',effort:model?.defaultEffort??'',approvalMode:prefs.approvalMode||'auto'});}catch(e){setMessage(e instanceof Error?e.message:'暂时无法读取模型设置');}finally{setLoading(false);}};
 useEffect(()=>{void load();},[]);
 const model=models.find(m=>m.model===value?.model);
 const save=async()=>{if(!value)return;setBusy(true);setMessage('');try{await backend('SaveExecutionSettings',value);setInherited(false);setMessage('已保存，下次请求生效。');}catch(e){setMessage(e instanceof Error?e.message:'设置未能保存');}finally{setBusy(false);}};
 return <section className="execution-settings"><h1>模型与权限</h1>
 {inherited&&<p className="settings-intro">当前沿用 Codex 配置，保存后使用以下设置。</p>}
 {value&&<fieldset disabled={busy||loading} className="execution-fields">
  <SettingGroup title="模型">
   <SettingRow label="使用模型" htmlFor="execution-model"><select id="execution-model" value={value.model} onChange={e=>{const m=models.find(m=>m.model===e.target.value)!;setValue({...value,model:m.model,effort:m.defaultEffort,serviceTier:''});setMessage('');}}>
    {!model&&<option value={value.model}>{value.model||'没有可用模型'}</option>}{models.map(m=><option key={m.model} value={m.model}>{m.name||m.model}{m.default?' · 默认':''}</option>)}
   </select></SettingRow>
   <SettingRow label="推理强度" htmlFor="execution-effort"><select id="execution-effort" value={value.effort} onChange={e=>setValue({...value,effort:e.target.value})}>
    {!model?.efforts.includes(value.effort)&&<option value={value.effort}>{value.effort||'由模型决定'}</option>}{model?.efforts.map(e=><option key={e} value={e}>{effortLabels[e]??e}</option>)}
   </select></SettingRow>
   <SettingRow label="响应速度" htmlFor="execution-tier"><select id="execution-tier" value={value.serviceTier} onChange={e=>setValue({...value,serviceTier:e.target.value})}>
    <option value="">标准</option>{value.serviceTier&&!model?.serviceTiers.some(t=>t.id===value.serviceTier)&&<option value={value.serviceTier}>{value.serviceTier} · 当前不可用</option>}{model?.serviceTiers.filter(t=>t.id).map(t=><option key={t.id} value={t.id}>{t.name||t.id}</option>)}
   </select></SettingRow>
  </SettingGroup>
  <SettingGroup title="权限"><SettingRow label="审批方式" htmlFor="execution-approval"><select id="execution-approval" value={value.approvalMode||'auto'} onChange={e=>setValue({...value,approvalMode:e.target.value})}>{modes.map(([id,name])=><option key={id} value={id}>{name}</option>)}</select></SettingRow>
   <p className={`setting-feedback ${value.approvalMode==='full-access'?'permission-warning':''}`}>{modes.find(([id])=>id===(value.approvalMode||'auto'))?.[2]}</p>
  </SettingGroup>
 </fieldset>}
 {message&&<p className="settings-note" role="status">{message}</p>}
 <div className="settings-footer"><button disabled={busy||loading} onClick={()=>void load()}>{loading?'正在加载…':'刷新选项'}</button><button className="primary" disabled={busy||loading||!model} onClick={()=>void save()}>{busy?'正在保存…':'保存设置'}</button></div>
 <SettingHelp><p>{model?.description||'模型选项来自本机 Codex。'}</p><p>{value?.serviceTier?model?.serviceTiers.find(t=>t.id===value.serviceTier)?.description:'Fast 是否可用取决于当前模型，用量以 Codex 账户规则为准。'}</p><p>请在工作结束后保存。配置仅用于新请求。</p></SettingHelp>
 </section>;
}
