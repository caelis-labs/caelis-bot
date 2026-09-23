import { useEffect, useState } from 'react';
import { backend } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { WorkExecutionSettings as Preferences, ModelOption } from './backend/contract';

const inherited: Preferences = { model: '', effort: '', serviceTier: '' };
const effortLabels: Record<string, string> = {none:'无',minimal:'最低',low:'低',medium:'中',high:'高',xhigh:'极高',max:'最高',ultra:'超高'};

export function WorkExecutionSettings() {
 const [value, setValue] = useState<Preferences>(inherited);
 const [manual, setManual] = useState(false);
 const [models, setModels] = useState<ModelOption[]>([]);
 const [busy, setBusy] = useState(false);
 const [loading, setLoading] = useState(true);
 const [loaded, setLoaded] = useState(false);
 const [message, setMessage] = useState('');
 const load = async () => {
  setLoading(true); setMessage('');
  try {
   const [prefs, list] = await Promise.all([backend<Preferences>('WorkExecutionSettings'), backend<ModelOption[]>('Models')]);
   setValue(prefs); setManual(!!prefs.model); setModels(list); setLoaded(true);
  } catch (e) { setMessage(e instanceof Error ? e.message : '暂时无法读取工作模型设置'); }
  finally { setLoading(false); }
 };
 useEffect(() => { void load(); }, []);
 const model = models.find(m => m.model === value.model);
 const choose = (m: ModelOption) => setValue({model:m.model, effort:m.defaultEffort, serviceTier:''});
 const save = async () => {
  setBusy(true); setMessage('');
  try {
   await backend('SaveWorkExecutionSettings', manual ? value : inherited);
   setMessage('工作模型已保存，仅用于新建任务。已有任务保持原设置。');
  } catch (e) { setMessage(e instanceof Error ? e.message : '工作模型设置未能保存'); }
  finally { setBusy(false); }
 };
 return <>
  <fieldset disabled={busy || loading || !loaded} className="execution-fields">
   <SettingGroup title="工作任务模型">
    <SettingRow label="模型来源" description="独立于 Bot 对话模型，仅影响新建的工作任务。" htmlFor="work-model-source">
     <select id="work-model-source" value={manual ? 'manual' : 'runtime'} onChange={e => {
      const custom = e.target.value === 'manual'; setManual(custom); setMessage('');
      if (custom && !model) { const first = models.find(m => m.default) ?? models[0]; if (first) choose(first); }
     }}><option value="runtime">沿用 Runtime 配置</option><option value="manual">手动指定</option></select>
    </SettingRow>
    {!manual && <p className="settings-note">创建任务时读取 Runtime 的模型、推理强度和响应速度。Runtime 未配置模型时，使用 Bot 的模型设置。</p>}
    {manual && <>
     <SettingRow label="工作模型" htmlFor="work-model"><select id="work-model" value={value.model} onChange={e => { const selected = models.find(m => m.model === e.target.value); if (selected) choose(selected); }}>
      {!model && <option value={value.model}>{value.model || '没有可用模型'}</option>}{models.map(m => <option key={m.model} value={m.model}>{m.name || m.model}</option>)}
     </select></SettingRow>
     <SettingRow label="推理强度" htmlFor="work-effort"><select id="work-effort" value={value.effort} onChange={e => setValue({...value, effort:e.target.value})}>
      {!model?.efforts.includes(value.effort) && <option value={value.effort}>{value.effort ? `${value.effort} · 当前不可用` : '由运行时决定'}</option>}{model?.efforts.filter(Boolean).map(e => <option key={e} value={e}>{effortLabels[e] ?? e}</option>)}
     </select></SettingRow>
     <SettingRow label="响应速度" htmlFor="work-tier"><select id="work-tier" value={value.serviceTier} onChange={e => setValue({...value, serviceTier:e.target.value})}>
      <option value="">标准</option>{value.serviceTier && !model?.serviceTiers.some(t => t.id === value.serviceTier) && <option value={value.serviceTier}>{value.serviceTier} · 当前不可用</option>}{model?.serviceTiers.filter(t => t.id).map(t => <option key={t.id} value={t.id}>{t.name || t.id}</option>)}
     </select></SettingRow>
    </>}
   </SettingGroup>
  </fieldset>
  {message && <p className="settings-note" role="status">{message}</p>}
  <div className="settings-footer"><button disabled={busy || loading} onClick={() => void load()}>{loading ? '正在加载…' : '刷新工作模型'}</button><button className="primary" disabled={busy || loading || !loaded || (manual && !model)} onClick={() => void save()}>{busy ? '正在保存…' : '保存工作设置'}</button></div>
 </>;
}
