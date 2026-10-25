import type {Machine} from '../../backend/contract';

// Readiness includes authorization and native admission, not network reachability.
export function remoteMachineStatus(machine:Machine,now=Date.now()):'online'|'offline'|'authorization'|'setup'|'unchecked' {
 if(machine.archived)return 'offline';
 if(machine.pairingStatus&&machine.pairingStatus!=='approved')return 'authorization';
 if(/revoked|authentication_required|original_peer_proof_required/.test(machine.issue||''))return 'authorization';
 if(machine.state==='setup'||/not_ready|incompatible/.test(machine.issue||''))return 'setup';
 const checked=machine.checkedAt||0;
 if(machine.state==='ready')return checked>0&&now-checked>=0&&now-checked<=60000?'online':'unchecked';
 return machine.state==='offline'?'offline':'unchecked';
}
