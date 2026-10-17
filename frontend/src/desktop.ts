// Use the runtime shipped by the pinned Go dependency, avoiding a second runtime version.
import {nativeCall} from './native-call';
type Runtime = { Call: { ByName<T>(name: string, ...args: unknown[]): Promise<T> } };
let runtime: Promise<Runtime> | undefined;
export async function desktop<T = void>(method: string, ...args: unknown[]): Promise<T> {
	return host<T>('desktop',method,...args);
}
export async function backend<T = void>(method: string, ...args: unknown[]): Promise<T> {
	const start=performance.now();
	try{return await host<T>('backend',method,...args);}
	finally{if(navigator.platform.toLowerCase().startsWith('win')&&['SetupProfile','InspectSetup','SetupModels','SetupOverview','ExecutionSettings','LocalWorkerSelection'].includes(method))void desktop('WindowsUIPerformance',method,performance.now()-start).catch(()=>{});}
}
async function host<T>(service: string,method: string,...args: unknown[]): Promise<T> {
  const url = '/wails/runtime.js';
  if(!runtime) {
    const loading=import(/* @vite-ignore */ url) as Promise<Runtime>;
    runtime=loading;
    void loading.catch(()=>{if(runtime===loading)runtime=undefined;});
  }
  return nativeCall(service,method,async()=> (await runtime!).Call.ByName<T>(`github.com/caelis-labs/caelis-bot/internal/${service}.Service.${method}`, ...args),args);
}
export type Placement = { x: number; y: number; scale: number; visible: boolean; positioned: boolean };
export type DraftFile = { id: string; name: string; size: number; type: string; image: boolean; unavailable: boolean };
export type PasteResult = { handled: boolean; files: DraftFile[] };
