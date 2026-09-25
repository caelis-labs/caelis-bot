export type TaskPreferences={maxRunning:number;terminal:string;revision:number;customCommand:string};
type Values=Pick<TaskPreferences,'maxRunning'|'terminal'|'customCommand'>;
export const validTaskLimit=(value:string)=>value.trim()!==''&&Number.isSafeInteger(Number(value))&&Number(value)>0;

// Serialize writes without blocking typing. A late receipt advances the revision
// but never replaces a newer draft. Failed writes wait for another explicit edit.
export class TaskPreferencesSaver {
 private saved:TaskPreferences;
 private pending:Partial<Values>={};
 private running=false;
 private reload=false;
 private blocked=false;
 private read:()=>Promise<TaskPreferences>;
 private write:(p:TaskPreferences)=>Promise<TaskPreferences>;
 private state:(s:'saving'|'saved'|'failed')=>void;
 private accepted:(p:TaskPreferences)=>void;
 constructor(initial:TaskPreferences,read:()=>Promise<TaskPreferences>,write:(p:TaskPreferences)=>Promise<TaskPreferences>,state:(s:'saving'|'saved'|'failed')=>void,accepted:(p:TaskPreferences)=>void=()=>{}){this.saved=initial;this.read=read;this.write=write;this.state=state;this.accepted=accepted;}
 change(p:Partial<Values>){this.pending={...this.pending,...p};this.blocked=false;}
 discardLimit(){delete this.pending.maxRunning;}
 async flush(){
  if(this.running||this.blocked||Object.keys(this.pending).length===0)return;
  this.running=true;this.state('saving');
  try{
   if(this.reload){this.saved=await this.read();this.reload=false;}
   while(Object.keys(this.pending).length){
    const patch=this.pending;this.pending={};
    try{this.saved=await this.write({...this.saved,...patch});if(Object.keys(this.pending).length===0)this.accepted(this.saved);}
    catch(e){this.pending={...patch,...this.pending};throw e;}
   }
   this.state('saved');
  }catch{this.reload=true;this.blocked=true;this.state('failed');}
  finally{this.running=false;}
 }
}
