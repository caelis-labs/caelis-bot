import {workerTerminal} from './worker-output-read.ts';

// Unknown and waiting states remain current; presentation never retires work.
export function primaryWebTask<T extends {status:string}>(tasks:readonly T[]):T|undefined {
 return tasks.find(task=>!workerTerminal.has(task.status))??tasks[0];
}
