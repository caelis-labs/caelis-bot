import type { Snapshot } from './backend/contract';

// Presentation follows backend capabilities; typing never grants permission to
// steer a run that is awaiting approval or recovering its connection.
export function composerAction(snapshot: Snapshot | null, quick: boolean, hasContent: boolean) {
 return !quick && snapshot?.canInterrupt && !hasContent ? 'stop' : 'send';
}

export type ChatActivity = 'thinking' | 'reviewing' | 'stopping';
export function chatActivity(snapshot: Snapshot | null): ChatActivity | null {
 if (!snapshot || snapshot.connection !== 'ready') return null;
 if (snapshot.phase === 'interrupting') return 'stopping';
 if (snapshot.approvals.some(p => p.status !== 'resolved')) return null;
 if (snapshot.phase !== 'sending' && snapshot.phase !== 'working') return null;
 if (snapshot.reviews.some(r => r.status === 'inProgress')) return 'reviewing';
 // A visible streaming answer already communicates progress. Empty started
 // items, completed commentary and tool work still need a waiting indicator.
 if (snapshot.items.some(i => i.turnKey === snapshot.currentTurn && i.kind === 'assistant' && i.text.trim() && (i.status === '' || i.status === 'inProgress'))) return null;
 return 'thinking';
}
