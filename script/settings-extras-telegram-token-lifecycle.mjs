import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
for(const state of ['invalid-token','invalid-late']){
 execFileSync('bash',['script/settings-extras-native-fixture.sh','token-lifecycle','telegram',state,'zh-CN','light','860','script/settings-extras-telegram-token-lifecycle.js'],{stdio:'pipe',timeout:30000});
 const result=JSON.parse(readFileSync(resolve(`.cache/settings-extras-fixture/token-lifecycle/telegram-${state}-zh-CN-light-860.png.json`),'utf8'));
 assert.equal(result.error,undefined);
 for(const key of state==='invalid-token'?['cleared','fieldError','masked']:['closeCleared','reopenCleared','navigationCleared','reentryCleared','pendingCloseCleared','lateCleared','fieldError','masked'])assert.equal(result[key],true,`${state}: ${key}`);
 assert.equal(result.calls,1,`${state}: one user action dispatched once`);
}
process.stdout.write('Telegram synthetic token close, reopen, navigation, validation and late result passed\n');
