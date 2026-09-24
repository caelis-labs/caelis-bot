import { useEffect, useRef, useState } from 'react';
import { backend } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { ExecutionSettings as Preferences, ExecutionOptions } from './backend/contract';

// Read current preferences before saving so this mounted panel cannot restore
// a model that has since been changed in Runtime settings.
export function ExecutionSettings() {
 const [options, setOptions] = useState<ExecutionOptions | null>(null), [value, setValue] = useState(''), [saved, setSaved] = useState('');
 const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
 const working = useRef(false);
 const load = async () => {
  if (working.current) return;
  working.current = true; setBusy(true); setError('');
  try { const [prefs, opts] = await Promise.all([backend<Preferences>('ExecutionSettings'), backend<ExecutionOptions>('ExecutionOptions')]); setOptions(opts); setValue(prefs.approvalMode || opts.defaultApprovalMode); setSaved(prefs.approvalMode || opts.defaultApprovalMode); }
  catch { setError('暂时无法读取权限设置。'); }
  finally { working.current = false; setBusy(false); }
 };
 useEffect(() => { void load(); }, []);
 const save = async () => {
  if (working.current) return;
  working.current = true; setBusy(true); setError(''); setNotice('');
  try { const current = await backend<Preferences>('ExecutionSettings'); await backend('SaveExecutionSettings', { ...current, approvalMode: value }); setSaved(value); setNotice('权限设置已保存。'); }
  catch { setError('未能确认保存结果，请重新读取权限设置后核对。'); }
  finally { working.current = false; setBusy(false); }
 };
 const mode = options?.approvalModes.find(m => m.id === value);
 return <section className="execution-settings"><h1>权限</h1><p className="settings-intro">控制 Bot 执行操作时如何向你请求确认。</p>
  {options && <SettingGroup><SettingRow label="审批方式" htmlFor="execution-approval" description={<span className={mode?.dangerous ? 'permission-warning' : undefined}>{mode?.description || '当前运行时暂未提供此设置'}</span>}><select id="execution-approval" disabled={busy || !options.approvalModes.length} value={value} onChange={e => { setValue(e.target.value); setError(''); setNotice(''); }}>{!mode && <option value={value}>{value || '暂不可用'}</option>}{options.approvalModes.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></SettingRow></SettingGroup>}
  <p className="settings-note">工作会话遵循所用运行时的权限与审批规则。</p>
  {error && <p role="alert" className="inline-error">{error}</p>}{notice && <p role="status" className="settings-note">{notice}</p>}
  <div className="settings-footer settings-save-bar"><button disabled={busy} onClick={() => void load()}>{busy ? '正在处理…' : '重新读取'}</button><button className="primary" disabled={busy || !mode || value === saved || !!error} onClick={() => void save()}>保存更改</button></div>
 </section>;
}
