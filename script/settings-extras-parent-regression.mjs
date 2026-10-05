import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

function run(page,state,script){
 execFileSync('bash',['script/settings-extras-native-fixture.sh','regression',page,state,'en','light','860',script],{stdio:'pipe',timeout:30000});
 const path=resolve('.cache/settings-extras-fixture/regression',`${page}-${state}-en-light-860.png.json`);
 return JSON.parse(readFileSync(path,'utf8'));
}

for(const state of ['permission-on','permission-fail']){
 const result=run('setup',state,'script/settings-extras-setup-regression.js');
 assert.equal(result.screenBefore,true);
 assert.equal(result.screenAfter,false);
 assert.equal(result.offAfterPolls,true,'BotSetup polling replaced the unsaved choice');
 assert.ok(result.botPolls>=3,'fixture did not exercise repeated parent refresh');
 assert.equal(result.preferenceReads,1,'a new onDone callback restarted preference loading');
 assert.equal(result.savedOff,true);
 assert.ok(result.save>=0);
 if(state==='permission-on'){
  assert.ok(result.finish>result.save,'guide completed before capture choice was saved');
 }else{
  assert.equal(result.finish,-1,'failed save completed the guide');
  assert.equal(result.stillOnStep,true);
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
