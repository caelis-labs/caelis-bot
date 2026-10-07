import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
const cases=[];
for(const language of ['en','zh-CN']){
 for(const selection of ['selected','unselected'])for(const connection of ['offline','connecting','unknown','ready'])cases.push({state:`runtime-${selection}-${connection}`,language});
 for(const connection of ['offline','ready'])cases.push({state:`runtime-onboarding-${connection}`,language});
 for(const action of ['install','update'])cases.push({state:`runtime-${action}-offline`,language});
}
for(const {state,language} of cases){
 execFileSync('bash',['script/settings-extras-native-fixture.sh','status-matrix','runtime',state,language,'light','860','script/runtime-status-native-regression.js'],{stdio:'pipe',timeout:30000});
 const result=JSON.parse(readFileSync(resolve(`.cache/settings-extras-fixture/status-matrix/runtime-${state}-${language}-light-860.png.json`),'utf8'));
 const ready=language==='en'?'Setup ready':'配置就绪',connected=language==='en'?'Connected':'已连接',choose=language==='en'?'Choose AI connection':'选择 AI 连接';
 if(state.includes('onboarding')){assert.equal(result.ready,true,state);assert.equal(result.badge,ready,state);continue}
 assert.equal(result.detailReady,true,state);
 assert.equal(result.initial,state.includes('unselected')?choose:state.endsWith('ready')?connected:state.endsWith('offline')?(language==='en'?'Configured · connection unavailable':'已配置 · 连接不可用'):state.endsWith('connecting')?(language==='en'?'Connecting…':'正在连接…'):(language==='en'?'Connection status unavailable':'无法读取连接状态'),state);
 assert.ok(result.notices.length>0,`${state}: operation notice`);
 assert.equal(result.notices.at(-1),state.includes('install')||state.includes('update')?ready:language==='en'?'Setup check passed.':'配置检查通过。',state);
 assert.notEqual(result.notices.at(-1),connected,state);
}
process.stdout.write(`${cases.length} native production-component setup/session matrix cases passed\n`);
