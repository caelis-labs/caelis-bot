import { useEffect, useState, type FormEvent } from 'react';
import { backend, desktop } from './desktop';
import { RuntimeSettings } from './RuntimeSettings';
import type { BotInitialization, BotIntroduction, Snapshot } from './backend/contract';

// The backend journals one ordinary user message. The Bot maintains MEMORY.md;
// this form does not create a second identity settings store.
export function BotSetup({ onDone }: { onDone: () => void }) {
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
        if (alive) setError(e instanceof Error ? e.message : '暂时无法读取初始化状态');
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), 1500);
    return () => { alive = false; clearInterval(timer); };
  }, []);

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
      setError(e instanceof Error ? e.message : '介绍尚未保存，请重试');
    } finally { setBusy(false); }
  };
  const retry = async () => {
    setBusy(true);
    setError('');
    try {
      setState(await backend<BotInitialization>('RetryBotIntroduction'));
      await continueWhenReady();
    } catch (e) {
      setError(e instanceof Error ? e.message : '暂时无法重试');
    } finally { setBusy(false); }
  };

  if (!state) return <section className="runtime-welcome" role="status">{error || '正在准备…'}</section>;
  if (state.status === 'rejected' || state.status === 'unknown') return <section className="bot-introduction">
    <h1>介绍尚未发送完成</h1>
    <p className="setup-lead" role="status">{state.message}</p>
    <div className="setup-actions">
      {state.status === 'rejected' && <button disabled={busy} onClick={() => void retry()}>重试发送</button>}
      <button onClick={() => void openConversation()}>打开对话</button>
    </div>
    {error && <p className="inline-error" role="alert">{error}</p>}
  </section>;
  if (!state.required) return <>
    {state.message && <p className="setup-introduction-status" role="status">{state.message}</p>}
    <RuntimeSettings onboarding onDone={onDone} />
  </>;

  return <section className="bot-introduction">
    <img className="setup-avatar" src="/icons/caelis-avatar.png" alt="" />
    <h1>认识你的 Bot</h1>
    <p className="setup-lead">给它一个名字，开始你们的对话。</p>
    <form onSubmit={event => void submit(event)}>
      <label htmlFor="bot-name">名字
        <input id="bot-name" autoFocus required maxLength={80} autoComplete="off" value={name}
          disabled={busy} onChange={e => setName(e.target.value)} placeholder="你想怎么称呼它" />
      </label>
      <label htmlFor="bot-description">描述 <span>可选</span>
        <textarea id="bot-description" rows={3} maxLength={2000} value={description}
          disabled={busy} onChange={e => setDescription(e.target.value)} placeholder="比如：说话简洁，喜欢分享有趣的发现" />
      </label>
      <p className="settings-note">这些话会作为你的第一条消息发给 Bot。</p>
      {error && <p className="inline-error" role="alert">{error}</p>}
      <div className="setup-end">
        <button className="primary" type="submit" disabled={busy || !name.trim()}>{busy ? '正在保存…' : '继续'}</button>
      </div>
    </form>
  </section>;
}
