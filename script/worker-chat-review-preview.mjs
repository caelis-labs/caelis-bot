// Read-only renderer regression entry point for a browser/CUA; no live backend.
import {createServer} from 'vite';
import {fixtureRuntime} from './desktop-settings-fixture.mjs';
const runtime=fixtureRuntime+String.raw`
const originalCall=Call.ByName;
Call.ByName=async(name,...args)=>{const method=name.split('.').at(-1),output=document.getElementById('fixture-call');if(output&&['OpenArtifact','ShowArtifactPreview'].includes(method)){output.textContent=method+' '+args[0];return;}return originalCall(name,...args);};
`;
const server=await createServer({server:{host:'127.0.0.1',port:0,strictPort:false,hmr:false},plugins:[{name:'worker-chat-review-fixture',configureServer(s){s.middlewares.use(async(req,res,next)=>{
 if(req.url?.split('?')[0]==='/wails/runtime.js'){res.setHeader('Content-Type','application/javascript');res.end(runtime);return;}
 if(req.url?.split('?')[0]!=='/worker-review')return next();
 const html=await s.transformIndexHtml('/worker-review','<!doctype html><html><head><meta charset="UTF-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/><title>Synthetic Worker regression</title></head><body><div id="root"></div><script type="module" src="/@fs/'+process.cwd()+'/script/worker-chat-review-fixture.tsx"></script></body></html>');
 res.setHeader('Content-Type','text/html');res.end(html);
})}}]});
await server.listen();
console.log(JSON.stringify({worker:server.resolvedUrls.local[0]+'worker-review',sources:server.resolvedUrls.local[0]+'desktop-settings-preview.html?section=machines&sources=duplicates&machines=empty',evidence:'actual production React; synthetic presentation data and transport only'}));
for(const signal of ['SIGINT','SIGTERM'])process.once(signal,async()=>{await server.close();process.exit(0)});
