import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

function run(page,state,script){
 execFileSync('bash',['script/settings-extras-native-fixture.sh','regression',page,state,'en','light','860',script],{stdio:'pipe',timeout:30000});
 const path=resolve('.cache/settings-extras-fixture/regression',`${page}-${state}-en-light-860.png.json`);
 return JSON.parse(readFileSync(path,'utf8'));
}

for(const state of ['feature-on','feature-fail','feature-off','feature-ready']){
 const result=run('setup',state,'script/settings-extras-setup-regression.js');
 assert.equal(result.defaultChoice,state!=='feature-off');
 assert.equal(result.draftAfterPolls,false,'BotSetup polling replaced the unsaved choice');
 assert.ok(result.botPolls>=3,'fixture did not exercise repeated parent refresh');
 assert.equal(result.preferenceReads,1,'a new onDone callback restarted preference loading');
 assert.equal(result.savedValue,false);
 assert.ok(result.saved>=0);
 if(state!=='feature-fail'){
  assert.ok(result.featureFinished>result.saved,'feature step finished before capture choice was saved');
  assert.ok(result.permissionFinished>result.featureFinished,'permission step finished before feature step');
  assert.equal(result.permissionStep,true);
  assert.equal(result.screenOnPermission,false,'disabled capture requested screen permission');
  assert.equal(result.featureOnPermission,false,'feature control remained in permissions');
  if(state==='feature-ready')assert.ok(result.calls.includes('OpenHistory'),'ready Runtime bypassed feature choice or failed to resume');
 }else{
  assert.equal(result.featureFinished,-1,'failed save completed feature step');
  assert.equal(result.permissionFinished,-1,'failed save completed permission step');
  assert.equal(result.stillOnFeature,true);
  assert.equal(result.errorVisible,true);
 }
}

const recovered=run('telegram','load-recover','script/settings-extras-telegram-regression.js');
assert.ok(recovered.statusReads>=2);
assert.equal(recovered.alert,'','successful status poll retained the load error');
assert.equal(recovered.tokenVisible,true);

const explicit=run('telegram','open-error','script/settings-extras-telegram-regression.js');
assert.ok(explicit.statusReads>=2);
assert.ok(explicit.calls.includes('OpenTelegramSetup'));
assert.ok(explicit.alert.length>0,'successful status poll erased an explicit action error');

process.stdout.write('Settings parent polling, save failure, and Telegram status recovery passed\n');
