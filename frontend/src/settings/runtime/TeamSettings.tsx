import { useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { RuntimeSettingsClient, TeamChange, TeamRole, TeamState } from './types';
import { messageOf } from './state';
import { ModelPicker, ModelSummary } from './ModelPicker';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';

export function TeamSettings({ team, models, client, onRefresh }: { team: TeamState; models: ModelOption[]; client: RuntimeSettingsClient; onRefresh: () => Promise<void> }) {
 const { t } = useI18n();
 const [editing, setEditing] = useState<TeamRole | null>(null), [panel, setPanel] = useState(''), [name, setName] = useState(''), [description, setDescription] = useState('');
 const [error, setError] = useState(''), [busy, setBusy] = useState(false), [blocked, setBlocked] = useState(false), working = useRef(false);
 const open = (kind: string) => { setPanel(kind); setName(''); setDescription(''); setError(''); setBlocked(false); };
 const close = () => { setPanel(''); setEditing(null); setError(''); setBlocked(false); };
 const write = async (change: TeamChange) => {
  await client.changeTeam(change, team.revision);
  // A refresh failure must not turn an already committed write into a retry.
  close(); await onRefresh();
 };
 const submit = async (change: TeamChange) => {
  if (working.current || blocked) return;
  working.current = true; setBusy(true); setError('');
  try { await write(change); } catch (e) { setError(messageOf(e, t('runtime.actionFailed'))); if (e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); } finally { working.current = false; setBusy(false); }
 };
 const row = (role: TeamRole) => <div className="runtime-setting-row" key={role.id}><div><strong title={role.description}>{role.id}</strong>{!role.modelIds && <p className="settings-note">{t('runtime.updateCaelisForRoleModels')}</p>}{role.problem && <p className="inline-error">{role.problem}</p>}</div><div className="runtime-row-actions"><ModelSummary unbound value={role.selection} models={models} label={` ${role.id}`} onClick={() => setEditing(role)}/>{role.custom && <button className="text-action" onClick={() => { setEditing(role); open('delete-role'); }}>{t('runtime.delete')}</button>}</div></div>;
 return <section className="runtime-team">
  <div className="runtime-section-title"><h2>{t('runtime.agentTeam')}</h2>{team.available && <div><button className="text-action" onClick={() => open('sets')}>{team.activeSet || t('runtime.teamScheme')}</button><button aria-label={t('runtime.addAgent')} onClick={() => open('create')}>{t('runtime.add')}</button></div>}</div>
  {!team.available ? <p className="settings-note">{team.reason}</p> : <>
   {team.roles.filter(r => r.id !== 'self' && !r.system).map(row)}
   <details className="runtime-system-agents"><summary>{t('runtime.systemAgents')}</summary><div className="runtime-setting-row"><strong>self</strong><span>{t('runtime.followMainModel')}</span></div>{team.roles.filter(r => r.system && r.id !== 'self').map(row)}</details>
  </>}
  {editing && !panel && <ModelPicker requireEffort title={t('runtime.configureRole', { id: editing.id })} description={editing.description || undefined} value={editing.selection} models={models.filter(m => editing.modelIds?.includes(m.model))} onSave={selection => write({ action: 'bind', id: editing.id, selection })} onClose={() => setEditing(null)} onReload={onRefresh} onReset={!editing.inherited ? () => write({ action: 'reset', id: editing.id }) : undefined}/>}
  {panel && <SettingsDialog title={panel === 'create' ? t('runtime.addAgent') : panel === 'save-set' ? t('runtime.saveTeamScheme') : panel === 'delete-role' ? t('runtime.deleteRolePrompt', { id: editing?.id ?? '' }) : t('runtime.teamScheme')} description={panel === 'sets' ? t('runtime.teamSchemeDescription') : undefined} busy={busy} onClose={close}>
   {panel === 'sets' ? <><div className="runtime-scheme-list">{team.sets.map(set => <div key={set.name}><div><strong>{set.name}</strong>{set.problem && <p className="inline-error">{set.problem}</p>}</div><button disabled={busy || blocked || !set.available} onClick={() => void submit({ action: 'apply-set', name: set.name })}>{t('runtime.apply')}</button><button disabled={busy} onClick={() => { setName(set.name); setPanel('delete-set'); }}>{t('runtime.delete')}</button></div>)}</div><button className="text-action" disabled={busy} onClick={() => open('save-set')}>{t('runtime.saveCurrentAsScheme')}</button></> : panel === 'delete-role' || panel === 'delete-set' ? <><p>{panel === 'delete-role' ? t('runtime.deleteRoleDescription') : t('runtime.deleteSchemeDescription', { name })}</p><div className="setup-end"><button disabled={busy} onClick={close}>{t('common.cancel')}</button><button disabled={busy || blocked} onClick={() => void submit(panel === 'delete-role' ? { action: 'delete-role', id: editing!.id } : { action: 'delete-set', name })}>{t('runtime.delete')}</button></div></> : <form onSubmit={e => { e.preventDefault(); void submit(panel === 'create' ? { action: 'create-role', id: name.trim(), description: description.trim() } : { action: 'save-set', name: name.trim() }); }}>
    <label>{panel === 'create' ? t('runtime.roleName') : t('runtime.schemeName')}<input required pattern={panel === 'create' ? '[a-z][a-z0-9-]*' : undefined} value={name} disabled={busy} onChange={e => setName(e.target.value)} placeholder={panel === 'create' ? t('runtime.roleNamePlaceholder') : t('runtime.schemeNamePlaceholder')}/></label>
    {panel === 'create' && <label>{t('runtime.roleResponsibilities')}<textarea required value={description} disabled={busy} onChange={e => setDescription(e.target.value)} placeholder={t('runtime.roleResponsibilitiesPlaceholder')}/></label>}
    <div className="setup-end"><button type="button" disabled={busy} onClick={close}>{t('common.cancel')}</button><button className="primary" disabled={busy || blocked || !name.trim() || panel === 'create' && !description.trim()}>{busy ? t('runtime.saving') : t('common.save')}</button></div>
   </form>}
   {error && <div role="alert" className="inline-error"><p>{error}</p><button disabled={busy} onClick={() => { close(); void onRefresh().catch(()=>{}); }}>{t('runtime.readLatestConfig')}</button></div>}
  </SettingsDialog>}
 </section>;
}
