import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {webRoutes} from '../frontend/src/web-routes.ts';
import {createNavigationGuard,parseWebRoute,webRouteHash} from '../frontend/src/web-navigation.ts';
import {feedbackSummary,decodeDiagnostic} from '../frontend/src/feedback.ts';

test('browser action paths match the explicit server allowlist',()=>{
 const source=readFileSync(new URL('../internal/websurface/routes.go',import.meta.url),'utf8');
 const paths=new Set([...source.matchAll(/Path: "([^"]+)"/g)].map(m=>m[1]));
 for(const [name,route] of Object.entries(webRoutes)){assert(paths.has(route.path),`${name} route missing`);assert(!/\/(?:wails|rpc|exec|shell)(?:\/|$)/i.test(route.path))}
 const names=Object.keys(webRoutes);
 for(const name of ['desktop.OpenMachineTerminal','desktop.OpenTaskTerminal','desktop.Notify','desktop.RequestSystemPermission','desktop.StartDrag','desktop.RevealArtifact'])assert(!names.includes(name),`${name} must remain a platform capability`);
});

test('setup guard covers direct URLs, settings return, history, refresh and partial completion',async()=>{
 let state={onboarding:true,required:true,status:'required'},commits=[];
 const guard=createNavigationGuard(async()=>state,(route,replace)=>commits.push({route,replace}));
 for(const hash of ['#/chat','#/settings/general','#/settings/connections','#/task/original-task','#/setup']){
  await guard(parseWebRoute(hash),true);
  assert.deepEqual(commits.at(-1),{route:{view:'settings',section:'setup'},replace:true});
 }
 state={onboarding:false,required:true,status:'required'};await guard({view:'chat'});
 assert.equal(commits.at(-1).route.section,'setup','runtime selection alone must not bypass introduction');
 for(const status of ['rejected','unknown']){
  state={onboarding:false,required:false,status};await guard({view:'chat'});
  assert.equal(commits.at(-1).route.section,'setup');
 }
 state={onboarding:false,required:false,status:'accepted'};
 await guard(parseWebRoute('#/settings/general'));assert.equal(webRouteHash(commits.at(-1).route),'#/settings/general');
 await guard({view:'chat'});assert.equal(webRouteHash(commits.at(-1).route),'#/chat');
 await guard(parseWebRoute('#/setup'),true);assert.equal(commits.at(-1).route.view,'chat','completed guide URL must not re-enter setup');
});

test('navigation fails closed and ignores out-of-order setup reads',async()=>{
 const reads=[],commits=[];
 const guard=createNavigationGuard(()=>new Promise((resolve,reject)=>reads.push({resolve,reject})),route=>commits.push(route));
 const first=guard({view:'chat'}),second=guard({view:'settings',section:'general'});
 reads[1].resolve({onboarding:true,required:true,status:'required'});await second;
 reads[0].resolve({onboarding:false,required:false,status:'accepted'});await first;
 assert.deepEqual(commits,[{view:'settings',section:'setup'}]);
 const failed=guard({view:'chat'});reads[2].reject(Error('offline'));await assert.rejects(failed);
 assert.equal(commits.length,1);
});

test('feedback is a readable allowlist and cannot copy private diagnostic payloads or URLs',()=>{
 const report={connection:'offline',platform:{os:'linux',arch:'arm64',go:'go1.26.8'},backend:{schemaBaseline:'0.153.4',retiredWorkerSubscriptions:42,secret:'PRIVATE_TOKEN'},url:'https://user:SECRET@example.com/?token=SECRET'};
 const decoded=decodeDiagnostic(Buffer.from(JSON.stringify(report)).toString('base64'));
 const summary=feedbackSummary({web:true,version:'0.17.1',report:decoded,setup:true,context:{issue:'gateway_catalog_unavailable',page:'magpie'},time:new Date('2026-10-20T00:00:00Z')});
 assert.match(summary,/问题：暂未读取到网关模型/);assert.match(summary,/页面：Web · Magpie/);
 assert.match(summary,/不代表网关/);assert.match(summary,/linux arm64/);
 assert.doesNotMatch(summary,/SECRET|PRIVATE_TOKEN|schemaBaseline|retiredWorker|0.153.4|\{|"backend"/);
 const hostile=feedbackSummary({web:true,version:'https://private/?token=x',context:{issue:'https://private',page:'https://private'},report:{connection:'secret',platform:{os:'secret'}}});
 assert.doesNotMatch(hostile,/https:|token=|secret|private/);
 assert.match(feedbackSummary({web:true,setup:true,report:{connection:'ready'}}),/对话：已连接/,'unfinished introduction must not claim the runtime is offline');
 assert.match(feedbackSummary({web:true}),/CaelisBot/,'API outage still has copyable feedback');
});
