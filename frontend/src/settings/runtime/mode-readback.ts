// Reads and writes share one epoch. A refresh started before a successful
// save can never replace the later authoritative readback with an old value.
export function createModeReadback() {
 let epoch=0;
 return {
  invalidate() { epoch++; },
  async read(read:()=>Promise<string>, accept:(value:string)=>void, fail:(error:unknown)=>void) {
   const ticket=++epoch;
   try { const value=await read(); if(ticket===epoch)accept(value); }
   catch(error) { if(ticket===epoch)fail(error); throw error; }
  },
  async save(write:()=>Promise<unknown>, read:()=>Promise<string>, accept:(value:string)=>void, fail:(error:unknown)=>void) {
   epoch++;
   let writeFailed=false,writeError:unknown;
   try { await write(); } catch(error) { writeFailed=true;writeError=error; }
   await this.read(read,accept,fail);
   if(writeFailed)throw writeError;
  },
 };
}
