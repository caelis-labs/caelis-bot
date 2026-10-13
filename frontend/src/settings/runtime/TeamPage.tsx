import { useEffect, useState } from 'react';
import { useI18n } from '../../i18n';
import { runtimeSettingsClient } from './client';
import { messageOf } from './state';
import { TeamSettings } from './TeamSettings';
import type { RuntimeView } from './types';

// The Team is a first-level settings destination. Its state and model options
// come from the shared Caelis Host configuration projection, never Bot defaults.
export function TeamPage({ onConnections }: { onConnections: () => void }) {
 const { t } = useI18n();
 const [view, setView] = useState<RuntimeView | null>(null);
 const [error, setError] = useState('');
 const [loading, setLoading] = useState(true);
 const refresh = async () => {
  setLoading(true); setError('');
  try { setView(await runtimeSettingsClient.read()); }
  catch (e) { setError(messageOf(e, t('runtime.loadRuntimeFailed'))); throw e; }
  finally { setLoading(false); }
 };
 useEffect(() => { void refresh().catch(() => {}); }, []);
 return <section className="runtime-workspace runtime-team-page">
  <h1>{t('settings.team')}</h1><p className="settings-page-intro">{t('runtime.teamOptional')}</p>
  {loading && <p role="status">{t('runtime.loadingRuntime')}</p>}
  {error && <p role="alert" className="inline-error">{error}</p>}
  {view?.profile.runtime === 'caelis' ? <TeamSettings team={view.team} models={view.team.models} client={runtimeSettingsClient} onRefresh={refresh}/> : !loading && <div className="runtime-empty"><p>{t('runtime.teamNeedsCaelis')}</p><button onClick={onConnections}>{t('settings.connections')}</button></div>}
  {error && <button onClick={() => void refresh()}>{t('runtime.reload')}</button>}
 </section>;
}
