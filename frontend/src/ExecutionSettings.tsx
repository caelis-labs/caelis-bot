import { useEffect, useRef, useState } from 'react';
import { backend } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { ExecutionSettings as Preferences, ExecutionOptions } from './backend/contract';
import { useI18n } from './i18n';
import {executionMode} from './execution-presentation';

// Read current preferences before saving so this mounted panel cannot restore
// a model that has since been changed in Runtime settings.
export function ExecutionSettings({embedded=false}:{embedded?:boolean}) {
 const {t}=useI18n();
 const [options, setOptions] = useState<ExecutionOptions | null>(null), [value, setValue] = useState(''), [saved, setSaved] = useState('');
 const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
 const working = useRef(false);
 const load = async () => {
  if (working.current) return;
  working.current = true; setBusy(true); setError('');
  try { const [prefs, opts] = await Promise.all([backend<Preferences>('ExecutionSettings'), backend<ExecutionOptions>('ExecutionOptions')]); setOptions(opts); setValue(prefs.approvalMode || opts.defaultApprovalMode); setSaved(prefs.approvalMode || opts.defaultApprovalMode); }
  catch { setError(t('settings.executionLoadFailed')); }
  finally { working.current = false; setBusy(false); }
 };
 useEffect(() => { void load(); }, []);
 const save = async () => {
  if (working.current) return;
  working.current = true; setBusy(true); setError(''); setNotice('');
  try { const current = await backend<Preferences>('ExecutionSettings'); await backend('SaveExecutionSettings', { ...current, approvalMode: value }); setSaved(value); setNotice(t('settings.executionSaved')); }
  catch { setError(t('settings.executionSaveFailed')); }
  finally { working.current = false; setBusy(false); }
 };
 const modes = options?.approvalModes.map(mode=>executionMode(mode,t))??[];
 const mode = modes.find(m => m.id === value);
 return <section className="execution-settings">{!embedded&&<h1>{t('settings.execution')}</h1>}
  {options && <SettingGroup title={t('settings.execution')}><SettingRow label={t('settings.executionApprovalMode')} htmlFor="execution-approval" description={<span className={mode?.dangerous ? 'permission-warning' : undefined}>{mode?.description || t('settings.executionModeUnavailable')}</span>}>{modes.length===1&&mode?<span className="permission-badge">{mode.name}</span>:<select id="execution-approval" disabled={busy || !modes.length} value={value} onChange={e => { setValue(e.target.value); setError(''); setNotice(''); }}>{!mode && <option value={value}>{value || t('settings.unavailable')}</option>}{modes.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select>}</SettingRow></SettingGroup>}

  {error && <p role="alert" className="inline-error">{error}</p>}{notice && <p role="status" className="settings-note">{notice}</p>}
  {(!options||modes.length>1||error)&&<div className="settings-footer"><button disabled={busy} onClick={() => void load()}>{busy ? t('common.loading') : t('settings.executionReload')}</button><button className="primary" disabled={busy || !mode || value === saved || !!error} onClick={() => void save()}>{t('settings.executionSave')}</button></div>}
 </section>;
}
