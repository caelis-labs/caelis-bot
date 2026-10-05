import {createServer} from 'vite';
import {execFileSync,spawn} from 'node:child_process';
import {mkdirSync,copyFileSync,writeFileSync,existsSync,readFileSync} from 'node:fs';
import {resolve,dirname} from 'node:path';

const [binary,phase='baseline',page='telegram',state='empty',lang='en',theme='light',width='860',interaction='-']=process.argv.slice(2);
const fixture={empty:{enabled:false,bot:'',owner:'',paired:false,candidate:'',pairURL:'',issue:''},pairing:{enabled:true,bot:'CaelisHelperBot',owner:'',paired:false,candidate:'',pairURL:'https://t.me/CaelisHelperBot?start=fixture',issue:''},candidate:{enabled:true,bot:'CaelisHelperBot',owner:'',paired:false,candidate:'Fixture User',pairURL:'https://t.me/CaelisHelperBot?start=fixture',issue:''},connected:{enabled:true,bot:'CaelisHelperBot',owner:'Fixture User',paired:true,candidate:'',pairURL:'',issue:''},paused:{enabled:false,bot:'CaelisHelperBot',owner:'Fixture User',paired:true,candidate:'',pairURL:'',issue:''},webhook:{enabled:false,bot:'CaelisHelperBot',owner:'',paired:false,candidate:'',pairURL:'',issue:'webhook'},network:{enabled:true,bot:'CaelisHelperBot',owner:'Fixture User',paired:true,candidate:'',pairURL:'',issue:'network'},expired:{enabled:true,bot:'CaelisHelperBot',owner:'',paired:false,candidate:'',pairURL:'',issue:'pairing_expired'}};
const telegram=fixture[state]??(state==='forget'?fixture.connected:fixture.empty);
const section=page==='setup'?'setup':page==='overview'?'chatConnections':page==='telegram'?'telegram':page==='extras'?'extras':'connections';
const mock=`let status=${JSON.stringify(telegram)},enabled=${!state.endsWith('off')},statusReads=0,preferenceReads=0,featuresPending=${state.startsWith('feature')},permissionsPending=${state.startsWith('feature')||state.startsWith('permission')};
window.__fixtureCalls=[];
export const Call={ByName:async(name,...args)=>{
 const method=name.split('.').at(-1);
 window.__fixtureCalls.push(method==='ConnectTelegram'?[method,'<masked>']:[method,...args]);
 switch(method){
 case 'TelegramStatus':if(${state==='load-recover'}&&++statusReads===1)throw Error('fixture status unavailable');return status;
 case 'OpenTelegramSetup':if(${state==='open-error'})throw Error('fixture open failed');return;
 case 'ConnectTelegram':if(${state==='connecting'})return new Promise(()=>{});return status;
 case 'FeatureGuidePending':return featuresPending;
 case 'FinishFeatureGuide':featuresPending=false;return;
 case 'PermissionGuidePending':return permissionsPending;
 case 'FinishPermissionGuide':permissionsPending=false;return;
 case 'BotInitialization':return {required:false,status:'',message:''};
 case 'Snapshot':return {connection:${state==='ready'||state==='feature-ready'?'"ready"':'"notReady"'}};
 case 'OpenHistory':return;
 case 'CloseSettings':return;
 case 'SystemPermissions':return {supported:true,appPath:'/Fixture.app',development:true,permissions:[{id:'accessibility',status:'notDetermined'},{id:'screenCapture',status:'notDetermined'},{id:'notifications',status:'notDetermined'}]};
 case 'CapturePreferences':if(${state==='feature-load-fail'}&&++preferenceReads===1)throw Error('fixture preference read failed');return {enabled,includeBackground:true,notice:''};
 case 'SetCaptureEnabled':if(${state==='feature-fail'})throw Error('fixture save failed');enabled=args[0];return {enabled,includeBackground:true,notice:''};
 case 'CaptureShortcutSettings':return {shortcut:{enabled:true,key:'F1',control:false,alt:false,shift:false,meta:false},registered:enabled,message:''};
 case 'PasteShortcutSettings':return {shortcut:{enabled:true,key:'F3',control:false,alt:false,shift:false,meta:false},registered:enabled,message:''};
 case 'AppVersion':return 'Fixture';
 case 'SettingsSection':return ${JSON.stringify(section)};
 default:throw Error('Fixture has no '+method)
 }
}};`;
const server=await createServer({configFile:false,root:resolve('frontend'),cacheDir:resolve('.cache/vite-fixture'),esbuild:{jsx:'automatic'},server:{host:'127.0.0.1',port:0},plugins:[{name:'fixture-wails',configureServer(s){s.middlewares.use('/wails/runtime.js',(_req,res)=>{res.setHeader('Content-Type','text/javascript');res.end(mock);});}}]});
await server.listen();
try{
 const outdir=resolve('.cache/settings-extras-fixture',phase), temp=resolve(dirname(binary),`settings-${process.pid}.app`),executable=resolve(temp,'Contents/MacOS/fixture');
 mkdirSync(outdir,{recursive:true});mkdirSync(dirname(executable),{recursive:true});copyFileSync(binary,executable);
 writeFileSync(resolve(temp,'Contents/Info.plist'),`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>fixture</string><key>CFBundleIdentifier</key><string>dev.caelis.settings-fixture.${process.pid}</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`);
 execFileSync('/usr/bin/codesign',['--force','--sign','-',temp]);
 const url=`${server.resolvedUrls.local[0]}${page==='runtime'?'runtime-settings-preview.html':'settings-extras-preview.html'}?page=${page}&state=${state}&lang=${lang}&theme=${theme}&width=${width}`;
 const out=resolve(outdir,`${page}-${state}-${lang}-${theme}-${width}.png`), log=resolve(outdir,'host.log');
 const child=spawn('/usr/bin/open',['-W','-n','--stdout',log,'--stderr',log,temp,'--args',url,out,interaction==='-'?'-':resolve(interaction)],{stdio:'inherit'});
 const code=await new Promise((done,reject)=>{child.once('error',reject);child.once('exit',done)});
 if(code!==0||!existsSync(out))throw Error(`Native fixture failed: ${existsSync(log)?readFileSync(log,'utf8'):code}`);
 process.stdout.write(out+'\n');
}finally{await server.close();}
