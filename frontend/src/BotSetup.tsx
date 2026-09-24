import { useEffect, useState, type FormEvent } from 'react';
import { backend, desktop } from './desktop';
import { RuntimeSettings } from './RuntimeSettings';
import type { BotInitialization, BotIntroduction, Snapshot } from './backend/contract';
import { useI18n } from './i18n';

// The backend journals one ordinary user message. The Bot maintains MEMORY.md;
// this form does not create a second identity settings store.
export function BotSetup({ onDone }: { onDone: () => void }) {
  const {t} = useI18n();
  const [state, setState] = useState<BotInitialization | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const value = await backend<BotInitialization>('BotInitialization');
        if (alive) setState(value);
      } catch (e) {
        if (alive) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed'));
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), 1500);
    return () => { alive = false; clearInterval(timer); };
  }, [t]);

  const openConversation = async () => {
    await desktop('OpenHistory');
    onDone();
  };
  const continueWhenReady = async () => {
    const snapshot = await backend<Snapshot>('Snapshot');
    if (snapshot.connection === 'ready') await openConversation();
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || !name.trim()) return;
    setBusy(true);
    setError('');
    try {
      const input: BotIntroduction = { name: name.trim(), description: description.trim() };
      setState(await backend<BotInitialization>('InitializeBot', input));
      setName('');
      setDescription('');
      await continueWhenReady();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('chat.setupSaveFailed'));
    } finally { setBusy(false); }
  };
  const retry = async () => {
    setBusy(true);
    setError('');
    try {
      setState(await backend<BotInitialization>('RetryBotIntroduction'));
      await continueWhenReady();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('chat.setupRetryFailed'));
    } finally { setBusy(false); }
  };

  if (!state) return <section className="runtime-welcome" role="status">{error || t('chat.setupPreparing')}</section>;
  if (state.status === 'rejected' || state.status === 'unknown') return <section className="bot-introduction">
    <h1>{t('chat.setupIncompleteTitle')}</h1>
    <p className="setup-lead" role="status">{state.message}</p>
    <div className="setup-actions">
      {state.status === 'rejected' && <button disabled={busy} onClick={() => void retry()}>{t('chat.setupRetry')}</button>}
      <button onClick={() => void openConversation()}>{t('chat.setupOpenChat')}</button>
    </div>
    {error && <p className="inline-error" role="alert">{error}</p>}
  </section>;
  if (!state.required) return <>
    {state.message && <p className="setup-introduction-status" role="status">{state.message}</p>}
    <RuntimeSettings onboarding onDone={onDone} />
  </>;

  return <section className="bot-introduction">
    <img className="setup-avatar" src="/icons/caelis-avatar.png" alt="" />
    <h1>{t('chat.setupIntroTitle')}</h1>
    <p className="setup-lead">{t('chat.setupSubtitle')}</p>
    <form onSubmit={event => void submit(event)}>
      <label htmlFor="bot-name">{t('chat.setupNameLabel')}
        <input id="bot-name" autoFocus required maxLength={80} autoComplete="off" value={name}
          disabled={busy} onChange={e => setName(e.target.value)} placeholder={t('chat.setupNamePlaceholder')} />
      </label>
      <label htmlFor="bot-description">{t('chat.setupDescLabel')} <span>{t('chat.setupOptional')}</span>
        <textarea id="bot-description" rows={3} maxLength={2000} value={description}
          disabled={busy} onChange={e => setDescription(e.target.value)} placeholder={t('chat.setupDescPlaceholder')} />
      </label>
      <p className="settings-note">{t('chat.setupHint')}</p>
      {error && <p className="inline-error" role="alert">{error}</p>}
      <div className="setup-end">
        <button className="primary" type="submit" disabled={busy || !name.trim()}>{busy ? t('common.loading') : t('chat.setupContinue')}</button>
      </div>
    </form>
  </section>;
}
