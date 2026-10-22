// Production UI, synthetic transport only; no credentials or model calls.
import {createServer} from 'vite';
import {mkdirSync,writeFileSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {execFileSync} from 'node:child_process';
import {fixtureRuntime} from './desktop-settings-fixture.mjs';
const runtime=fixtureRuntime.replace('calls.push(method);','calls.push(method);(window.fixturePayloads??=[]).push({method,args:structuredClone(args)});');
const entry=resolve('script/input-menu-fixture.tsx');
const baseline=process.env.BOT_INPUT_MENU_BASELINE;
if(baseline&&!/^[a-f0-9]{7,40}$/i.test(baseline))throw Error('Baseline must be a commit SHA');
const baselineSurface=baseline?resolve('.cache/input-menu-baseline',baseline,'surface.css'):'';
if(baseline){mkdirSync(dirname(baselineSurface),{recursive:true});writeFileSync(baselineSurface,execFileSync('git',['show',`${baseline}:frontend/src/surface.css`]));}
const sourceFiles=['Panel.tsx','SettingsSelect.tsx'].map(file=>resolve('frontend/src',file));
const server=await createServer({cacheDir:'.cache/input-menu-vite'+(baseline?'-'+baseline:''),server:{host:'127.0.0.1',port:0,strictPort:false,hmr:false},plugins:[{name:'input-menu-baseline',enforce:'pre',transform(code,id){if(baseline&&id.split('?')[0]===resolve('frontend/src/application.css'))return code.replace("@import './surface.css';",`@import '${baselineSurface}';`);},load(id){if(baseline&&sourceFiles.includes(id.split('?')[0]))return execFileSync('git',['show',`${baseline}:${id.slice(process.cwd().length+1).split('?')[0]}`],{encoding:'utf8'});}},{name:'input-menu-fixture',configureServer(s){s.middlewares.use(async(req,res,next)=>{
 if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','application/javascript');res.end(runtime);return;}
 if(req.url?.startsWith('/acceptance')){res.setHeader('Content-Type','text/html');res.end(await s.transformIndexHtml('/acceptance','<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="root"></div><script type="module" src="/@fs/'+entry+'"></script></body></html>'));return;}
 next();
});}}]});
await server.listen();
const url=`http://127.0.0.1:${server.httpServer.address().port}/acceptance`;
if(process.argv[2])writeFileSync(process.argv[2],url);else console.log(url);
