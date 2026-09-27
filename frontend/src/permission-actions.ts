export type PermissionID='accessibility'|'screenCapture'|'automation'|'notifications';
export type PermissionRequestResult={state:'requested'|'authorized'|'settingsRequired'|'settingsOpened'};
export type PermissionCall=<T=unknown>(method:string,...args:unknown[])=>Promise<T>;

// Only the host snapshot owns checked state. A click requests access or opens the
// OS settings for revocation; it never resets grants or flips a local permission.
export async function changeSystemPermission(call:PermissionCall,id:PermissionID,authorized:boolean):Promise<PermissionRequestResult> {
 if(authorized){await call('OpenSystemPermissionSettings',id);return {state:'settingsOpened'};}
 const result=await call<PermissionRequestResult>('RequestSystemPermission',id);
 // A completed refusal cannot produce another consent prompt. Take the user
 // to the only available recovery path, after (never during) the native request.
 if(result.state==='settingsRequired'){
  await call('OpenSystemPermissionSettings',id);
  return {state:'settingsOpened'};
 }
 return result;
}
