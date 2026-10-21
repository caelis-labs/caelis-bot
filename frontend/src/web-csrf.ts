// Refresh only after the server's explicit pre-admission CSRF refusal. Neither
// transport loss nor an owner error authorizes replay. Keep the same body/ID and
// mounted client so drafts, focus, pending receipts and tab ownership survive.
export function createCSRFFetch(initial:string,send:typeof fetch=fetch){
 let csrf=initial,refresh:Promise<void>|undefined;
 const renew=async()=>{
  const response=await send('/api/v1/bootstrap',{method:'POST',headers:{'X-Caelis-Request':'1'},signal:AbortSignal.timeout(8000)});
  if(!response.ok)return;
  const value=await response.json();
  if(typeof value.csrf==='string'&&value.csrf)csrf=value.csrf;
 };
 return async(path:string,init:RequestInit):Promise<Response>=>{
  const used=csrf;
  const attempt=()=>{const headers=new Headers(init.headers);headers.set('X-CSRF-Token',csrf);return send(path,{...init,headers});};
  const response=await attempt();
  if(response.status!==403)return response;
  let code;try{code=(await response.clone().json()).error;}catch{return response;}
  if(code!=='csrf_denied')return response;
  // Concurrent stale responses share a bootstrap. A late response must not
  // replace a newer token or start another refresh for the same transition.
  if(csrf===used){
   if(!refresh)refresh=renew().catch(()=>{}).finally(()=>{refresh=undefined});
   await refresh;
  }
  if(csrf===used||init.signal?.aborted)return response;
  return attempt(); // At most once, and only this definitely unadmitted request.
 };
}
