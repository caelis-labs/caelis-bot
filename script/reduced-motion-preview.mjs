// Loopback-only browser/native-WebView fixture; never connects to a real Bot.
import {createServer} from 'vite';
import {resolve} from 'node:path';
const entry=resolve('script/reduced-motion-fixture.tsx').replaceAll('\\','/');
const server=await createServer({server:{host:'127.0.0.1',port:9247,strictPort:true,hmr:false},plugins:[{
 name:'reduced-motion-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
  if(req.url?.startsWith('/acceptance')){
   res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml('/acceptance',`<html><body style="overflow:auto"><div id="root"></div><script type="module" src="/@fs/${entry}"></script></body></html>`));return;
  }
  if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','text/javascript');res.end('export const Call={ByName:async(name,...args)=>window.acceptanceNative(name,...args)};');return;}
  next();
 });}
}]});
await server.listen();console.log('http://127.0.0.1:9247/acceptance');
