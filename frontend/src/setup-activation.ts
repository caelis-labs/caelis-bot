// Own activation above the changing onboarding pages. A returned/backtracked
// attempt may finish natively, but cannot authorize a newer first message.
export class SetupActivation {
 private attempt?: {done:Promise<boolean>};
 private initiallyReady:boolean;
 constructor(initiallyReady=false) {this.initiallyReady=initiallyReady;}
 reset() { this.attempt=undefined;this.initiallyReady=false; }
 start(work:()=>Promise<void>) {
  const attempt={done:Promise.resolve(false)};
  this.initiallyReady=false;
  this.attempt=attempt;
  attempt.done=Promise.resolve().then(work).then(()=>this.attempt===attempt,()=>false);
  return attempt.done;
 }
 // Never replay authentication, configuration effects or an uncertain message.
 // Failed preparation returns to model selection with the introduction intact.
 async ready() {
  const attempt=this.attempt;
  if(!attempt)return this.initiallyReady;
  const ready=await attempt.done;
  return ready&&this.attempt===attempt;
 }
}
