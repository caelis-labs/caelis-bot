import type {WorkerPeerSource} from '../../backend/contract';

// Names are supplied by peers and may repeat. Keep a fingerprint from the
// original grant visible wherever the user chooses or revokes that authority.
export function peerSourceLabel(source:WorkerPeerSource,sources:WorkerPeerSource[],fallback:string):string {
 let length=Math.min(8,source.id.length);
 while(length<source.id.length&&sources.some(other=>other.id!==source.id&&other.id.startsWith(source.id.slice(0,length))))length++;
 return `${source.name||fallback} · ${source.id.slice(0,length)}`;
}
