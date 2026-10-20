import {createServer} from 'vite';
import {writeFileSync} from 'node:fs';
import {fixtureRuntime} from './desktop-settings-fixture.mjs';
const server=await createServer({server:{port:0,strictPort:false},plugins:[{name:'synthetic-desktop-transport',configureServer(s){s.middlewares.use((req,res,next)=>{if(req.url?.split('?')[0]!=='/wails/runtime.js')return next();res.setHeader('Content-Type','application/javascript');res.end(fixtureRuntime);});}}]});
await server.listen();
writeFileSync(process.argv[2],server.resolvedUrls.local[0]+'desktop-settings-preview.html');
