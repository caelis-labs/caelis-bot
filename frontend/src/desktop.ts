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
  runtime ??= import(/* @vite-ignore */ url) as Promise<Runtime>;
  return (await runtime).Call.ByName<T>(`github.com/caelis-labs/caelis-bot/internal/${service}.Service.${method}`, ...args);
}
export type Placement = { x: number; y: number; scale: number; visible: boolean; positioned: boolean };
export type DraftFile = { id: string; name: string; size: number; unavailable: boolean };
