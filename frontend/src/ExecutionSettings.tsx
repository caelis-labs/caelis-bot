import { useEffect, useRef, useState } from 'react';
import { backend } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { ExecutionSettings as Preferences, ExecutionOptions } from './backend/contract';
import { useI18n } from './i18n';

// Read current preferences before saving so this mounted panel cannot restore
// a model that has since been changed in Runtime settings.
export function ExecutionSettings() {
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
 const mode = options?.approvalModes.find(m => m.id === value);
 return <section className="execution-settings"><h1>{t('settings.execution')}</h1><p className="settings-intro">{t('settings.executionIntro')}</p>
  {options && <SettingGroup><SettingRow label={t('settings.executionApprovalMode')} htmlFor="execution-approval" description={<span className={mode?.dangerous ? 'permission-warning' : undefined}>{mode?.description || t('settings.executionModeUnavailable')}</span>}><select id="execution-approval" disabled={busy || !options.approvalModes.length} value={value} onChange={e => { setValue(e.target.value); setError(''); setNotice(''); }}>{!mode && <option value={value}>{value || t('settings.unavailable')}</option>}{options.approvalModes.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></SettingRow></SettingGroup>}
  <p className="settings-note">{t('settings.executionNote')}</p>
  {error && <p role="alert" className="inline-error">{error}</p>}{notice && <p role="status" className="settings-note">{notice}</p>}
  <div className="settings-footer settings-save-bar"><button disabled={busy} onClick={() => void load()}>{busy ? t('common.loading') : t('settings.executionReload')}</button><button className="primary" disabled={busy || !mode || value === saved || !!error} onClick={() => void save()}>{t('settings.executionSave')}</button></div>
 </section>;
}
