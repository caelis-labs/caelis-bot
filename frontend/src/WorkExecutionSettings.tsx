import { SettingGroup, SettingRow } from './SettingsUI';
import type { WorkExecutionSettings as Preferences, ModelOption } from './backend/contract';

export const inheritedWork: Preferences = { model: '', effort: '', serviceTier: '' };
export const effortLabels: Record<string, string> = {none:'无',minimal:'最低',low:'低',medium:'中',high:'高',xhigh:'极高',max:'最高',ultra:'超高'};

export const tierLabel = (id:string,name:string) => (id === 'fast' || name.toLowerCase() === 'fast') ? '快速（Fast）' : name || id;

export function WorkExecutionSettings({value,models,onChange}:{value:Preferences;models:ModelOption[];onChange:(value:Preferences)=>void}) {
 const manual=!!value.model,model=models.find(m=>m.model===value.model);
 const choose=(m:ModelOption)=>onChange({model:m.model,effort:m.defaultEffort,serviceTier:''});
 return <details className="work-model-settings">
  <summary>独立工作模型 <span>{manual ? model?.name || value.model : '沿用运行时设置'}</span></summary>
  <p className="settings-note">可为委派任务单独选择模型，只影响新建任务。已有任务保持原设置。</p>
   <SettingGroup>
    <SettingRow label="模型来源" htmlFor="work-model-source">
     <select id="work-model-source" value={manual ? 'manual' : 'runtime'} onChange={e => {
      const custom = e.target.value === 'manual';
      if (!custom) onChange(inheritedWork);
      else if (!model) { const first = models.find(m => m.default) ?? models[0]; if (first) choose(first); }
     }}><option value="runtime">沿用运行时设置</option><option value="manual">单独指定</option></select>
    </SettingRow>
    {!manual && <p className="setting-feedback">使用当前运行时的模型、推理强度和速度；未配置模型时使用上方对话模型。</p>}
    {manual && <>
     <SettingRow label="工作模型" htmlFor="work-model"><select id="work-model" value={value.model} onChange={e => { const selected = models.find(m => m.model === e.target.value); if (selected) choose(selected); }}>
      {!model && <option value={value.model}>{value.model || '没有可用模型'}</option>}{models.map(m => <option key={m.model} value={m.model}>{m.name || m.model}</option>)}
     </select></SettingRow>
     <SettingRow label="推理强度" htmlFor="work-effort"><select id="work-effort" value={value.effort} onChange={e => onChange({...value, effort:e.target.value})}>
      {!model?.efforts.includes(value.effort) && <option value={value.effort}>{value.effort ? `${value.effort} · 当前不可用` : '由运行时决定'}</option>}{model?.efforts.filter(Boolean).map(e => <option key={e} value={e}>{effortLabels[e] ?? e}</option>)}
     </select></SettingRow>
     <SettingRow label="响应速度" htmlFor="work-tier"><select id="work-tier" value={value.serviceTier} onChange={e => onChange({...value, serviceTier:e.target.value})}>
      <option value="">标准</option>{value.serviceTier && !model?.serviceTiers.some(t => t.id === value.serviceTier) && <option value={value.serviceTier}>{value.serviceTier} · 当前不可用</option>}{model?.serviceTiers.filter(t => t.id).map(t => <option key={t.id} value={t.id}>{tierLabel(t.id,t.name)}</option>)}
     </select></SettingRow>
    </>}
   </SettingGroup>
 </details>;
}
