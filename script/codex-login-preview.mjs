// Mounted shared App/Web ceremony with a fake account. No real credentials.
import {createServer} from 'vite';
import {resolve} from 'node:path';
const entry=resolve('script/codex-login-fixture.tsx');
const server=await createServer({cacheDir:resolve('.cache/codex-login-vite'),server:{host:'127.0.0.1',port:0,hmr:false},plugins:[{
 name:'codex-login-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
  if(req.url?.startsWith('/acceptance')){
   res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/acceptance','<html><body><div id="root"></div><script type="module" src="/@fs/'+entry+'"></script></body></html>'));return;
  }
  if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','text/javascript');res.end('export const Call={ByName:async()=>{}};');return;}
  next();
 });}
}]});
await server.listen();console.log(`http://127.0.0.1:${server.httpServer.address().port}/acceptance`);
