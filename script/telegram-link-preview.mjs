// Disposable synthetic transport. Never connects to Telegram or a Bot service.
import {createServer} from 'vite';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {execFileSync} from 'node:child_process';
import {fixtureRuntime} from './desktop-settings-fixture.mjs';
const state=`window.fixtureTelegram={enabled:true,bot:'fixture_caelis_bot',owner:'',paired:false,candidate:'',pairURL:'https://t.me/fixture_caelis_bot?start=SYNTHETIC_pair%2D9&extra=keep%2Fvalue#fixture',issue:''};window.fixtureNativeLinks=[];`;
const runtime=state+fixtureRuntime.replace("case 'TelegramStatus':return {enabled:false,bot:'',owner:'',paired:false,candidate:'',pairURL:'',issue:''};",`case 'TelegramStatus':return structuredClone(window.fixtureTelegram);
 case 'OpenTelegramSetup':window.fixtureNativeLinks.push({pair:args[0],url:args[0]?(window.fixtureTelegram.pairURL||'https://t.me/'+window.fixtureTelegram.bot):'https://t.me/BotFather'});if(window.fixtureOpenFailure)throw Error('synthetic opener unavailable');return;
 case 'CopyText':if(window.fixtureCopyFailure)throw Error('synthetic clipboard denied');window.fixtureCopied=args[0];return;
 case 'ConfirmTelegram':if(!window.fixtureTelegram.candidate)throw Error('synthetic candidate required');Object.assign(window.fixtureTelegram,{paired:true,owner:window.fixtureTelegram.candidate,candidate:'',pairURL:''});return structuredClone(window.fixtureTelegram);`);
const baseline=process.env.BOT_TELEGRAM_BASELINE;
if(baseline&&!/^[a-f0-9]{7,40}$/i.test(baseline))throw Error('Baseline must be a commit SHA');
const sources=['TelegramSettings.tsx','telegram-state.ts','web-transport.ts'].map(file=>resolve('frontend/src',file));
const entry=resolve('script/telegram-link-fixture.tsx');
const server=await createServer({cacheDir:'.cache/telegram-link-vite'+(baseline?'-'+baseline:''),server:{host:'127.0.0.1',port:0,hmr:false},plugins:[{
 name:'telegram-link-fixture',enforce:'pre',load(id){if(baseline&&sources.includes(id.split('?')[0]))return execFileSync('git',['show',`${baseline}:${id.slice(process.cwd().length+1).split('?')[0]}`],{encoding:'utf8'});},
 configureServer(s){s.middlewares.use(async(req,res,next)=>{
  if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','application/javascript');res.end(runtime);return;}
  if(req.url?.startsWith('/acceptance')){res.setHeader('Content-Type','text/html');res.end(await s.transformIndexHtml('/acceptance','<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body data-surface="settings"><div id="root"></div><script type="module" src="/@fs/'+entry+'"></script></body></html>'));return;}
  next();
 });}
}]});
await server.listen();
const url=`http://127.0.0.1:${server.httpServer.address().port}`;
if(process.argv[2])writeFileSync(process.argv[2],url);else console.log(url);
