import { useEffect, useState } from 'react';
import { useI18n } from '../../i18n';
import { backend } from '../../desktop';
import { messageOf } from './state';
import { TeamSettings } from './TeamSettings';
import type { RuntimeConfiguration, RuntimeMutationResult } from '../../backend/contract';
import { ConfigurationError } from './client';
import type { RuntimeSettingsClient } from './types';

// Opened from Caelis Worker settings. Its Host profile is resolved natively
// from that Worker, including when Bot conversation uses Codex.
export function TeamPage({ onConnections }: { onConnections: () => void }) {
 const { t } = useI18n();
 const [view, setView] = useState<RuntimeConfiguration | null>(null);
 const [error, setError] = useState('');
 const [loading, setLoading] = useState(true);
 const canConfigure = Boolean(view?.team.available);
 const refresh = async () => {
  setLoading(true); setError('');
  try { setView(await backend<RuntimeConfiguration>('WorkerTeamConfiguration')); }
  catch (e) { setError(messageOf(e, t('runtime.loadRuntimeFailed'))); throw e; }
  finally { setLoading(false); }
 };
 const client: Pick<RuntimeSettingsClient, 'changeTeam'> = {changeTeam: async (change, revision) => {
  const receipt = await backend<RuntimeMutationResult>('ChangeWorkerTeamConfiguration', {...change, expectedRevision: revision});
  if (receipt.outcome !== 'committed') throw new ConfigurationError(receipt);
 }};
 useEffect(() => { void refresh().catch(() => {}); }, []);
 return <section className="runtime-workspace runtime-team-page">
  <button className="text-action" onClick={onConnections}>← {t('runtime.manageConnections')}</button>
  <h1>{t('settings.team')}</h1>{view && <p className="settings-page-intro">{canConfigure ? t('runtime.teamOptional') : view.team.reason || t('runtime.teamNeedsCaelis')}</p>}
  {loading && <p role="status">{t('runtime.loadingRuntime')}</p>}
  {error && <p role="alert" className="inline-error">{error}</p>}
  {canConfigure && view ? <TeamSettings team={view.team} models={view.team.models} client={client} onRefresh={refresh}/> : view && !loading && <div className="runtime-empty"><button onClick={onConnections}>{t('runtime.manageConnections')}</button></div>}
  {error && <button onClick={() => void refresh()}>{t('runtime.reload')}</button>}
 </section>;
}
