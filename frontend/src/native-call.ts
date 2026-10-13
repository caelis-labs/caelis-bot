// Deadlines bound host work, not the time a person spends in a native sheet.
// An unknown result never retries the original operation.
export async function nativeCall<T>(service:string,method:string,invoke:()=>Promise<T>,args:unknown[]=[]):Promise<T> {
 if(service==='desktop'&&method==='PickFiles')return invoke();
 // A fresh install can make slow but measurable network progress for hours.
 // Its result is authoritative only when the native call itself completes.
 if(service==='backend'&&method==='ApplySetup'&&typeof args[0]==='object'&&args[0]!==null&&'action' in args[0]&&args[0].action==='install')return invoke();
 const reading=/Snapshot$|Preferences$|Status$/.test(method)||['Draft','DraftFiles','DraftImage','DraftImagePreview','HistoryVisible'].includes(method);
 // Other setup operations may activate a shared Host after changing the CLI.
 const deadline=service==='backend'&&method==='ApplySetup'?720_000:reading?5_000:40_000;
 let timer:ReturnType<typeof setTimeout>|undefined;
 try {
  return await Promise.race([
   invoke(),
   new Promise<T>((_,reject)=>{timer=setTimeout(()=>reject(new Error('The operation has not been confirmed yet. Its original request is retained.')),deadline);}),
  ]);
 }finally{if(timer!==undefined)clearTimeout(timer);}
}
