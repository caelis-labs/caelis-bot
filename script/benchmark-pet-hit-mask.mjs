import assert from 'node:assert/strict';
import {cpus,platform,arch} from 'node:os';
import {execFileSync} from 'node:child_process';
import {performance} from 'node:perf_hooks';
import {readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {legacyHitMask,maskFrames} from './pet-hit-mask-fixture.mjs';

const mode=process.argv[2];
assert.ok(['baseline','candidate'].includes(mode),'Use baseline or candidate');
const width=180,height=240,frames=maskFrames(width,height),iterations=350,trials=5;
const create=mode==='baseline'?()=>rgba=>legacyHitMask(rgba,width,height):()=>createHitMaskEncoder(width,height);
let createHitMaskEncoder;
if(mode==='candidate')({createHitMaskEncoder}=await import('../frontend/src/character/hit-mask.ts'));
const cases={transparent:[frames.blank],stablePose:[frames.body],changingPose:[frames.body,frames.shifted],dense:[frames.dense]};
const results={mode,sourceHead:execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim(),platform:platform(),arch:arch(),cpu:cpus()[0].model,node:process.version,width,height,iterations,trials,scope:'Pure synthetic RGBA-to-packed-base64 microbenchmark. No WebGL/native IPC, GPU/watts or whole-app energy measurement.',cases:{}};
const digest=path=>createHash('sha256').update(readFileSync(new URL(path,import.meta.url))).digest('hex');
results.referenceFixtureSha256=digest('./pet-hit-mask-fixture.mjs');
if(mode==='candidate')results.candidateHelperSha256=digest('../frontend/src/character/hit-mask.ts');
let sink=0;
for(const [name,sequence] of Object.entries(cases)){
 const samples=[];
 for(let trial=0;trial<trials;trial++){
  const encode=create();
  for(let i=0;i<50;i++)encode(sequence[i%sequence.length]);
  const cpu=process.cpuUsage(),started=performance.now();
  for(let i=0;i<iterations;i++)sink+=encode(sequence[i%sequence.length]).length;
  const elapsedMs=performance.now()-started,used=process.cpuUsage(cpu);
  samples.push({wallUsPerFrame:elapsedMs*1000/iterations,cpuUsPerFrame:(used.user+used.system)/iterations});
 }
 const median=key=>samples.map(s=>s[key]).sort((a,b)=>a-b)[Math.floor(trials/2)];
 results.cases[name]={medianWallUsPerFrame:median('wallUsPerFrame'),medianCpuUsPerFrame:median('cpuUsPerFrame'),samples};
}
results.sink=sink;
console.log(JSON.stringify(results,null,2));
