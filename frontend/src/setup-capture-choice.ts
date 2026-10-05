// A first-run choice is durable before the permission step is marked complete.
export async function finishCaptureChoice(call:(method:string,...args:unknown[])=>Promise<unknown>,enabled:boolean,onDone:()=>void):Promise<void>{
 await call('SetCaptureEnabled',enabled);
 await call('FinishPermissionGuide');
 onDone();
}
