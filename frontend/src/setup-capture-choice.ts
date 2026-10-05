// A first-run feature choice is durable before its own step is marked complete.
export async function finishCaptureChoice(call:(method:string,...args:unknown[])=>Promise<unknown>,enabled:boolean,onDone:()=>void):Promise<void>{
 await call('SetCaptureEnabled',enabled);
 await call('FinishFeatureGuide');
 onDone();
}
