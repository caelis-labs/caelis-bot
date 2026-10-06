// Use the runtime shipped by the pinned Go dependency, avoiding a second runtime version.
type Runtime = { Call: { ByName<T>(name: string, ...args: unknown[]): Promise<T> } };
let runtime: Promise<Runtime> | undefined;
export async function desktop<T = void>(method: string, ...args: unknown[]): Promise<T> {
	return host<T>('desktop',method,...args);
}
export async function backend<T = void>(method: string, ...args: unknown[]): Promise<T> {
	return host<T>('backend',method,...args);
}
async function host<T>(service: string,method: string,...args: unknown[]): Promise<T> {
  const url = '/wails/runtime.js';
  if(!runtime) {
    const loading=import(/* @vite-ignore */ url) as Promise<Runtime>;
    runtime=loading;
    void loading.catch(()=>{if(runtime===loading)runtime=undefined;});
  }
  const reading=/Snapshot$|Preferences$|Status$/.test(method);
  const timeout=reading?5000:40000;
  let timer:ReturnType<typeof setTimeout>|undefined;
  try {
    return await Promise.race([
      (async()=> (await runtime!).Call.ByName<T>(`github.com/caelis-labs/caelis-bot/internal/${service}.Service.${method}`, ...args))(),
      new Promise<T>((_,reject)=>{timer=setTimeout(()=>reject(new Error('The operation has not been confirmed yet. Its original request is retained.')),timeout);}),
    ]);
  } finally { if(timer!==undefined)clearTimeout(timer); }
}
export type Placement = { x: number; y: number; scale: number; visible: boolean; positioned: boolean };
export type DraftFile = { id: string; name: string; size: number; type: string; image: boolean; unavailable: boolean };
export type PasteResult = { handled: boolean; files: DraftFile[] };
