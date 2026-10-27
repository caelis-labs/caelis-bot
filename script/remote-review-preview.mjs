import {createServer} from 'vite';
import {resolve} from 'node:path';
import {execFileSync} from 'node:child_process';
const target=process.env.REMOTE_REVIEW_SERVICE;if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(target||''))throw Error('isolated service required');
const baseline=process.env.REMOTE_REVIEW_BASELINE==='1';
const server=await createServer({cacheDir:'.cache/review-233/vite-'+(baseline?'before':'after'),server:{host:'127.0.0.1',port:0,hmr:false,proxy:{'/rpc':{target,rewrite:()=>'/rpc'}}},plugins:[{
 name:'remote-review',enforce:'pre',transform(code,id){if(baseline&&id.endsWith('/settings/runtime/BotConnections.tsx'))return execFileSync('git',['show','0e675de2:frontend/src/settings/runtime/BotConnections.tsx'],{encoding:'utf8'})},configureServer(s){s.middlewares.use(async(req,res,next)=>{
 if(req.url?.startsWith('/acceptance')){res.setHeader('Content-Type','text/html');res.end(await s.transformIndexHtml('/acceptance','<html><head><meta name="viewport" content="width=device-width,initial-scale=1"/></head><body><div id="root"></div><script type="module" src="/@fs/'+resolve('script/remote-review-fixture.tsx')+'"></script></body></html>'));return}
 if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','text/javascript');res.end('export const Call={ByName:async()=>{throw Error("fixture only")}}');return}next()
 })}
}]});await server.listen();console.log(server.resolvedUrls.local[0]+'acceptance');
