// The copied endpoint always carries its actual port. A manually typed bare
// host uses the product default; explicit HTTP(S) URLs keep their own defaults.
export function botAddress(value:string,port=''):string {
 const text=value.trim(),explicit=/^https?:\/\//i.test(text);
 try {
  const url=new URL(explicit?text:`http://${text}`);
  if(port.trim())url.port=port.trim();
  else if(!explicit&&!url.port)url.port='9317';
  // Keep paths, credentials and queries for the host to reject, never silently
  // normalize an invalid/untrusted input into a different usable target.
  return url.toString().replace(/\/$/,'');
 }catch{return text;}
}
import type {Machine} from '../../backend/contract';

export function botProfileAt(machines:Machine[],address:string,port=''):Machine|undefined {
 const target=botAddress(address,port);
 return machines.find(machine=>machine.connectionType==='bot'&&botAddress(machine.address)===target);
}
