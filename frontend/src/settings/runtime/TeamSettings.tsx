import { useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { RuntimeSettingsClient, TeamChange, TeamRole, TeamState } from './types';
import { messageOf, selectionSummary } from './state';
import { ModelPicker } from './ModelPicker';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';
import type { MessageKey } from '../../i18n/catalogs';
import { SettingsChevron } from '../../SettingsIcons';
import './team.css';

const cardRoles = ['breeze', 'orbit', 'zenith', 'tool-search', 'guardian'] as const;
type CardRole = typeof cardRoles[number];

function RoleIcon({ id }: { id: CardRole }) {
 const paths: Record<CardRole, React.ReactNode> = {
  breeze: <><path d="M3 9h12a3 3 0 1 0-3-3"/><path d="M2 13h17a3 3 0 1 1-3 3"/><path d="M5 17h5"/></>,
  orbit: <><circle cx="12" cy="12" r="4"/><ellipse cx="12" cy="12" rx="10" ry="5" transform="rotate(-28 12 12)"/></>,
  zenith: <><path d="m2 20 8-14 4 7 2-3 6 10H2Z"/><path d="m8 10 2 2 2-2"/></>,
  'tool-search': <><circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5M8 10.5h5m-2.5-2.5v5"/></>,
  guardian: <><path d="M12 2 4 5v6c0 5 3.5 8.5 8 11 4.5-2.5 8-6 8-11V5l-8-3Z"/><path d="m8.5 12 2.3 2.3 4.8-4.8"/></>,
 };
 return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round">{paths[id]}</svg>;
}

export function TeamSettings({ team, models, client, onRefresh }: { team: TeamState; models: ModelOption[]; client: Pick<RuntimeSettingsClient, 'changeTeam'>; onRefresh: () => Promise<void> }) {
 const { t } = useI18n();
 const roleText = (id: string, part: 'name' | 'description') => t(`runtime.role_${id.replaceAll('-', '_')}_${part}` as MessageKey);
 const roleEmpty = (role: TeamRole) => role.id === 'guardian-screen' ? t('runtime.judgmentModelRequired') : t('runtime.roleNoCompatibleModel');
 const modelAnchor = useRef<HTMLElement | null>(null);
 const [editing, setEditing] = useState<TeamRole | null>(null);
 const [panel, setPanel] = useState('');
 const [name, setName] = useState('');
 const [error, setError] = useState('');
 const [busy, setBusy] = useState(false);
 const [blocked, setBlocked] = useState(false);
 const working = useRef(false);
 const openPanel = (kind: string) => { setPanel(kind); setName(''); setError(''); setBlocked(false); };
 const closePanel = () => { setPanel(''); setEditing(null); setError(''); setBlocked(false); };
 const openRole = (role: TeamRole, anchor: HTMLElement) => { modelAnchor.current = anchor; setEditing(role); setError(''); };
 const write = async (change: TeamChange) => {
  await client.changeTeam(change, team.revision);
  // A committed mutation must never be retried because its readback failed.
  closePanel();
  try { await onRefresh(); }
  catch { setBlocked(true); setError(t('runtime.savedRefreshFailed')); throw new Error(t('runtime.savedRefreshFailed')); }
 };
 const submit = async (change: TeamChange) => {
  if (working.current || blocked) return;
  working.current = true; setBusy(true); setError('');
  try { await write(change); }
  catch (e) { setError(messageOf(e, t('runtime.actionFailed'))); if (e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); }
  finally { working.current = false; setBusy(false); }
 };
 const summary = (role: TeamRole) => !role.selection.model
  ? t('runtime.unbound')
  : selectionSummary(role.selection, models, t).name;
 const guardian = team.roles.find(role => role.id === 'guardian');
 const guardianScreen = team.roles.find(role => role.id === 'guardian-screen');
 const bindingRow = (role: TeamRole) => <div className="runtime-team-binding" key={role.id}>
  <div className="runtime-team-binding-copy"><strong>{roleText(role.id, 'name')}</strong><p>{roleText(role.id, 'description')}</p>{role.problem && <p role="alert" className="inline-error">{role.problem}</p>}</div>
  <button className="runtime-team-bind-button" aria-label={t('runtime.configureRole', { id: roleText(role.id, 'name') })} onClick={event => openRole(role, event.currentTarget)}><span title={summary(role)}>{summary(role)}</span><SettingsChevron/></button>
 </div>;

 return <section className="runtime-team-cards">
  {error && !panel && !editing && <div role="alert" className="inline-error"><p>{error}</p><button onClick={() => void onRefresh().then(() => { setError(''); setBlocked(false); }).catch(() => {})}>{t('runtime.readLatestConfig')}</button></div>}
  {!team.available ? <p className="settings-note">{team.reason || t('runtime.teamNeedsCaelis')}</p> : <>
   <div className="runtime-team-cards-heading"><h2>{t('runtime.agentTeam')}</h2><button className="text-action" onClick={() => openPanel('sets')}>{t('runtime.teamScheme')}</button></div>
   {team.activeSet && <p className="runtime-team-plan-status">{t('runtime.teamCurrentScheme', { name: team.activeSet })}</p>}
   <div className="runtime-team-card-grid">{cardRoles.map(id => {
    const role = team.roles.find(item => item.id === id);
    return <button key={id} type="button" className="runtime-team-card" disabled={!role} onClick={event => {
     if (!role) return;
     if (id === 'guardian') openPanel('guardian');
     else openRole(role, event.currentTarget);
    }}>
     <span className="runtime-team-icon"><RoleIcon id={id}/></span>
     <span className="runtime-team-card-copy"><strong>{roleText(id, 'name')}</strong><small className="runtime-team-card-id">{id}</small><span className="runtime-team-card-description">{roleText(id, 'description')}</span><span className="runtime-team-card-model" title={role ? summary(role) : undefined}>{role ? summary(role) : t('runtime.roleNoCompatibleModel')}</span>{role?.problem && <span className="runtime-team-card-problem">{role.problem}</span>}</span>
     <span className="runtime-team-card-arrow" aria-hidden="true">›</span>
    </button>;
   })}</div>
   {models.length === 0 && <p className="settings-note">{t('runtime.teamNeedsCaelis')}</p>}
  </>}
  {panel === 'guardian' && guardian && <SettingsDialog title={t('runtime.configureRole', { id: roleText('guardian', 'name') })} description={roleText('guardian', 'description')} blocked={!!editing} onClose={closePanel} className="runtime-team-role-dialog">
   {bindingRow(guardian)}
   {guardianScreen && bindingRow(guardianScreen)}
  </SettingsDialog>}
  {editing && <ModelPicker key={editing.id} anchor={modelAnchor.current} title={t('runtime.configureRole', { id: roleText(editing.id, 'name') })} description={roleText(editing.id, 'description')} emptyMessage={roleEmpty(editing)} value={editing.selection} models={models.filter(model => editing.modelIds?.includes(model.model))} onSave={selection => write({ action: 'bind', id: editing.id, selection })} onClose={() => setEditing(null)} onReload={onRefresh} onReset={!editing.inherited ? () => write({ action: 'reset', id: editing.id }) : undefined}/>}
  {panel && panel !== 'guardian' && <SettingsDialog title={panel === 'save-set' ? t('runtime.saveTeamScheme') : t('runtime.teamScheme')} description={panel === 'sets' ? t('runtime.teamSchemeDescription') : undefined} busy={busy} onClose={closePanel} className="runtime-team-plan-dialog">
   {panel === 'sets' ? <><div className="runtime-team-plan-list">{team.sets.map(set => <div key={set.name}><div><strong>{set.name}</strong>{set.problem && <p className="inline-error">{set.problem}</p>}</div><button disabled={busy || blocked || !set.available} onClick={() => void submit({ action: 'apply-set', name: set.name })}>{t('runtime.apply')}</button><button disabled={busy} onClick={() => { setName(set.name); setPanel('delete-set'); }}>{t('runtime.delete')}</button></div>)}</div><button className="text-action" disabled={busy} onClick={() => openPanel('save-set')}>{t('runtime.saveCurrentAsScheme')}</button></> : panel === 'delete-set' ? <><p>{t('runtime.deleteSchemeDescription', { name })}</p><div className="setup-end"><button disabled={busy} onClick={closePanel}>{t('common.cancel')}</button><button disabled={busy || blocked} onClick={() => void submit({ action: 'delete-set', name })}>{t('runtime.delete')}</button></div></> : <form onSubmit={event => { event.preventDefault(); void submit({ action: 'save-set', name: name.trim() }); }}><label>{t('runtime.schemeName')}<small>{t('runtime.teamIdentifierHint')}</small><input required maxLength={32} pattern="[a-z][a-z0-9-]*" value={name} disabled={busy} onChange={event => setName(event.target.value)} placeholder={t('runtime.schemeNamePlaceholder')}/></label><div className="setup-end"><button type="button" disabled={busy} onClick={closePanel}>{t('common.cancel')}</button><button className="primary" disabled={busy || blocked || !/^[a-z][a-z0-9-]{0,31}$/.test(name.trim())}>{busy ? t('runtime.saving') : t('common.save')}</button></div></form>}
   {error && <div role="alert" className="inline-error"><p>{error}</p><button disabled={busy} onClick={() => { closePanel(); void onRefresh().catch(() => {}); }}>{t('runtime.readLatestConfig')}</button></div>}
  </SettingsDialog>}
 </section>;
}
