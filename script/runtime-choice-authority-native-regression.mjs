import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const states=['unselected-error','unselected-ready','selected-offline','selected-connecting','selected-unknown','selected-ready','selected-error'];
const expected={
 en:{choose:'Choose AI connection',noConnection:'No AI connection selected',offline:'Configured · connection unavailable',connecting:'Connecting…',unknown:'Connection status unavailable',ready:'Connected',start:'Restart and start'},
 'zh-CN':{choose:'选择 AI 连接',noConnection:'尚未选择 AI 连接',offline:'已配置 · 连接不可用',connecting:'正在连接…',unknown:'无法读取连接状态',ready:'已连接',start:'重新启动并开始'},
};
for(const language of ['en','zh-CN'])for(const suffix of states){
 const state=`runtime-authority-${suffix}`;
 execFileSync('bash',['script/settings-extras-native-fixture.sh','choice-authority','runtime',state,language,'light','860','script/runtime-choice-authority-native-regression.js'],{stdio:'pipe',timeout:30000});
 const result=JSON.parse(readFileSync(resolve(`.cache/settings-extras-fixture/choice-authority/runtime-${state}-${language}-light-860.png.json`),'utf8'));
 assert.equal(result.error,undefined,state);
 assert.equal(result.active,'codex',state);
 const selected=suffix.startsWith('selected-'),status=suffix.split('-').at(-1);
 assert.equal(result.hasRuntimeChoice,selected,state);
 assert.equal(result.snapshotFails,status==='error',state);
 if(!selected){
  assert.equal(result.title,expected[language].noConnection,state);
  assert.equal(result.status,expected[language].choose,state);
  assert.equal(result.primary,expected[language].choose,state);
  assert.equal(result.choiceVisible,true,state);
  assert.equal(result.detailReady,true,state);
  assert.equal(result.startAction,expected[language].start,state);
 }else{
  assert.equal(result.title,'Codex',state);
  assert.equal(result.status,expected[language][status==='error'?'unknown':status],state);
  assert.notEqual(result.primary,expected[language].choose,state);
 }
}
process.stdout.write(`${states.length*2} native production-client/component choice-authority cases passed\n`);
