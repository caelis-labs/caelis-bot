import {createContext, useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {ChatCardField, Item, Receipt, Snapshot} from './backend/contract';
import {canSubmit} from './chat-presentation';
import {backend} from './desktop';

export type Outcome = 'sending' | 'accepted' | 'rejected' | 'unknown';
type Attempt = {outgoing: Item; outcome: Outcome};

export const ChatA2uiReply = createContext<string | undefined>(undefined);
export const ChatA2uiSubmission = createContext<ReturnType<typeof useChatA2uiSubmission> | null>(null);

function terminalOutcome(status: string): Outcome | undefined {
 if (status === 'accepted' || status === 'completed') return 'accepted';
 if (status === 'rejected') return 'rejected';
}

function originalOutcome(snapshot: Snapshot | null, id: string): Outcome | undefined {
 const item = snapshot?.items.find(item => item.kind === 'user' && item.requestId === id);
 if (snapshot?.lastReceipt.id === id) return terminalOutcome(snapshot.lastReceipt.outcome) ?? terminalOutcome(item?.status ?? '');
 return terminalOutcome(item?.status ?? '');
}

export async function chatReplyHash(value: string) {
 const hash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
 return Array.from(new Uint8Array(hash).slice(0, 16), byte => byte.toString(16).padStart(2, '0')).join('');
}

export function useChatA2uiSubmission(snapshot: Snapshot | null, refresh: () => Promise<unknown>, stage: (item: Item) => void) {
 const [attempts, setAttempts] = useState<ReadonlyMap<string, Attempt>>(() => new Map());
 const current = useRef(attempts);
 const available = canSubmit(snapshot);
 const pendingCards = useRef(new Set<string>());
 const save = useCallback((key: string, attempt: Attempt) => {
  current.current = new Map(current.current).set(key, attempt);
  setAttempts(current.current);
 }, []);
 useEffect(() => {
  for (const [key, attempt] of current.current) {
   if (terminalOutcome(attempt.outcome)) continue;
   const observed = originalOutcome(snapshot, attempt.outgoing.requestId!);
   if (observed) {
    save(key, {...attempt, outcome: observed});
    stage({...attempt.outgoing, status: observed});
   }
  }
 }, [snapshot, save, stage]);
 const outcome = useCallback((key: string, local = attempts.get(key)): Outcome | undefined => {
  if (local) return terminalOutcome(local.outcome) ?? originalOutcome(snapshot, local.outgoing.requestId!) ?? local.outcome;
  // An ordinary user item retains the original request ID even on history
  // replay. The prefix identifies its form without making a second journal.
  const item = snapshot?.items.filter(item => item.kind === 'user' && item.requestId?.startsWith(key)).at(-1);
  if (item) return terminalOutcome(item.status) ?? 'unknown';
  const last = snapshot?.lastReceipt;
  if (last?.id.startsWith(key)) return terminalOutcome(last.outcome) ?? 'unknown';
 }, [attempts, snapshot]);
 const dispatch = useCallback(async (key: string, id: string, text: string, fields?: ChatCardField[]) => {
  const outgoing: Item = {
   id: 'outgoing:' + id, requestId: id, kind: 'user', text, status: 'sending',
   seenAt: Date.now() * 1000, turnKey: '', details: '', activity: null, artifacts: [],
   chatCard: fields ? {fields} : undefined,
  };
  save(key, {outgoing, outcome: 'sending'});
  stage(outgoing);
  let result: Outcome = 'unknown';
  try {
   const receipt = fields
    ? await backend<Receipt>('SubmitChatCard', id, text, fields)
    : await backend<Receipt>('SubmitChatAnswers', id, text);
   if (receipt.id === id) result = terminalOutcome(receipt.outcome) ?? 'unknown';
  } catch {
   // A transport error says nothing about the effect of the original click.
  }
  const latest = current.current.get(key);
  if (latest?.outgoing.requestId === id) {
   result = terminalOutcome(latest.outcome) ?? result;
   save(key, {outgoing, outcome: result});
  }
  stage({...outgoing, status: result});
  void refresh().catch(() => {});
 }, [refresh, save, stage]);
 const send = useCallback(async (key: string, text: string) => {
  const prior = outcome(key, current.current.get(key));
  if (!available || prior && prior !== 'rejected') return;
  await dispatch(key, key + crypto.randomUUID(), text);
 }, [available, dispatch, outcome]);
 const cards = useCallback((prefix: string) => {
  const matches = new Map<string, Item>();
  for (const [id, attempt] of attempts) {
   if (id.startsWith(prefix) && attempt.outgoing.chatCard) {
    matches.set(id, {...attempt.outgoing, status: outcome(id) ?? attempt.outcome});
   }
  }
  for (const item of snapshot?.items ?? []) {
   if (!item.chatCard || !item.requestId.startsWith(prefix)) continue;
   const previous = matches.get(item.requestId);
   if (!previous || terminalOutcome(item.status)) matches.set(item.requestId, item);
  }
  return [...matches.values()].sort((a, b) => (a.seenAt ?? 0) - (b.seenAt ?? 0));
 }, [attempts, outcome, snapshot]);
 const sendCard = useCallback(async (prefix: string, source: string, text: string, fields: ChatCardField[]) => {
  if (!available || pendingCards.current.has(prefix)) return;
  pendingCards.current.add(prefix);
  try {
   const identity = JSON.stringify([source, text, [...fields].sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0)]);
   const key = prefix + await chatReplyHash(identity) + '-';
   // Only a proven rejection permits a new attempt. Its exact predecessor
   // remains in history and an unknown attempt is always fenced by its ID.
   for (let attempt = 0; ; attempt++) {
    const id = key + attempt.toString(36);
    const local = current.current.get(id);
    const observed = originalOutcome(snapshot, id);
    const recorded = snapshot?.items.find(item => item.kind === 'user' && item.requestId === id);
    const prior = observed ?? local?.outcome ?? (recorded ? 'unknown' : undefined);
    if (prior === 'rejected') continue;
    if (!prior) await dispatch(id, id, text, fields);
    return;
   }
  } finally {
   pendingCards.current.delete(prefix);
  }
 }, [available, dispatch, snapshot]);
 return useMemo(() => ({available, outcome, send, cards, sendCard}), [available, outcome, send, cards, sendCard]);
}
