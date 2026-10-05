import type { Snapshot } from './backend/contract';
import { activeReplyID, belongsToTurn, chatActivity } from './chat-presentation.ts';

export type PortraitClip='companion'|'think'|'scan'|'focus'|'waiting'|'listen'|'delight'|'dreaming';

// The same native facts drive both the waiting label and the portrait. Neither
// generated prose nor a character gesture grants authority or implies progress.
export function activityPortrait(s:Snapshot|null):PortraitClip {
 if(!s||s.connection!=='ready'||s.phase==='interrupting')return 'companion';
 if(s.approvals.some(p=>p.status!=='resolved'&&belongsToTurn(p,s.currentTurn))||['failed','unknown','attention'].includes(s.phase))return 'waiting';
 const activity=chatActivity(s);
 if(activity==='dreaming')return 'dreaming';
 if(activity==='reviewing')return 'focus';
 if(activeReplyID(s))return 'listen';
 if(activity==='tool'){
  if(['web','search','list'].includes(s.activity!.kind))return 'scan';
  if(['plan','compact'].includes(s.activity!.kind))return 'think';
  return 'focus';
 }
 return activity==='thinking'?'think':'companion';
}

// Completion is a live, observed edge, never a guess from text or history.
// Codex clears currentTurn on completion; the final item retains its turn key.
export function completedReply(previous:Snapshot|null,next:Snapshot):string|null {
 if(!previous||previous.connection!=='ready'||next.connection!=='ready'||
  !['sending','working'].includes(previous.phase)||next.phase!=='completed'||
  previous.quiet||next.quiet||previous.maintenance||next.maintenance||
  !previous.currentTurn||next.currentTurn&&next.currentTurn!==previous.currentTurn||
  next.approvals.some(p=>p.status!=='resolved'&&belongsToTurn(p,previous.currentTurn))||next.reviews.some(r=>r.status==='inProgress'&&belongsToTurn(r,previous.currentTurn)))return null;
 return next.items.slice().reverse().find(i=>i.kind==='assistant'&&i.turnKey===previous.currentTurn&&i.status==='completed'&&i.text.trim())?.id??null;
}

export function animatedReplyID(snapshot:Snapshot|null,completion:string|null):string|null {
 const streaming=activeReplyID(snapshot);
 if(streaming)return streaming;
 if(!completion||!snapshot||snapshot.connection!=='ready'||snapshot.phase!=='completed'||
  snapshot.quiet||snapshot.maintenance||snapshot.message||snapshot.approvals.some(p=>p.status!=='resolved'&&belongsToTurn(p,snapshot.currentTurn))||
  snapshot.reviews.some(r=>r.status==='inProgress'&&belongsToTurn(r,snapshot.currentTurn)))return null;
 return snapshot.items.find(i=>i.id===completion&&i.kind==='assistant'&&i.status==='completed'&&
  (!snapshot.currentTurn||i.turnKey===snapshot.currentTurn))?.id??null;
}
