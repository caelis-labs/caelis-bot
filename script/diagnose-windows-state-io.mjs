// Process original-suite traces, including app/memory flight snapshots. No
// second test invocation, network fetch, deadline change or durability relaxation.
import {spawnSync} from 'node:child_process';
import {mkdirSync,writeFileSync,readFileSync,openSync,closeSync,existsSync} from 'node:fs';
import {resolve,basename} from 'node:path';
const root=resolve(import.meta.dirname,'..');
const tracePath=resolve(process.argv[2]||resolve(root,'.cache/windows-state-io/original-workerpeer.trace'));
const dir=resolve(root,'.cache/windows-state-io',basename(tracePath,'.trace'));
mkdirSync(dir,{recursive:true});
const run=args=>spawnSync('go',args,{cwd:root,encoding:'utf8',maxBuffer:16<<20});
const meta={sha:spawnSync('git',['rev-parse','HEAD'],{cwd:root,encoding:'utf8'}).stdout?.trim(),sourceTree:spawnSync('git',['rev-parse','HEAD^{tree}'],{cwd:root,encoding:'utf8'}).stdout?.trim(),head:process.env.CI_SOURCE_HEAD||null,worktreeDirty:!!spawnSync('git',['status','--porcelain','--untracked-files=normal'],{cwd:root,encoding:'utf8'}).stdout?.trim(),platform:process.platform,go:run(['version']).stdout?.trim(),evidence:'original full-suite TestMain trace: '+basename(tracePath)+'; no second test invocation',limits:'syscall/sync wall times and stacks; not Windows error/ACL/driver attribution or proof of writer completion after trace ends'};
if(!existsSync(tracePath)){writeFileSync(resolve(dir,'metadata.json'),JSON.stringify({...meta,unavailable:'package did not produce trace'},null,2));throw new Error('Original package trace unavailable; preserve preceding suite result')}
const tools={};let diagnosticFailed=false;
for(const name of ['trace','pprof']){
 const override=process.env[name==='trace'?'GO_TRACE_TOOL':'GO_PPROF_TOOL'];
 if(override){tools[name]=[override];continue}
 if(run(['tool','-n',name]).status===0){tools[name]=['go','tool',name];continue}
 // Some Go installations omit these executables. Compile only installed
 // official GOROOT sources/vendor, with network/toolchain downloads disabled.
 const target=resolve(root,'.cache/diagnostic-tools',name+(process.platform==='win32'?'.exe':''));mkdirSync(resolve(root,'.cache/diagnostic-tools'),{recursive:true});
 const built=spawnSync('go',['build','-o',target,'cmd/'+name],{cwd:root,env:{...process.env,GOPROXY:'off',GOSUMDB:'off',GOTOOLCHAIN:'local',GO111MODULE:'off',GOWORK:'off',GOFLAGS:''},encoding:'utf8'});
 if(built.status!==0){writeFileSync(resolve(dir,name+'-tool-error.log'),built.stderr??'');throw new Error('Cannot compile installed Go diagnostic tool: '+name)}
 tools[name]=[target];
}
function tool(name,args,output){const fd=openSync(resolve(dir,output),'w');const [cmd,...prefix]=tools[name];const r=spawnSync(cmd,[...prefix,...args],{cwd:root,stdio:['ignore',fd,'pipe'],encoding:'utf8'});closeSync(fd);if(r.status!==0){diagnosticFailed=true;writeFileSync(resolve(dir,output+'.stderr'),r.stderr??r.error?.message??'');console.error('STATE_IO_DIAGNOSTIC tool failed',name,r.status)}return r.status}
for(const type of ['syscall','sync']){
 if(tool('trace',['-pprof='+type,tracePath],type+'.pprof')===0){
  tool('pprof',['-top','-nodecount=30',resolve(dir,type+'.pprof')],type+'-top.txt');
  tool('pprof',['-traces','-focus=localstate|botmemory|memory/internal/appliance',resolve(dir,type+'.pprof')],'storage-'+type+'.txt');
  console.log('STATE_IO_DIAGNOSTIC original-suite '+type+' aggregate:\n'+readFileSync(resolve(dir,type+'-top.txt'),'utf8'));
  console.log('STATE_IO_DIAGNOSTIC full storage '+type+' stacks/times retained in '+resolve(dir,'storage-'+type+'.txt'));
 }
}
tool('trace',['-d=parsed',tracePath],'runtime-parsed.txt');
writeFileSync(resolve(dir,'metadata.json'),JSON.stringify({...meta,diagnosticFailed},null,2));
if(diagnosticFailed)process.exitCode=1;
