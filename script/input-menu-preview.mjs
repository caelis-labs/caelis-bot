// Production UI, synthetic transport only; no credentials or model calls.
import {createServer} from 'vite';
import {mkdtempSync,readFileSync,rmSync,symlinkSync,writeFileSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';

const urlFile=process.argv[2]&&resolve(process.argv[2]);
const baseline=process.env.BOT_INPUT_MENU_BASELINE;
if(baseline){
 if(!/^[a-f0-9]{7,40}$/i.test(baseline))throw Error('Baseline must be a commit SHA');
 const current=process.cwd(),snapshot=mkdtempSync(join(tmpdir(),'bot-input-baseline-'));
 process.once('exit',()=>rmSync(snapshot,{recursive:true,force:true}));
 process.once('SIGINT',()=>process.exit(130));
 process.once('SIGTERM',()=>process.exit(143));
 const archive=join(snapshot,'source.tar');
 execFileSync('git',['archive','--format=tar','-o',archive,baseline]);
 execFileSync('tar',['-xf',archive,'-C',snapshot]);
 rmSync(archive);
 // Reuse installed packages only for the same lockfile. All source, fixtures,
 // Vite configuration and public assets come from the archived tree.
 if(!readFileSync(join(current,'package-lock.json')).equals(readFileSync(join(snapshot,'package-lock.json')))){
  throw Error('Baseline has different dependencies. Run the preview from a checkout of that commit after npm ci.');
 }
 symlinkSync(join(current,'node_modules'),join(snapshot,'node_modules'),'junction');
 process.chdir(snapshot);
}

const {fixtureRuntime}=await import(pathToFileURL(resolve('script/desktop-settings-fixture.mjs')).href);
const runtime=fixtureRuntime.replace('calls.push(method);','calls.push(method);(window.fixturePayloads??=[]).push({method,args:structuredClone(args)});');
const streamRuntime=runtime.replace("case 'BotInitialization':","case 'SetBubbleVisible':case 'SetBubbleHeight':case 'CollapseBubble':return;case 'BotInitialization':").replace('return {changed:true,snapshot};','window.fixtureStreamObserved={revision:snapshot.revision,status:snapshot.items.at(-1)?.status};return {changed:true,snapshot:structuredClone(snapshot)};').replace("case 'Snapshot':case 'ComposerSnapshot':case 'SnapshotSummary':return snapshot;","case 'PetSnapshot':case 'Snapshot':case 'ComposerSnapshot':case 'SnapshotSummary':window.fixtureStreamObserved={revision:snapshot.revision,status:snapshot.items.at(-1)?.status};return structuredClone(snapshot);").replace("case 'Placement':return {scale:1};","case 'Placement':return {scale:1,visible:true};")+`
window.fixtureStreamSnapshot=({id,text,status})=>{const item={id,kind:'assistant',turnKey:id,text,status,artifacts:[]};snapshot.items=id==='history'?[item]:[...snapshot.items.filter(previous=>previous.id!==id),item];Object.assign(snapshot,{phase:status==='inProgress'?'working':'completed',currentTurn:status==='inProgress'?id:'',canInterrupt:status==='inProgress',canSend:status!=='inProgress',approvals:[],reviews:[],quiet:false,previewDismissed:false});return ++snapshot.revision};
`;
const entry=resolve('script/input-menu-fixture.tsx');
const server=await createServer({cacheDir:'.cache/input-menu-vite',server:{host:'127.0.0.1',port:0,strictPort:false,hmr:false},plugins:[{name:'input-menu-fixture',configureServer(s){s.middlewares.use(async(req,res,next)=>{
 if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','application/javascript');res.end(streamRuntime);return;}
 if(req.url?.startsWith('/acceptance')){res.setHeader('Content-Type','text/html');res.end(await s.transformIndexHtml('/acceptance','<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="root"></div><script type="module" src="/@fs/'+entry+'"></script></body></html>'));return;}
 next();
});}}]});
await server.listen();
const url=`http://127.0.0.1:${server.httpServer.address().port}/acceptance`;
if(urlFile)writeFileSync(urlFile,url);else console.log(url);
