import {createContext, useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {Item, Receipt, Snapshot} from './backend/contract';
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
 const outcome = useCallback((key: string, repeatable = false, local = attempts.get(key)): Outcome | undefined => {
  if (local) return terminalOutcome(local.outcome) ?? originalOutcome(snapshot, local.outgoing.requestId) ?? local.outcome;
  if (repeatable) return;
  // Saved root Forms had a single submission, with its origin in the request ID.
  const item = snapshot?.items.filter(item => item.kind === 'user' && item.requestId?.startsWith(key)).at(-1);
  if (item) return terminalOutcome(item.status) ?? 'unknown';
  const last = snapshot?.lastReceipt;
  if (last?.id.startsWith(key)) return terminalOutcome(last.outcome) ?? 'unknown';
 }, [attempts, snapshot]);
 const dispatch = useCallback(async (key: string, id: string, text: string) => {
  const outgoing: Item = {
   id: 'outgoing:' + id, requestId: id, kind: 'user', text, status: 'sending',
   seenAt: Date.now() * 1000, turnKey: '', details: '', activity: null, artifacts: [],
  };
  save(key, {outgoing, outcome: 'sending'});
  stage(outgoing);
  let result: Outcome = 'unknown';
  try {
   const receipt = await backend<Receipt>('SubmitChatAnswers', id, text);
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
 const send = useCallback(async (key: string, text: string, repeatable = false) => {
  const prior = outcome(key, repeatable, current.current.get(key));
  if (!available || prior === 'sending' || prior === 'unknown' || (!repeatable && prior === 'accepted')) return;
  const request = crypto.randomUUID();
  await dispatch(key, repeatable ? request : key + request, text);
 }, [available, dispatch, outcome]);
 return useMemo(() => ({available, outcome, send}), [available, outcome, send]);
}
