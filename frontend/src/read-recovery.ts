// Only use for reads. A failed effect must reconcile its original receipt.
export async function recoverRead<T>(read:()=>Promise<T>, signal?:AbortSignal,
 pause:(ms:number)=>Promise<void>=ms=>new Promise(resolve=>setTimeout(resolve,ms))):Promise<T> {
 for(let attempt=0;attempt<3;attempt++) {
  signal?.throwIfAborted();
  try {const value=await read();signal?.throwIfAborted();return value;}
  catch {
   signal?.throwIfAborted();
   if(attempt===2)throw new Error('read_unavailable');
   await pause(attempt===0?300:1200);
  }
 }
 throw new Error('read_unavailable');
}
