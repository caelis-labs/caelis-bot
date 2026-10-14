export type ResourceReadback<T> =
 | {status:'idle'|'loading';value:null}
 | {status:'ready';value:T}
 | {status:'error';value:null;error:unknown};

export function createResourceReadback<T>() {
 let epoch=0;
 return {
  invalidate() { epoch++; },
  async load(read:()=>Promise<T>, publish:(state:ResourceReadback<T>)=>void) {
   const ticket=++epoch;
   publish({status:'loading',value:null});
   try {const value=await read();if(ticket===epoch)publish({status:'ready',value});}
   catch(error) {if(ticket===epoch)publish({status:'error',value:null,error});}
  },
 };
}
