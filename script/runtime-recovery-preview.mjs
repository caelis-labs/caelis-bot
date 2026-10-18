// Mounted production component fixture, with no account, filesystem or mutation authority.
// Open the printed loopback URL; window.runRecoveryRegression() returns evidence.
import {createServer} from 'vite';
import {resolve} from 'node:path';
const entry=resolve('script/runtime-recovery-fixture.tsx');
const server=await createServer({server:{host:'127.0.0.1',port:0,strictPort:false,hmr:false},plugins:[{
 name:'runtime-recovery-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
  if(req.url?.startsWith('/acceptance')){
   res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/acceptance','<html><body><div id="root"></div><script type="module" src="/@fs/'+entry+'"></script></body></html>'));return;
  }
  if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','text/javascript');res.end('export const Call={ByName:async(name,...args)=>window.acceptanceNative?.(name,...args)};');return;}
  next();
 });}
}]});
await server.listen();console.log(`http://127.0.0.1:${server.httpServer.address().port}/acceptance`);
