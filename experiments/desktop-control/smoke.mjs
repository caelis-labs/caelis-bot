import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {SemanticDesktop} from './semantic-driver.mjs';

const desktop=new SemanticDesktop();
const evidence={driver:'@trycua/cua-driver@0.30.2',scenario:'native checkbox and visible task count',screenshots:0,runs:[]};
const filter=observation=>{
  const candidates=observation.targets.filter(t=>t.role==='AXCheckBox'&&t.name==='Only incomplete');
  assert.equal(candidates.length,1,'ambiguous filter');
  assert.deepEqual(candidates[0].actions,['click']);
  assert.ok(candidates[0].bounds.w>0&&candidates[0].bounds.h>0);
  return candidates[0];
};
try {
  await desktop.bindFixture();
  let observed=await desktop.observe();
  evidence.before=observed;
  const stale=observed;
  observed=await desktop.observe();
  await assert.rejects(()=>desktop.perform({observation:stale.observation,steps:[{op:'click',target:filter(stale).target}]}),/stale_observation/);
  evidence.staleRejected=true;
  for(let i=0;i<10;i++) {
    const before=filter(observed),wanted=before.value==='0'?'1':'0';
    const steps=[{op:'click',target:before.target}];
    // Prove the checkpoint doesn't silently execute a second external mutation.
    if(i===0)steps.push({...steps[0]});
    const started=performance.now();
    const result=await desktop.perform({observation:observed.observation,steps});
    observed=result.observation;
    assert.equal(filter(observed).value,wanted,'UI state failed to change');
    assert.ok(observed.text.includes(`Visible tasks: ${wanted==='1'?2:3}`),'effect not observed');
    assert.equal(result.remaining.length,i===0?1:0);
    assert.equal(result.steps.length,1);
    evidence.runs.push({iteration:i+1,from:before.value,to:wanted,elapsedMs:Math.round(performance.now()-started),effectObserved:true});
  }
  evidence.after=observed;
  evidence.passed=true;
} catch(error) {evidence.passed=false;evidence.error={message:error.message,mayHaveActed:error.mayHaveActed??false};throw error;}
finally {
  await desktop.close();
  await mkdir(new URL('../../.cache/desktop-control/',import.meta.url),{recursive:true});
  await writeFile(new URL('../../.cache/desktop-control/evidence.json',import.meta.url),JSON.stringify(evidence,null,2),{mode:0o600});
  console.log(JSON.stringify({passed:evidence.passed,driver:evidence.driver,runs:evidence.runs.length,screenshots:evidence.screenshots,staleRejected:evidence.staleRejected}));
}
