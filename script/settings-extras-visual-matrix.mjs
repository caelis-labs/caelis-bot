import {execFileSync} from 'node:child_process';
const specs=[
 ['overview','connected','en','light','860'],
 ['overview','paused','zh-CN','dark','640'],
 ['telegram','empty','en','light','860'],
 ['telegram','pairing','zh-CN','dark','640'],
 ['telegram','candidate','en','light','860'],
 ['telegram','connected','zh-CN','light','640'],
 ['telegram','paused','en','dark','860'],
 ['telegram','webhook','zh-CN','dark','640'],
 ['telegram','network','en','light','640'],
 ['extras','on','en','light','860'],
 ['extras','off','zh-CN','dark','640'],
 ['setup','feature-on','zh-CN','light','640'],
 ['setup','feature-off','en','dark','860'],
 ['setup','permission-on','en','light','860'],
 ['setup','permission-off','zh-CN','dark','640'],
];
for(const spec of specs){
 const out=execFileSync('bash',['script/settings-extras-native-fixture.sh','visual',...spec],{encoding:'utf8',timeout:30000});
 process.stdout.write(out);
}
