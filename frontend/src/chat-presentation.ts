import type { Item, Snapshot } from './backend/contract';

export function canSubmit(snapshot: Snapshot | null) {
 return !!(snapshot?.canSend || snapshot?.canSteer);
}

export function withOutgoing(items: Item[], outgoing: Item[]): Item[] {
 const known = new Set(items.map(item => item.requestId).filter(Boolean));
 return [...items, ...outgoing.filter(item => !known.has(item.requestId))];
}

// Older stored projections can still say inProgress after their turn ended.
// Only identify an orphaned assistant item; do not relabel the whole turn.
export function incompleteAssistant(item: Item, snapshot: Snapshot | null): boolean {
 if(item.kind !== 'assistant' || !snapshot) return false;
 if(item.status === 'incomplete') return true;
 return item.status === 'inProgress' && !!item.turnKey && item.turnKey !== snapshot.currentTurn &&
  (!!snapshot.currentTurn || ['completed','failed','interrupted','idle'].includes(snapshot.phase));
}

// Presentation follows backend capabilities; typing never grants permission to
// steer a run that is awaiting approval or recovering its connection.
export function composerAction(snapshot: Snapshot | null, quick: boolean, hasContent: boolean) {
 return !quick && snapshot?.canInterrupt && !hasContent ? 'stop' : 'send';
}

export type ChatActivity = 'thinking' | 'stopping' | 'tool' | 'dreaming';
// Older adapter snapshots may omit turnKey. Native Codex projections always
// provide it, so a Worker decision cannot preempt the resident reply.
export function belongsToTurn(value: {turnKey?: string}, turn: string) { return !value.turnKey || value.turnKey === turn; }

// Track arrivals separately from execution state. A reply may already be
// completed on its first poll; opening/recovering history must not replay it.
export function liveReplyIDs(previous: Snapshot | null, next: Snapshot, live: ReadonlySet<string>, latestOnly=false): Set<string> {
 const result = new Set<string>();
 if (!previous || previous.connection !== 'ready' || next.connection !== 'ready') return result;
 const before = new Map(previous.items.map(item => [item.id, item]));
 const tail = previous.items.at(-1);
 // Native acceptance can replace an optimistic user item between polls.
 // Match its exact request identity, just as the transcript does.
 const anchor = tail ? next.items.findIndex(item => item.id === tail.id ||
  tail.kind === 'user' && !!tail.requestId && item.kind === 'user' && item.requestId === tail.requestId) : -1;
 for (const [index,item] of next.items.entries()) {
  if (item.kind !== 'assistant') continue;
  const old = before.get(item.id);
  // Pet snapshots intentionally retain only the latest item of each kind.
  const appended = !old && (latestOnly || !tail || anchor >= 0 && index > anchor);
  const extended = old && (old.status === '' || old.status === 'inProgress' || item.status === 'inProgress') && item.text.length > old.text.length && item.text.startsWith(old.text);
  if (live.has(item.id) || appended || extended) result.add(item.id);
 }
 return result;
}

export function activeReplyID(snapshot: Snapshot | null): string | null {
 if(!snapshot||snapshot.quiet||snapshot.connection!=='ready'||!['sending','working'].includes(snapshot.phase)||snapshot.approvals.some(p=>p.status!=='resolved'&&belongsToTurn(p,snapshot.currentTurn)))return null;
 for(let n=snapshot.items.length-1;n>=0;n--){
  const i=snapshot.items[n];
  if(i.turnKey===snapshot.currentTurn&&i.kind==='assistant'&&i.text.trim()&&i.status==='inProgress')return i.id;
 }
 return null;
}
export function chatActivity(snapshot: Snapshot | null): ChatActivity | null {
 if (!snapshot || snapshot.connection !== 'ready') return null;
 if (snapshot.phase === 'interrupting') return 'stopping';
 if (snapshot.approvals.some(p => p.status !== 'resolved' && belongsToTurn(p,snapshot.currentTurn))) return null;
 if (snapshot.maintenance === 'dreaming' && snapshot.phase === 'working' && !snapshot.message) return 'dreaming';
 if (snapshot.quiet) return null;
 if (snapshot.phase !== 'sending' && snapshot.phase !== 'working') return null;
 // A visible streaming answer already communicates progress. Empty started
 // items, completed commentary and tool work still need a waiting indicator.
 if (activeReplyID(snapshot)) return null;
 if (snapshot.activity) return 'tool';
 return 'thinking';
}
