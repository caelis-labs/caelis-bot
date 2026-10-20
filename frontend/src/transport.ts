// A surface changes transport and local presentation actions, never product
// semantics. Desktop remains the default so native entry points are unchanged.
export type ProductTransport = <T>(service:string, method:string, args:unknown[]) => Promise<T>;
let selected:ProductTransport|undefined;
export function installTransport(transport:ProductTransport){selected=transport;}
export function productTransport(){return selected;}
export function isWebSurface(){return !!selected;}
