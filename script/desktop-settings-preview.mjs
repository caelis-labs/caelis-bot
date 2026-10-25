import {createServer} from 'vite';
import {writeFileSync} from 'node:fs';
import {fixtureRuntime} from './desktop-settings-fixture.mjs';
const server=await createServer({cacheDir:process.env.BOT_SETTINGS_PREVIEW_CACHE||'.cache/desktop-settings-vite',server:{port:0,strictPort:false,hmr:process.env.BOT_REMOTE_UI_MATRIX?false:undefined},plugins:[{name:'synthetic-desktop-transport',configureServer(s){s.middlewares.use((req,res,next)=>{if(req.url?.split('?')[0]!=='/wails/runtime.js')return next();res.setHeader('Content-Type','application/javascript');res.end(fixtureRuntime);});}}]});
await server.listen();
writeFileSync(process.argv[2],process.env.BOT_SETTINGS_PREVIEW_URL||server.resolvedUrls.local[0]+'desktop-settings-preview.html');
