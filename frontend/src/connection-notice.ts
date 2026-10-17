import type { Snapshot } from './backend/contract';

type ConnectionState = Pick<Snapshot, 'connection'|'connectionIssue'|'message'|'introductionPending'|'phase'>;

// Presentation only. Send/stop/approval admission always uses the original
// snapshot. A first handshake or brief automatic recovery need not flash a
// manual reconnect card; actionable failures and uncertain work stay immediate.
export function connectionNoticeGrace(state: ConnectionState | null, windows: boolean): number {
 if (!windows || !state || state.phase==='unknown' || state.connection==='login') return 0;
 if (state.connectionIssue && !['connection','connection_lost'].includes(state.connectionIssue)) return 0;
 if (state.connection==='connecting' || state.connection==='offline'&&!state.message) return 750;
 return 0;
}

export function connectionNoticeKey(state: ConnectionState | null): string {
 if (!state || state.connection==='ready' || state.introductionPending) return '';
 return JSON.stringify([state.connection,state.connectionIssue,state.message,state.phase==='unknown']);
}
