import type { Snapshot } from './backend/contract';

// Presentation follows backend capabilities; typing never grants permission to
// steer a run that is awaiting approval or recovering its connection.
export function composerAction(snapshot: Snapshot | null, quick: boolean, hasContent: boolean) {
 return !quick && snapshot?.canInterrupt && !hasContent ? 'stop' : 'send';
}

export type ChatActivity = 'thinking' | 'reviewing' | 'stopping';
export function activeReplyID(snapshot: Snapshot | null): string | null {
 if(!snapshot||snapshot.connection!=='ready'||!['sending','working'].includes(snapshot.phase)||snapshot.approvals.some(p=>p.status!=='resolved')||snapshot.reviews.some(r=>r.status==='inProgress'))return null;
 for(let n=snapshot.items.length-1;n>=0;n--){
  const i=snapshot.items[n];
  if(i.turnKey===snapshot.currentTurn&&i.kind==='assistant'&&i.text.trim()&&(i.status===''||i.status==='inProgress'))return i.id;
 }
 return null;
}
export function chatActivity(snapshot: Snapshot | null): ChatActivity | null {
 if (!snapshot || snapshot.connection !== 'ready') return null;
 if (snapshot.phase === 'interrupting') return 'stopping';
 if (snapshot.approvals.some(p => p.status !== 'resolved')) return null;
 if (snapshot.phase !== 'sending' && snapshot.phase !== 'working') return null;
 if (snapshot.reviews.some(r => r.status === 'inProgress')) return 'reviewing';
 // A visible streaming answer already communicates progress. Empty started
 // items, completed commentary and tool work still need a waiting indicator.
 if (activeReplyID(snapshot)) return null;
 return 'thinking';
}
