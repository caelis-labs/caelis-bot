// Deadlines bound host work, not the time a person spends in a native sheet.
// An unknown result never retries the original operation.
export async function nativeCall<T>(service:string,method:string,invoke:()=>Promise<T>):Promise<T> {
 if(service==='desktop'&&method==='PickFiles')return invoke();
 const reading=/Snapshot$|Preferences$|Status$/.test(method)||['Draft','DraftFiles','DraftImage','DraftImagePreview','HistoryVisible'].includes(method);
 let timer:ReturnType<typeof setTimeout>|undefined;
 try {
  return await Promise.race([
   invoke(),
   new Promise<T>((_,reject)=>{timer=setTimeout(()=>reject(new Error('The operation has not been confirmed yet. Its original request is retained.')),reading?5000:40000);}),
  ]);
 }finally{if(timer!==undefined)clearTimeout(timer);}
}
