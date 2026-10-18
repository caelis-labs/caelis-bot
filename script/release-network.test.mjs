import test from 'node:test';
import assert from 'node:assert/strict';
import {releaseCommand,networkDelays} from './release-network.mjs';
const reset=Object.assign(new Error('connection reset'),{status:1,stderr:'connection reset by peer'});
for(const [cmd,args] of [['xcrun',['notarytool','info','same-id']],['xcrun',['notarytool','log','same-id','same-file']],['gh',['api','repos/example/releases']],['gh',['release','download','v1.0.0','--clobber']],['curl',['--fail','https://example.test']]])test(`${cmd} ${args[0]} recovers bounded reads with identical arguments`,()=>{
 let calls=0;const waits=[];
 assert.equal(releaseCommand(cmd,args,{},(binary,parameters)=>{assert.equal(binary,cmd);assert.deepEqual(parameters,args);if(++calls<3)throw reset;return 'ok';},ms=>waits.push(ms)),'ok');
 assert.equal(calls,3);assert.deepEqual(waits,networkDelays.slice(0,2));
});
for(const args of [['notarytool','submit','artifact'],['notarytool','store-credentials','profile']])test(`never replays ${args[1]}`,()=>{
 let calls=0;assert.throws(()=>releaseCommand('xcrun',args,{},()=>{calls++;throw reset;},()=>assert.fail()),/connection reset/);assert.equal(calls,1);
});
for(const text of ['HTTP 403 Forbidden','TLS certificate failure','signature mismatch','HTTP 404','unknown failure','Domain=NSURLErrorDomain Code=-1200'])test(`fails closed on ${text}`,()=>{
 assert.throws(()=>releaseCommand('gh',['api','x'],{},()=>{throw Object.assign(new Error(text),{stderr:text});},()=>assert.fail()),Error);
});
test('persistent interruption exhausts the budget and keeps failure',()=>{
 let calls=0;assert.throws(()=>releaseCommand('gh',['api','x'],{},()=>{calls++;throw reset;},()=>{}),/connection reset/);assert.equal(calls,5);
});

for(const diagnostic of ['curl: (18) end of response with 1024 bytes missing','curl: (56) Recv failure: Connection was reset'])test(`Windows download resumes the same temporary target after ${diagnostic}`,()=>{
 let calls=0;
 const args=['--fail','--location','https://example.test/pinned.zip','--output','same.partial'];
 assert.equal(releaseCommand('curl.exe',args,{},(_cmd,actual)=>{assert.deepEqual(actual,args);if(++calls===1)throw Object.assign(new Error(diagnostic),{stderr:diagnostic});return '';},()=>{}),'');
 assert.equal(calls,2);
});
for(const stderr of ['Could not resolve host: www.apple.com','curl: (22) The requested URL returned error: 503'])test(`diagnostics, not certificate URL in argv, determine retry: ${stderr}`,()=>{
 let calls=0;
 assert.equal(releaseCommand('curl',['--fail','https://www.apple.com/certificateauthority/DeveloperIDG2CA.cer'],{},()=>{
  if(++calls===1)throw Object.assign(new Error('Command failed: curl https://www.apple.com/certificateauthority/DeveloperIDG2CA.cer'),{stderr});return 'certificate bytes';
 },()=>{}),'certificate bytes');
 assert.equal(calls,2);
});
